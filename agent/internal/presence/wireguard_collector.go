package presence

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const WireGuardHandshakeCollectorSource = "wireguard-latest-handshake"
const wireGuardHandshakeMaxAge = 3 * time.Minute

var errWireGuardPresenceUnavailable = errors.New("wireguard presence source is unavailable")

type WireGuardCollector struct {
	activeConfigPath string
	interfaceName    string
	wgPath           string
	run              commandRunner
	now              func() time.Time
}

func NewWireGuardCollector(activeConfigPath, interfaceName, wgPath string) *WireGuardCollector {
	if strings.TrimSpace(wgPath) == "" {
		wgPath = "wg"
	}
	return &WireGuardCollector{
		activeConfigPath: strings.TrimSpace(activeConfigPath),
		interfaceName: strings.TrimSpace(interfaceName),
		wgPath: strings.TrimSpace(wgPath),
		run: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return exec.CommandContext(ctx, name, args...).Output()
		},
		now: time.Now,
	}
}

func (c *WireGuardCollector) Collect(ctx context.Context) (Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	now := c.now().UTC()
	peers, err := readWireGuardPeers(c.activeConfigPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Snapshot{}, errWireGuardPresenceUnavailable
		}
		return Snapshot{}, err
	}
	if len(peers) == 0 || c.interfaceName == "" {
		return Snapshot{}, errWireGuardPresenceUnavailable
	}
	dump, err := c.run(ctx, c.wgPath, "show", c.interfaceName, "dump")
	if err != nil {
		return Snapshot{}, errWireGuardPresenceUnavailable
	}
	items := make([]Observation, 0)
	for publicKey, handshakeAt := range parseWireGuardLatestHandshakes(string(dump)) {
		accountID, ok := peers[publicKey]
		if !ok {
			continue
		}
		age := now.Sub(handshakeAt)
		if age < -15*time.Second || age > wireGuardHandshakeMaxAge {
			continue
		}
		lastActivity := handshakeAt
		items = append(items, Observation{
			VPNAccountID: accountID,
			Protocol: "wireguard",
			ConnectionCount: 1,
			Source: WireGuardHandshakeCollectorSource,
			Confidence: "exact",
			LastActivityAt: &lastActivity,
		})
	}
	items = mergePresenceItems(nil, items)
	return Snapshot{ObservedAt: now, Items: items}, nil
}

func readWireGuardPeers(path string) (map[string]string, error) {
	data, err := os.ReadFile(strings.TrimSpace(path))
	if err != nil {
		return nil, fmt.Errorf("read active WireGuard config: %w", err)
	}
	peers := make(map[string]string)
	section := ""
	accountID := ""
	for _, rawLine := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(rawLine)
		switch line {
		case "[Interface]", "[Peer]":
			section = line
			accountID = ""
			continue
		}
		if section != "[Peer]" {
			continue
		}
		if value, ok := strings.CutPrefix(line, "# routegate-account-id:"); ok {
			accountID = strings.TrimSpace(value)
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(key) != "PublicKey" || accountID == "" {
			continue
		}
		publicKey := strings.TrimSpace(value)
		if publicKey != "" {
			peers[publicKey] = accountID
		}
	}
	return peers, nil
}

func parseWireGuardLatestHandshakes(dump string) map[string]time.Time {
	result := make(map[string]time.Time)
	lines := strings.Split(strings.TrimSpace(dump), "\n")
	for index, line := range lines {
		if index == 0 {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 5 {
			fields = strings.Fields(line)
		}
		if len(fields) < 5 {
			continue
		}
		seconds, err := strconv.ParseInt(fields[4], 10, 64)
		if err != nil || seconds <= 0 {
			continue
		}
		result[strings.TrimSpace(fields[0])] = time.Unix(seconds, 0).UTC()
	}
	return result
}
