package presence

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const SingBoxSocketCollectorSource = "sing-box-socket-journal"
const SingBoxRecentAuthCollectorSource = "sing-box-auth-journal"
const externalSnapshotMaxAge = 75 * time.Second
const recentAuthenticationMaxAge = 75 * time.Second
const singBoxLogRoot = "/var/lib/sing-box"
const maxSingBoxLogReadBytes int64 = 8 << 20

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
		if fileSnapshot.ObservedAt.After(nativeSnapshot.ObservedAt.Add(externalSnapshotMaxAge)) ||
			nativeSnapshot.ObservedAt.Sub(fileSnapshot.ObservedAt) > externalSnapshotMaxAge {
			return nativeSnapshot, nil
		}
		seen := make(map[string]struct{}, len(nativeSnapshot.Items))
		for _, item := range nativeSnapshot.Items {
			seen[item.VPNAccountID+"\x00"+strings.ToLower(item.Protocol)] = struct{}{}
		}
		for _, item := range fileSnapshot.Items {
			key := item.VPNAccountID + "\x00" + strings.ToLower(item.Protocol)
			if _, exists := seen[key]; !exists {
				if item.LastActivityAt == nil {
					observedAt := fileSnapshot.ObservedAt
					item.LastActivityAt = &observedAt
				}
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
	mu               sync.Mutex
	processID        string
	journalReadAt    time.Time
	contexts         map[string]singBoxContextState
	authenticated    map[netip.AddrPort]string
	recentAuth       map[string]time.Time
	logFilePath      string
	logFileOffset    int64
	logRoot          string
}

func NewSingBoxCollector(activeConfigPath, serviceName string) *SingBoxCollector {
	return &SingBoxCollector{
		activeConfigPath: strings.TrimSpace(activeConfigPath),
		serviceName:      strings.TrimSpace(serviceName),
		run: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return exec.CommandContext(ctx, name, args...).Output()
		},
		now: time.Now,
		logRoot: singBoxLogRoot,
	}
}

type singBoxConfig struct {
	Log struct {
		Output string `json:"output"`
	} `json:"log"`
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
	usersByPort, logOutput, err := readSingBoxUsers(c.activeConfigPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Snapshot{}, errSingBoxPresenceUnavailable
		}
		return Snapshot{}, err
	}
	if len(usersByPort) == 0 {
		return Snapshot{}, errSingBoxPresenceUnavailable
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	serviceName := normalizeServiceName(c.serviceName)
	pidOutput, err := c.run(ctx, "systemctl", "show", "--property=MainPID", "--value", serviceName)
	if err != nil {
		return Snapshot{}, fmt.Errorf("read sing-box process ID: %w", err)
	}
	processID := strings.TrimSpace(string(pidOutput))
	pid, parseErr := strconv.ParseUint(processID, 10, 32)
	if parseErr != nil || pid == 0 {
		return Snapshot{}, errSingBoxPresenceUnavailable
	}

	// Filter by the systemd unit rather than MainPID. Depending on how the
	// service hands stdout/stderr to journald, sing-box records can belong to
	// the unit invocation without carrying the unit's current MainPID field.
	// MainPID is still used below to reset correlation state after a restart.
	journalArgs := []string{"-b", "-u", serviceName, "--no-pager", "-o", "json"}
	if c.processID != processID {
		c.processID = processID
		c.journalReadAt = time.Time{}
		c.contexts = make(map[string]singBoxContextState)
		c.authenticated = make(map[netip.AddrPort]string)
		c.recentAuth = make(map[string]time.Time)
		c.logFilePath = ""
		c.logFileOffset = 0
	} else if !c.journalReadAt.IsZero() {
		journalArgs = append(journalArgs, "--since", "@"+strconv.FormatInt(c.journalReadAt.Add(-2*time.Second).Unix(), 10))
	}
	journal, err := c.run(ctx, "journalctl", journalArgs...)
	if err != nil {
		return Snapshot{}, fmt.Errorf("read sing-box journal: %w", err)
	}
	c.consumeJournal(string(journal), now)
	if logOutput != "" && logOutput != "stdout" && logOutput != "stderr" {
		fileLog, readErr := c.readLogFile(logOutput)
		if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
			return Snapshot{}, fmt.Errorf("read sing-box presence log: %w", readErr)
		}
		c.consumeJournal(fileLog, now)
	}
	c.journalReadAt = now
	activeSockets, err := c.run(ctx, "ss", "-Htn", "state", "established")
	if err != nil {
		return Snapshot{}, fmt.Errorf("read established TCP sockets: %w", err)
	}

	activePeers := make(map[netip.AddrPort]struct{})
	counts := make(map[singBoxUser]int)
	for _, socket := range parseEstablishedSockets(string(activeSockets)) {
		users := usersByPort[socket.localPort]
		if len(users) == 0 {
			continue
		}
		activePeers[socket.peer] = struct{}{}
		userName, ok := c.authenticated[socket.peer]
		if !ok {
			continue
		}
		user, ok := users[userName]
		if !ok {
			continue
		}
		counts[user]++
	}
	for peer := range c.authenticated {
		if _, active := activePeers[peer]; !active {
			delete(c.authenticated, peer)
		}
	}
	for contextID, state := range c.contexts {
		if _, active := activePeers[state.peer]; !active {
			delete(c.contexts, contextID)
		}
	}
	for name, authenticatedAt := range c.recentAuth {
		if now.Sub(authenticatedAt) > recentAuthenticationMaxAge {
			delete(c.recentAuth, name)
		}
	}

	items := make([]Observation, 0, len(counts)+len(c.recentAuth))
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
	usersByName := uniqueSingBoxUsersByName(usersByPort)
	for name, authenticatedAt := range c.recentAuth {
		user, ok := usersByName[name]
		if !ok || counts[user] > 0 {
			continue
		}
		lastActivity := authenticatedAt
		items = append(items, Observation{
			VPNAccountID:   user.credentialID,
			Protocol:       user.protocol,
			ConnectionCount: 1,
			Source:         SingBoxRecentAuthCollectorSource,
			Confidence:     "heuristic",
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

func uniqueSingBoxUsersByName(usersByPort map[int]map[string]singBoxUser) map[string]singBoxUser {
	counts := make(map[string]int)
	users := make(map[string]singBoxUser)
	for _, byName := range usersByPort {
		for name, user := range byName {
			counts[name]++
			users[name] = user
		}
	}
	for name, count := range counts {
		if count != 1 { delete(users, name) }
	}
	return users
}

func readSingBoxUsers(path string) (map[int]map[string]singBoxUser, string, error) {
	data, err := os.ReadFile(strings.TrimSpace(path))
	if err != nil {
		return nil, "", fmt.Errorf("read active sing-box config: %w", err)
	}
	var config singBoxConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, "", fmt.Errorf("parse active sing-box config: %w", err)
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
	return result, strings.TrimSpace(config.Log.Output), nil
}

func (c *SingBoxCollector) readLogFile(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", nil
	}
	root := filepath.Clean(strings.TrimSpace(c.logRoot))
	cleanPath := filepath.Clean(path)
	relative, err := filepath.Rel(root, cleanPath)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("sing-box log path is outside the allowed state directory")
	}
	file, err := os.Open(cleanPath)
	if err != nil {
		return "", err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", err
	}
	if c.logFilePath != cleanPath || info.Size() < c.logFileOffset {
		c.logFilePath = cleanPath
		c.logFileOffset = 0
	}
	if unread := info.Size() - c.logFileOffset; unread > maxSingBoxLogReadBytes {
		c.logFileOffset = info.Size() - maxSingBoxLogReadBytes
	}
	if _, err := file.Seek(c.logFileOffset, io.SeekStart); err != nil {
		return "", err
	}
	data, err := io.ReadAll(io.LimitReader(file, maxSingBoxLogReadBytes))
	if err != nil {
		return "", err
	}
	c.logFileOffset += int64(len(data))
	return string(data), nil
}

var singBoxJournalContextPattern = regexp.MustCompile(`\[(\d+)(?:\s+[^\]]*)?\]\s+inbound/vless\[[^\]]+\]:\s+(.+)$`)
var singBoxAuthenticatedUserPattern = regexp.MustCompile(`^\[([^\]]+)\]\s+inbound (?:multiplex |packet addr |packet )?connection`)

type singBoxContextState struct {
	peer netip.AddrPort
	name string
}

type systemdJournalEntry struct {
	Message   string `json:"MESSAGE"`
	Timestamp string `json:"__REALTIME_TIMESTAMP"`
}

type singBoxJournalMessage struct {
	message string
	at      time.Time
}

func parseJournalMessages(journal string, fallback time.Time) []singBoxJournalMessage {
	messages := make([]singBoxJournalMessage, 0)
	for _, line := range strings.Split(journal, "\n") {
		line = strings.TrimSpace(line)
		if line == "" { continue }
		entry := systemdJournalEntry{}
		if strings.HasPrefix(line, "{") && json.Unmarshal([]byte(line), &entry) == nil && entry.Message != "" {
			at := fallback
			if micros, err := strconv.ParseInt(entry.Timestamp, 10, 64); err == nil {
				at = time.UnixMicro(micros).UTC()
			}
			messages = append(messages, singBoxJournalMessage{message: entry.Message, at: at})
			continue
		}
		messages = append(messages, singBoxJournalMessage{message: line, at: fallback})
	}
	return messages
}

func (c *SingBoxCollector) consumeJournal(journal string, fallback time.Time) {
	if c.contexts == nil { c.contexts = make(map[string]singBoxContextState) }
	if c.authenticated == nil { c.authenticated = make(map[netip.AddrPort]string) }
	if c.recentAuth == nil { c.recentAuth = make(map[string]time.Time) }
	for _, journalMessage := range parseJournalMessages(journal, fallback) {
		match := singBoxJournalContextPattern.FindStringSubmatch(journalMessage.message)
		if len(match) != 3 {
			continue
		}
		state := c.contexts[match[1]]
		payload := match[2]
		if rawPeer, ok := strings.CutPrefix(payload, "inbound connection from "); ok {
			fields := strings.Fields(rawPeer)
			if len(fields) == 0 {
				continue
			}
			if peer, err := parseAddrPort(fields[0]); err == nil {
				if state.peer.IsValid() {
					delete(c.authenticated, state.peer)
				}
				state = singBoxContextState{peer: peer}
				delete(c.authenticated, peer)
			}
		} else if userMatch := singBoxAuthenticatedUserPattern.FindStringSubmatch(payload); len(userMatch) == 2 {
			state.name = strings.TrimSpace(userMatch[1])
			if previous, ok := c.recentAuth[state.name]; !ok || journalMessage.at.After(previous) {
				c.recentAuth[state.name] = journalMessage.at
			}
			if state.peer.IsValid() {
				c.authenticated[state.peer] = state.name
			}
		}
		c.contexts[match[1]] = state
	}
}

func parseAuthenticatedPeers(journal string) map[netip.AddrPort]string {
	collector := &SingBoxCollector{}
	collector.consumeJournal(journal, time.Now().UTC())
	return collector.authenticated
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
