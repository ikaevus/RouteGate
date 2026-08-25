package updates

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

var ErrStageCandidatePinned = errors.New("staged candidate is pinned by an apply with unresolved outcome")

type stageApplyPinner interface {
	Pin(string) (func() error, error)
}

type filesystemStageApplyPinner struct {
	stagingRoot string
}

func newStageApplyPinner(stagingRoot string) stageApplyPinner {
	return filesystemStageApplyPinner{stagingRoot: stagingRoot}
}

func (p filesystemStageApplyPinner) Pin(stageJobID string) (func() error, error) {
	if !canonicalUUIDv4Pattern.MatchString(stageJobID) {
		return nil, errors.New("stage job ID is not a canonical UUIDv4")
	}
	if err := validatePrivateDirectory(p.stagingRoot); err != nil {
		return nil, err
	}
	pinRoot := filepath.Join(p.stagingRoot, ".apply-pins")
	if err := os.MkdirAll(pinRoot, 0o700); err != nil {
		return nil, fmt.Errorf("create apply pin root: %w", err)
	}
	if err := validatePrivateDirectory(pinRoot); err != nil {
		return nil, err
	}
	pinPath := filepath.Join(pinRoot, stageJobID)
	file, err := os.OpenFile(pinPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, fmt.Errorf("create staged-candidate apply pin: %w", err)
	}
	if _, err := file.WriteString("apply-in-flight\n"); err != nil {
		_ = file.Close()
		_ = os.Remove(pinPath)
		return nil, fmt.Errorf("write staged-candidate apply pin: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		_ = os.Remove(pinPath)
		return nil, fmt.Errorf("sync staged-candidate apply pin: %w", err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(pinPath)
		return nil, fmt.Errorf("close staged-candidate apply pin: %w", err)
	}
	return func() error {
		if err := os.Remove(pinPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove staged-candidate apply pin: %w", err)
		}
		return nil
	}, nil
}

func stageCandidatePinned(stagingRoot, stageJobID string) (bool, error) {
	if !canonicalUUIDv4Pattern.MatchString(stageJobID) {
		return false, errors.New("stage job ID is not a canonical UUIDv4")
	}
	pinRoot := filepath.Join(stagingRoot, ".apply-pins")
	if _, err := os.Lstat(pinRoot); errors.Is(err, os.ErrNotExist) {
		return false, nil
	} else if err != nil {
		return false, fmt.Errorf("inspect staged-candidate pin root: %w", err)
	}
	if err := validatePrivateDirectory(pinRoot); err != nil {
		return false, fmt.Errorf("validate staged-candidate pin root: %w", err)
	}

	pinPath := filepath.Join(pinRoot, stageJobID)
	info, err := os.Lstat(pinPath)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect staged-candidate apply pin: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return false, errors.New("staged-candidate apply pin is unsafe")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) != os.Geteuid() {
		return false, errors.New("staged-candidate apply pin has unexpected owner")
	}
	return true, nil
}
