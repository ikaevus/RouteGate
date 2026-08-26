package tasks

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

const (
	platformUpdateReceiptRoot          = "/var/lib/routegate-agent/update-receipts"
	platformUpdateReceiptSchemaVersion = 1
	platformUpdateReceiptMaxBytes      = 4096
)

type PlatformUpdateReceiptPhase string

const (
	PlatformUpdateReceiptPrepared        PlatformUpdateReceiptPhase = "prepared"
	PlatformUpdateReceiptMutationStarted PlatformUpdateReceiptPhase = "mutation_started"
	PlatformUpdateReceiptSucceeded       PlatformUpdateReceiptPhase = "succeeded"
	PlatformUpdateReceiptFailed          PlatformUpdateReceiptPhase = "failed"
	PlatformUpdateReceiptOutcomeUnknown  PlatformUpdateReceiptPhase = "outcome_unknown"
)

type PlatformUpdateReceipt struct {
	SchemaVersion   int                        `json:"schemaVersion"`
	TaskID          string                     `json:"taskId"`
	TargetVersion   string                     `json:"targetVersion"`
	Phase           PlatformUpdateReceiptPhase `json:"phase"`
	MutationStarted bool                       `json:"mutationStarted"`
	Code            string                     `json:"code,omitempty"`
	CreatedAt       time.Time                  `json:"createdAt"`
	UpdatedAt       time.Time                  `json:"updatedAt"`
}

type platformUpdateReceiptStore struct {
	root     string
	ownerUID uint32
	now      func() time.Time
}

func fixedPlatformUpdateReceiptStore() platformUpdateReceiptStore {
	return platformUpdateReceiptStore{root: platformUpdateReceiptRoot, ownerUID: 0, now: time.Now}
}

func (s platformUpdateReceiptStore) CreatePrepared(taskID, targetVersion string) (PlatformUpdateReceipt, error) {
	if !canonicalTaskIDPattern.MatchString(taskID) {
		return PlatformUpdateReceipt{}, fmt.Errorf("platform update task id must be canonical UUIDv4")
	}
	if !routeGateReleaseVersionPattern.MatchString(targetVersion) {
		return PlatformUpdateReceipt{}, fmt.Errorf("invalid RouteGate target release version")
	}
	if err := s.ensureRoot(); err != nil {
		return PlatformUpdateReceipt{}, err
	}
	now := s.now().UTC()
	receipt := PlatformUpdateReceipt{
		SchemaVersion: platformUpdateReceiptSchemaVersion,
		TaskID:        taskID,
		TargetVersion: targetVersion,
		Phase:         PlatformUpdateReceiptPrepared,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := s.writeAtomic(receipt, false); err != nil {
		return PlatformUpdateReceipt{}, err
	}
	return receipt, nil
}

func (s platformUpdateReceiptStore) MarkMutationStarted(taskID string) (PlatformUpdateReceipt, error) {
	return s.transition(taskID, PlatformUpdateReceiptMutationStarted, true, "")
}

func (s platformUpdateReceiptStore) MarkSucceeded(taskID string) (PlatformUpdateReceipt, error) {
	return s.transition(taskID, PlatformUpdateReceiptSucceeded, true, "")
}

func (s platformUpdateReceiptStore) MarkFailed(taskID, code string) (PlatformUpdateReceipt, error) {
	if !validPlatformUpdateReceiptCode(code) {
		return PlatformUpdateReceipt{}, fmt.Errorf("invalid platform update receipt code")
	}
	return s.transition(taskID, PlatformUpdateReceiptFailed, true, code)
}

func (s platformUpdateReceiptStore) MarkPreDispatchFailed(taskID, code string) (PlatformUpdateReceipt, error) {
	if !validPlatformUpdateReceiptCode(code) {
		return PlatformUpdateReceipt{}, fmt.Errorf("invalid platform update receipt code")
	}
	return s.transition(taskID, PlatformUpdateReceiptFailed, false, code)
}

func (s platformUpdateReceiptStore) MarkOutcomeUnknown(taskID, code string) (PlatformUpdateReceipt, error) {
	if !validPlatformUpdateReceiptCode(code) {
		return PlatformUpdateReceipt{}, fmt.Errorf("invalid platform update receipt code")
	}
	return s.transition(taskID, PlatformUpdateReceiptOutcomeUnknown, true, code)
}

func (s platformUpdateReceiptStore) ReconcileInterrupted(taskID string) (PlatformUpdateReceipt, error) {
	receipt, err := s.Read(taskID)
	if err != nil {
		return PlatformUpdateReceipt{}, err
	}
	if receipt.Phase != PlatformUpdateReceiptMutationStarted {
		return receipt, nil
	}
	return s.MarkOutcomeUnknown(taskID, "agent_restart_after_mutation_started")
}

func (s platformUpdateReceiptStore) Read(taskID string) (PlatformUpdateReceipt, error) {
	if !canonicalTaskIDPattern.MatchString(taskID) {
		return PlatformUpdateReceipt{}, fmt.Errorf("platform update task id must be canonical UUIDv4")
	}
	if err := s.validateRoot(); err != nil {
		return PlatformUpdateReceipt{}, err
	}
	path := s.path(taskID)
	info, err := os.Lstat(path)
	if err != nil {
		return PlatformUpdateReceipt{}, err
	}
	if err := s.validateOwnedRegular(info); err != nil {
		return PlatformUpdateReceipt{}, fmt.Errorf("unsafe platform update receipt: %w", err)
	}
	if info.Size() <= 0 || info.Size() > platformUpdateReceiptMaxBytes {
		return PlatformUpdateReceipt{}, fmt.Errorf("platform update receipt size is invalid")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return PlatformUpdateReceipt{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var receipt PlatformUpdateReceipt
	if err := decoder.Decode(&receipt); err != nil {
		return PlatformUpdateReceipt{}, fmt.Errorf("decode platform update receipt: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return PlatformUpdateReceipt{}, fmt.Errorf("decode platform update receipt: %w", err)
	}
	if err := validatePlatformUpdateReceiptContract(receipt, taskID); err != nil {
		return PlatformUpdateReceipt{}, err
	}
	return receipt, nil
}

// transition is serialized across detached workers and the Agent dispatch path.
// The lock prevents a stale prepared read from overwriting a concurrent
// mutation_started receipt and thereby erasing the durable at-most-once proof.
func (s platformUpdateReceiptStore) transition(taskID string, phase PlatformUpdateReceiptPhase, mutationStarted bool, code string) (PlatformUpdateReceipt, error) {
	lock, err := s.acquireTransitionLock()
	if err != nil {
		return PlatformUpdateReceipt{}, err
	}
	defer func() {
		_ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
		_ = lock.Close()
	}()

	receipt, err := s.Read(taskID)
	if err != nil {
		return PlatformUpdateReceipt{}, err
	}
	if !allowedPlatformUpdateReceiptTransition(receipt.Phase, phase) {
		return PlatformUpdateReceipt{}, fmt.Errorf("invalid platform update receipt transition %s -> %s", receipt.Phase, phase)
	}
	if receipt.MutationStarted && !mutationStarted {
		return PlatformUpdateReceipt{}, fmt.Errorf("platform update mutation-started evidence cannot be cleared")
	}
	if phase == PlatformUpdateReceiptFailed {
		if mutationStarted && receipt.Phase != PlatformUpdateReceiptMutationStarted {
			return PlatformUpdateReceipt{}, fmt.Errorf("post-dispatch failure requires mutation_started receipt")
		}
		if !mutationStarted && receipt.Phase != PlatformUpdateReceiptPrepared {
			return PlatformUpdateReceipt{}, fmt.Errorf("pre-dispatch failure requires prepared receipt")
		}
	}

	receipt.Phase = phase
	receipt.MutationStarted = mutationStarted
	receipt.Code = code
	receipt.UpdatedAt = s.now().UTC()
	if err := validatePlatformUpdateReceiptContract(receipt, taskID); err != nil {
		return PlatformUpdateReceipt{}, err
	}
	if err := s.writeAtomic(receipt, true); err != nil {
		return PlatformUpdateReceipt{}, err
	}
	return receipt, nil
}

func (s platformUpdateReceiptStore) acquireTransitionLock() (*os.File, error) {
	if err := s.ensureRoot(); err != nil {
		return nil, err
	}
	path := filepath.Join(s.root, ".transition.lock")
	lock, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("open platform update receipt transition lock: %w", err)
	}
	info, err := lock.Stat()
	if err != nil {
		_ = lock.Close()
		return nil, err
	}
	if err := s.validateOwnedRegular(info); err != nil {
		_ = lock.Close()
		return nil, fmt.Errorf("unsafe platform update receipt transition lock: %w", err)
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		_ = lock.Close()
		return nil, fmt.Errorf("lock platform update receipt transition: %w", err)
	}
	return lock, nil
}

func (s platformUpdateReceiptStore) path(taskID string) string {
	return filepath.Join(s.root, taskID+".json")
}

func (s platformUpdateReceiptStore) ensureRoot() error {
	if err := os.Mkdir(s.root, 0700); err != nil && !os.IsExist(err) {
		return fmt.Errorf("create platform update receipt root: %w", err)
	}
	info, err := os.Lstat(s.root)
	if err != nil {
		return fmt.Errorf("inspect platform update receipt root: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("platform update receipt root is unsafe")
	}
	if err := s.validateOwnerMode(info); err != nil {
		return err
	}
	if info.Mode().Perm() != 0700 {
		return fmt.Errorf("platform update receipt root mode must be 0700")
	}
	return nil
}

func (s platformUpdateReceiptStore) validateRoot() error {
	info, err := os.Lstat(s.root)
	if err != nil {
		return fmt.Errorf("inspect platform update receipt root: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("platform update receipt root is unsafe")
	}
	if info.Mode().Perm() != 0700 {
		return fmt.Errorf("platform update receipt root mode must be 0700")
	}
	return s.validateOwnerMode(info)
}

func (s platformUpdateReceiptStore) validateOwnedRegular(info os.FileInfo) error {
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("not a regular file")
	}
	return s.validateOwnerMode(info)
}

func (s platformUpdateReceiptStore) validateOwnerMode(info os.FileInfo) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("filesystem ownership metadata unavailable")
	}
	if stat.Uid != s.ownerUID {
		return fmt.Errorf("unexpected owner")
	}
	if info.Mode().Perm()&0077 != 0 {
		return fmt.Errorf("group/world accessible")
	}
	return nil
}

func (s platformUpdateReceiptStore) writeAtomic(receipt PlatformUpdateReceipt, replace bool) error {
	if err := s.ensureRoot(); err != nil {
		return err
	}
	data, err := json.Marshal(receipt)
	if err != nil {
		return err
	}
	if len(data) == 0 || len(data) > platformUpdateReceiptMaxBytes {
		return fmt.Errorf("platform update receipt exceeds bounded size")
	}
	tmp, err := os.CreateTemp(s.root, ".receipt-*")
	if err != nil {
		return fmt.Errorf("create platform update receipt temp file: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		_ = tmp.Close()
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := tmp.Chmod(0600); err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	path := s.path(receipt.TaskID)
	if replace {
		if err := os.Rename(tmpPath, path); err != nil {
			return err
		}
		cleanup = false
	} else {
		if err := os.Link(tmpPath, path); err != nil {
			if os.IsExist(err) {
				return fmt.Errorf("platform update receipt already exists")
			}
			return err
		}
		if err := os.Remove(tmpPath); err != nil {
			_ = os.Remove(path)
			return err
		}
		cleanup = false
	}
	dir, err := os.Open(s.root)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("multiple JSON values are not allowed")
		}
		return err
	}
	return nil
}

func validatePlatformUpdateReceiptContract(receipt PlatformUpdateReceipt, taskID string) error {
	if receipt.SchemaVersion != platformUpdateReceiptSchemaVersion ||
		receipt.TaskID != taskID ||
		!routeGateReleaseVersionPattern.MatchString(receipt.TargetVersion) ||
		!validPlatformUpdateReceiptPhase(receipt.Phase) {
		return fmt.Errorf("platform update receipt contract is invalid")
	}
	if receipt.CreatedAt.IsZero() || receipt.UpdatedAt.IsZero() || receipt.UpdatedAt.Before(receipt.CreatedAt) {
		return fmt.Errorf("platform update receipt timestamps are invalid")
	}
	switch receipt.Phase {
	case PlatformUpdateReceiptPrepared:
		if receipt.MutationStarted || receipt.Code != "" {
			return fmt.Errorf("platform update prepared receipt is inconsistent")
		}
	case PlatformUpdateReceiptMutationStarted, PlatformUpdateReceiptSucceeded:
		if !receipt.MutationStarted || receipt.Code != "" {
			return fmt.Errorf("platform update receipt phase is inconsistent")
		}
	case PlatformUpdateReceiptFailed:
		if !validPlatformUpdateReceiptCode(receipt.Code) {
			return fmt.Errorf("platform update terminal receipt is inconsistent")
		}
	case PlatformUpdateReceiptOutcomeUnknown:
		if !receipt.MutationStarted || !validPlatformUpdateReceiptCode(receipt.Code) {
			return fmt.Errorf("platform update terminal receipt is inconsistent")
		}
	}
	return nil
}

func allowedPlatformUpdateReceiptTransition(from, to PlatformUpdateReceiptPhase) bool {
	switch from {
	case PlatformUpdateReceiptPrepared:
		return to == PlatformUpdateReceiptMutationStarted || to == PlatformUpdateReceiptFailed
	case PlatformUpdateReceiptMutationStarted:
		return to == PlatformUpdateReceiptSucceeded || to == PlatformUpdateReceiptFailed || to == PlatformUpdateReceiptOutcomeUnknown
	default:
		return false
	}
}

func validPlatformUpdateReceiptPhase(phase PlatformUpdateReceiptPhase) bool {
	switch phase {
	case PlatformUpdateReceiptPrepared,
		PlatformUpdateReceiptMutationStarted,
		PlatformUpdateReceiptSucceeded,
		PlatformUpdateReceiptFailed,
		PlatformUpdateReceiptOutcomeUnknown:
		return true
	default:
		return false
	}
}

func validPlatformUpdateReceiptCode(code string) bool {
	if len(code) == 0 || len(code) > 64 {
		return false
	}
	for _, r := range code {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			continue
		}
		return false
	}
	return true
}
