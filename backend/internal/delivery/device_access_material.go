package delivery

import (
	"strings"
	"sync"
	"time"
)

// deviceAccessMaterialStore holds a device's plaintext RG-115 access URL in
// memory just long enough for the delivery worker to render and send it.
//
// RG-115 subscription tokens are stored hash-only; RouteGate never persists
// the plaintext bearer URL anywhere (see docs/architecture/secure-config-delivery.md).
// A device-scoped delivery therefore cannot re-resolve its material from the
// database the way account-level delivery re-derives raw protocol material
// from persistent account credentials. Stashing the URL here - process-local,
// short-lived, never written to disk or logs - is what lets a device-scoped
// "Send" reuse the same durable queue/retry/history worker as account-level
// delivery without introducing plaintext token storage. If the process
// restarts or the entry expires before the worker gets to it, resolution
// fails with a clear, permanent "rotate and resend" error instead of
// fabricating or recovering the URL.
type deviceAccessMaterialStore struct {
	mu      sync.Mutex
	entries map[string]deviceAccessMaterialEntry
	ttl     time.Duration
	now     func() time.Time
}

type deviceAccessMaterialEntry struct {
	accessURL   string
	profileName string
	expiresAt   time.Time
}

const defaultDeviceAccessMaterialTTL = 30 * time.Minute

func newDeviceAccessMaterialStore() *deviceAccessMaterialStore {
	return &deviceAccessMaterialStore{
		entries: map[string]deviceAccessMaterialEntry{},
		ttl:     defaultDeviceAccessMaterialTTL,
		now:     time.Now,
	}
}

// globalDeviceAccessMaterialStore is a package-level singleton so the
// Handler (which stashes material right after creating a device-scoped
// delivery) and the Worker's resolver (constructed independently, possibly
// in a different call site) share the same in-memory entries within one
// process.
var globalDeviceAccessMaterialStore = newDeviceAccessMaterialStore()

func (s *deviceAccessMaterialStore) put(deliveryID, accessURL, profileName string) {
	deliveryID = strings.TrimSpace(deliveryID)
	if deliveryID == "" || strings.TrimSpace(accessURL) == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepLocked()
	s.entries[deliveryID] = deviceAccessMaterialEntry{
		accessURL:   accessURL,
		profileName: profileName,
		expiresAt:   s.now().Add(s.ttl),
	}
}

func (s *deviceAccessMaterialStore) get(deliveryID string) (deviceAccessMaterialEntry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.entries[strings.TrimSpace(deliveryID)]
	if !ok || s.now().After(entry.expiresAt) {
		return deviceAccessMaterialEntry{}, false
	}
	return entry, true
}

// sweepLocked drops expired entries. Called with mu held.
func (s *deviceAccessMaterialStore) sweepLocked() {
	now := s.now()
	for id, entry := range s.entries {
		if now.After(entry.expiresAt) {
			delete(s.entries, id)
		}
	}
}
