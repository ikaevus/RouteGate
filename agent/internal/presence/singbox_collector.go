package presence

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const SingBoxSocketCollectorSource = "sing-box-socket-journal"

var errSingBoxPresenceUnavailable = errors.New("sing-box presence source is unavailable")

type commandRunner func(context.Context, string, ...string) ([]byte, error)

type RuntimeCollector struct {
	singBox *SingBoxCollector
	file    *FileCollector
}

func NewRuntimeCollector(activeConfigPath, serviceName, filePath string) *RuntimeCollector {
	return &RuntimeCollector{
		singBox: NewSingBoxCollector(activeConfigPath, serviceName),
		file:    NewFileCollector(filePath),
	}
}

func (c *RuntimeCollector) Collect(ctx context.Context) (Snapshot, error) {
	nativeSnapshot, err := c.singBox.Collect(ctx)
	if !errors.Is(err, errSingBoxPresenceUnavailable) {
		if err != nil {
			return Snapshot{}, err
		}
		fileSnapshot, fileErr := c.file.Collect(ctx)
		if fileErr != nil {
			return Snapshot{}, fileErr
		}
		seen := make(map[string]struct{}, len(nativeSnapshot.Items))
		for _, item := range nativeSnapshot.Items {
			seen[item.VPNAccountID+"\x00"+strings.ToLower(item.Protocol)] = struct{}{}
		}
		for _, item := range fileSnapshot.Items {
			key := item.VPNAccountID + "\x00" + strings.ToLower(item.Protocol)
			if _, exists := seen[key]; !exists {
				nativeSnapshot.Items = append(nativeSnapshot.Items, item)
			}
		}
		return nativeSnapshot, nil
	}
	return c.file.Collect(ctx)
}

type SingBoxCollector struct {
	activeConfigPath string
	serviceName      string
	run              commandRunner
	now              func() time.Time
}

func NewSingBoxCollector(activeConfigPath, serviceName string) *SingBoxCollector {
	return &SingBoxCollector{
		activeConfigPath: strings.TrimSpace(activeConfigPath),
		serviceName:      strings.TrimSpace(serviceName),
		run: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return exec.CommandContext(ctx, name, args...).Output()
		},
		now: time.Now,
	}
}

type singBoxConfig struct {
	Inbounds []struct {
		Type       string `json:"type"`
		ListenPort int    `json:"listen_port"`
		TLS        *struct {
			Reality *struct {
				Enabled bool `json:"enabled"`
			} `json:"reality"`
		} `json:"tls"`
		Users []struct {
			Name string `json:"name"`
			UUID string `json:"uuid"`
		} `json:"users"`
	} `json:"inbounds"`
}

type singBoxUser struct {
	credentialID string
	protocol     string
}

func (c *SingBoxCollector) Collect(ctx context.Context) (Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	now := c.now().UTC()
	usersByPort, err := readSingBoxUsers(c.activeConfigPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Snapshot{}, errSingBoxPresenceUnavailable
		}
		return Snapshot{}, err
	}
	if len(usersByPort) == 0 {
		return Snapshot{}, errSingBoxPresenceUnavailable
	}

	journal, err := c.run(ctx, "journalctl", "-u", normalizeServiceName(c.serviceName), "--no-pager", "-o", "cat", "-n", "20000")
	if err != nil {
		return Snapshot{}, fmt.Errorf("read sing-box journal: %w", err)
	}
	activeSockets, err := c.run(ctx, "ss", "-Htn", "state", "established")
	if err != nil {
		return Snapshot{}, fmt.Errorf("read established TCP sockets: %w", err)
	}

	authenticatedPeers := parseAuthenticatedPeers(string(journal))
	counts := make(map[singBoxUser]int)
	for _, socket := range parseEstablishedSockets(string(activeSockets)) {
		users := usersByPort[socket.localPort]
		if len(users) == 0 {
			continue
		}
		userName, ok := authenticatedPeers[socket.peer]
		if !ok {
			continue
		}
		user, ok := users[userName]
		if !ok {
			continue
		}
		counts[user]++
	}

	items := make([]Observation, 0, len(counts))
	for user, count := range counts {
		lastActivity := now
		items = append(items, Observation{
			VPNAccountID:   user.credentialID,
			Protocol:       user.protocol,
			ConnectionCount: count,
			Source:         SingBoxSocketCollectorSource,
			Confidence:     "exact",
			LastActivityAt: &lastActivity,
		})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].VPNAccountID == items[j].VPNAccountID {
			return items[i].Protocol < items[j].Protocol
		}
		return items[i].VPNAccountID < items[j].VPNAccountID
	})
	return Snapshot{ObservedAt: now, Items: items}, nil
}

func readSingBoxUsers(path string) (map[int]map[string]singBoxUser, error) {
	data, err := os.ReadFile(strings.TrimSpace(path))
	if err != nil {
		return nil, fmt.Errorf("read active sing-box config: %w", err)
	}
	var config singBoxConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("parse active sing-box config: %w", err)
	}
	result := make(map[int]map[string]singBoxUser)
	for _, inbound := range config.Inbounds {
		if !strings.EqualFold(strings.TrimSpace(inbound.Type), "vless") || inbound.ListenPort < 1 || inbound.ListenPort > 65535 {
			continue
		}
		protocol := "vless"
		if inbound.TLS != nil && inbound.TLS.Reality != nil && inbound.TLS.Reality.Enabled {
			protocol = "vless-reality"
		}
		byName := make(map[string]singBoxUser)
		nameCounts := make(map[string]int)
		for _, user := range inbound.Users {
			nameCounts[strings.TrimSpace(user.Name)]++
		}
		for _, user := range inbound.Users {
			name := strings.TrimSpace(user.Name)
			credentialID := strings.TrimSpace(user.UUID)
			if name == "" || credentialID == "" || nameCounts[name] != 1 {
				continue
			}
			byName[name] = singBoxUser{credentialID: credentialID, protocol: protocol}
		}
		if len(byName) > 0 {
			result[inbound.ListenPort] = byName
		}
	}
	return result, nil
}

var singBoxJournalContextPattern = regexp.MustCompile(`\[(\d+)(?:\s+[^\]]*)?\]\s+inbound/vless\[[^\]]+\]:\s+(.+)$`)
var singBoxAuthenticatedUserPattern = regexp.MustCompile(`^\[([^\]]+)\]\s+inbound (?:multiplex |packet addr |packet )?connection`)

func parseAuthenticatedPeers(journal string) map[netip.AddrPort]string {
	type contextState struct {
		peer netip.AddrPort
		name string
	}
	states := make(map[string]contextState)
	result := make(map[netip.AddrPort]string)
	for _, line := range strings.Split(journal, "\n") {
		match := singBoxJournalContextPattern.FindStringSubmatch(strings.TrimSpace(line))
		if len(match) != 3 {
			continue
		}
		state := states[match[1]]
		payload := match[2]
		if rawPeer, ok := strings.CutPrefix(payload, "inbound connection from "); ok {
			fields := strings.Fields(rawPeer)
			if len(fields) == 0 {
				continue
			}
			if peer, err := parseAddrPort(fields[0]); err == nil {
				if state.peer.IsValid() {
					delete(result, state.peer)
				}
				state = contextState{peer: peer}
				delete(result, peer)
			}
		} else if userMatch := singBoxAuthenticatedUserPattern.FindStringSubmatch(payload); len(userMatch) == 2 {
			state.name = strings.TrimSpace(userMatch[1])
			if state.peer.IsValid() {
				result[state.peer] = state.name
			}
		}
		states[match[1]] = state
	}
	return result
}

type establishedSocket struct {
	localPort int
	peer      netip.AddrPort
}

func parseEstablishedSockets(output string) []establishedSocket {
	result := []establishedSocket{}
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		localPort, err := endpointPort(fields[2])
		if err != nil {
			continue
		}
		peer, err := parseAddrPort(fields[3])
		if err != nil {
			continue
		}
		result = append(result, establishedSocket{localPort: localPort, peer: peer})
	}
	return result
}

func endpointPort(value string) (int, error) {
	index := strings.LastIndex(value, ":")
	if index < 0 || index == len(value)-1 {
		return 0, fmt.Errorf("endpoint has no port")
	}
	return strconv.Atoi(value[index+1:])
}

func parseAddrPort(value string) (netip.AddrPort, error) {
	value = strings.TrimSpace(value)
	if parsed, err := netip.ParseAddrPort(value); err == nil {
		return netip.AddrPortFrom(parsed.Addr().Unmap(), parsed.Port()), nil
	}
	index := strings.LastIndex(value, ":")
	if index < 0 {
		return netip.AddrPort{}, fmt.Errorf("endpoint has no port")
	}
	host := strings.Trim(value[:index], "[]")
	port, err := strconv.ParseUint(value[index+1:], 10, 16)
	if err != nil {
		return netip.AddrPort{}, err
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return netip.AddrPort{}, err
	}
	return netip.AddrPortFrom(addr.Unmap(), uint16(port)), nil
}

func normalizeServiceName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "sing-box.service"
	}
	if strings.HasSuffix(value, ".service") {
		return value
	}
	return value + ".service"
}
