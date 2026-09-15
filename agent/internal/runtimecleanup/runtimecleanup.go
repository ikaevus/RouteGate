package runtimecleanup

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"syscall"
	"time"

	"github.com/ikaevus/routegate/agent/internal/config"
)

const (
	SchemaVersion   = 1
	RetentionDays   = 30
	MaxCandidates   = 256
	MaxRequestBytes = 64 * 1024

	OperationAnalyze = "analyze_runtime_artifacts"
	OperationCleanup = "cleanup_runtime_artifacts"
	OperationVerify  = "verify_runtime_artifacts"
)

var canonicalArtifactName = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-5][0-9a-fA-F]{3}-[89aAbB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}(?:\.previous)?\.(?:json|conf|toml)$`)

type Candidate struct {
	RootID string `json:"rootId"`
	Name   string `json:"name"`
}

type Request struct {
	SchemaVersion int         `json:"schemaVersion"`
	Cutoff        time.Time   `json:"cutoff"`
	Candidates    []Candidate `json:"candidates,omitempty"`
}

type Result struct {
	SchemaVersion  int         `json:"schemaVersion"`
	Operation      string      `json:"operation"`
	CandidateCount int64       `json:"candidateCount,omitempty"`
	EstimatedBytes int64       `json:"estimatedBytes,omitempty"`
	DeletedCount   int64       `json:"deletedCount,omitempty"`
	ReclaimedBytes int64       `json:"reclaimedBytes,omitempty"`
	RemainingCount int64       `json:"remainingCount,omitempty"`
	Candidates     []Candidate `json:"candidates,omitempty"`
}

type rootSpec struct {
	id       string
	path     string
	isBackup bool
}

type Cleaner struct {
	roots    map[string]rootSpec
	ownerUID uint32
}

func New(cfg config.Config) Cleaner {
	specs := []rootSpec{
		{id: "sing_box_staging", path: cfg.ConfigStagingDir},
		{id: "sing_box_backups", path: cfg.ConfigBackupDir, isBackup: true},
		{id: "wireguard_staging", path: cfg.WireGuardStagingDir},
		{id: "wireguard_backups", path: cfg.WireGuardBackupDir, isBackup: true},
		{id: "hysteria2_staging", path: cfg.Hysteria2StagingDir},
		{id: "hysteria2_backups", path: cfg.Hysteria2BackupDir, isBackup: true},
		{id: "mtproto_staging", path: cfg.MTProtoStagingDir},
		{id: "mtproto_backups", path: cfg.MTProtoBackupDir, isBackup: true},
	}
	roots := make(map[string]rootSpec, len(specs))
	for _, spec := range specs {
		if spec.path != "" {
			roots[spec.id] = spec
		}
	}
	return Cleaner{roots: roots, ownerUID: uint32(os.Geteuid())}
}

func DecodeRequest(payload json.RawMessage, operation string) (Request, error) {
	if len(payload) == 0 || len(payload) > MaxRequestBytes {
		return Request{}, errors.New("maintenance request payload size is invalid")
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var request Request
	if err := decoder.Decode(&request); err != nil {
		return Request{}, fmt.Errorf("decode maintenance request: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return Request{}, errors.New("maintenance request must contain one JSON object")
	}
	if request.SchemaVersion != SchemaVersion || request.Cutoff.IsZero() || request.Cutoff.After(time.Now().UTC()) {
		return Request{}, errors.New("maintenance request policy is invalid")
	}
	if len(request.Candidates) > MaxCandidates {
		return Request{}, errors.New("maintenance request has too many candidates")
	}
	if operation == OperationAnalyze && len(request.Candidates) != 0 {
		return Request{}, errors.New("analysis request must not contain candidates")
	}
	if operation != OperationAnalyze && operation != OperationCleanup && operation != OperationVerify {
		return Request{}, errors.New("unsupported maintenance operation")
	}
	return request, nil
}

func (c Cleaner) Execute(operation string, request Request) (Result, error) {
	switch operation {
	case OperationAnalyze:
		candidates, bytes, err := c.analyze(request.Cutoff)
		return Result{SchemaVersion: SchemaVersion, Operation: operation, CandidateCount: int64(len(candidates)), EstimatedBytes: bytes, Candidates: candidates}, err
	case OperationCleanup:
		deleted, reclaimed, err := c.cleanup(request.Cutoff, request.Candidates)
		return Result{SchemaVersion: SchemaVersion, Operation: operation, DeletedCount: deleted, ReclaimedBytes: reclaimed}, err
	case OperationVerify:
		remaining, err := c.verify(request.Candidates)
		return Result{SchemaVersion: SchemaVersion, Operation: operation, RemainingCount: remaining}, err
	default:
		return Result{}, errors.New("unsupported maintenance operation")
	}
}

func (c Cleaner) analyze(cutoff time.Time) ([]Candidate, int64, error) {
	rootIDs := make([]string, 0, len(c.roots))
	for id := range c.roots {
		rootIDs = append(rootIDs, id)
	}
	sort.Strings(rootIDs)
	var candidates []Candidate
	var estimatedBytes int64
	seenPaths := make(map[string]struct{}, len(rootIDs))
	for _, id := range rootIDs {
		spec := c.roots[id]
		cleanPath := filepath.Clean(spec.path)
		if _, duplicate := seenPaths[cleanPath]; duplicate {
			return nil, 0, errors.New("Agent runtime artifact roots overlap")
		}
		seenPaths[cleanPath] = struct{}{}
		entries, err := c.rootEntries(spec)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, 0, err
		}
		newestBackup := ""
		var newestTime time.Time
		for _, entry := range entries {
			if spec.isBackup && (newestBackup == "" || entry.info.ModTime().After(newestTime)) {
				newestBackup, newestTime = entry.name, entry.info.ModTime()
			}
		}
		for _, entry := range entries {
			if !entry.info.ModTime().Before(cutoff) || entry.name == newestBackup {
				continue
			}
			candidates = append(candidates, Candidate{RootID: id, Name: entry.name})
			estimatedBytes += entry.info.Size()
			if len(candidates) > MaxCandidates {
				return nil, 0, errors.New("too many Agent runtime cleanup candidates")
			}
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].RootID == candidates[j].RootID {
			return candidates[i].Name < candidates[j].Name
		}
		return candidates[i].RootID < candidates[j].RootID
	})
	return candidates, estimatedBytes, nil
}

type artifactEntry struct {
	name string
	info os.FileInfo
}

func (c Cleaner) rootEntries(spec rootSpec) ([]artifactEntry, error) {
	info, err := os.Lstat(spec.path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || info.Mode().Perm()&0o022 != 0 || fileOwner(info) != c.ownerUID {
		return nil, errors.New("Agent runtime artifact root is unsafe")
	}
	entries, err := os.ReadDir(spec.path)
	if err != nil {
		return nil, err
	}
	result := make([]artifactEntry, 0, len(entries))
	for _, entry := range entries {
		if !canonicalArtifactName.MatchString(entry.Name()) {
			continue
		}
		path := filepath.Join(spec.path, entry.Name())
		entryInfo, err := os.Lstat(path)
		if err != nil {
			return nil, err
		}
		if entryInfo.Mode()&os.ModeSymlink != 0 || !entryInfo.Mode().IsRegular() || entryInfo.Mode().Perm()&0o022 != 0 || fileOwner(entryInfo) != c.ownerUID {
			return nil, errors.New("Agent runtime artifact is unsafe")
		}
		result = append(result, artifactEntry{name: entry.Name(), info: entryInfo})
	}
	return result, nil
}

func (c Cleaner) cleanup(cutoff time.Time, requested []Candidate) (int64, int64, error) {
	current, _, err := c.analyze(cutoff)
	if err != nil {
		return 0, 0, err
	}
	allowed := make(map[Candidate]struct{}, len(current))
	for _, candidate := range current {
		allowed[candidate] = struct{}{}
	}
	seen := make(map[Candidate]struct{}, len(requested))
	for _, candidate := range requested {
		if _, duplicate := seen[candidate]; duplicate {
			return 0, 0, errors.New("duplicate Agent runtime cleanup candidate")
		}
		seen[candidate] = struct{}{}
		spec, validRoot := c.roots[candidate.RootID]
		if !validRoot || !canonicalArtifactName.MatchString(candidate.Name) {
			return 0, 0, errors.New("invalid Agent runtime cleanup candidate")
		}
		if _, err := os.Lstat(filepath.Join(spec.path, candidate.Name)); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return 0, 0, err
		}
		if _, ok := allowed[candidate]; !ok {
			return 0, 0, errors.New("Agent runtime cleanup candidate changed")
		}
	}
	var deleted, reclaimed int64
	for _, candidate := range requested {
		spec := c.roots[candidate.RootID]
		path := filepath.Join(spec.path, candidate.Name)
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return deleted, reclaimed, err
		}
		if err := os.Remove(path); err != nil {
			return deleted, reclaimed, err
		}
		deleted++
		reclaimed += info.Size()
	}
	return deleted, reclaimed, nil
}

func (c Cleaner) verify(candidates []Candidate) (int64, error) {
	var remaining int64
	for _, candidate := range candidates {
		spec, ok := c.roots[candidate.RootID]
		if !ok || !canonicalArtifactName.MatchString(candidate.Name) {
			return 0, errors.New("invalid Agent runtime cleanup candidate")
		}
		if _, err := os.Lstat(filepath.Join(spec.path, candidate.Name)); err == nil {
			remaining++
		} else if !os.IsNotExist(err) {
			return 0, err
		}
	}
	return remaining, nil
}

func fileOwner(info os.FileInfo) uint32 {
	if value, ok := info.Sys().(*syscall.Stat_t); ok {
		return value.Uid
	}
	return ^uint32(0)
}
