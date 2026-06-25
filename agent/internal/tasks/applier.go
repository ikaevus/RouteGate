package tasks

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type ApplyResult struct {
	StagedPath      string `json:"stagedPath"`
	ActivePath      string `json:"activePath"`
	BackupPath      string `json:"backupPath,omitempty"`
	ConfigVersionID string `json:"configVersionId"`
}

type Applier struct {
	activePath string
	backupDir  string
}

func NewApplier(activePath, backupDir string) Applier {
	return Applier{activePath: activePath, backupDir: backupDir}
}

func (a Applier) Apply(stagedPath, configVersionID string) (ApplyResult, error) {
	stagedPath = strings.TrimSpace(stagedPath)
	configVersionID = strings.TrimSpace(configVersionID)
	if stagedPath == "" {
		return ApplyResult{}, fmt.Errorf("staged config path is required")
	}
	if configVersionID == "" {
		return ApplyResult{}, fmt.Errorf("config version id is required")
	}
	if strings.TrimSpace(a.activePath) == "" {
		return ApplyResult{}, fmt.Errorf("active config path is required")
	}
	if strings.TrimSpace(a.backupDir) == "" {
		return ApplyResult{}, fmt.Errorf("config backup dir is required")
	}
	if _, err := os.Stat(stagedPath); err != nil {
		return ApplyResult{}, fmt.Errorf("read staged config: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(a.activePath), 0o750); err != nil {
		return ApplyResult{}, fmt.Errorf("create active config dir: %w", err)
	}
	if err := os.MkdirAll(a.backupDir, 0o750); err != nil {
		return ApplyResult{}, fmt.Errorf("create config backup dir: %w", err)
	}

	result := ApplyResult{StagedPath: stagedPath, ActivePath: a.activePath, ConfigVersionID: configVersionID}
	if _, err := os.Stat(a.activePath); err == nil {
		backupPath := filepath.Join(a.backupDir, configVersionID+".previous.json")
		if err := copyFileAtomic(a.activePath, backupPath, 0o600); err != nil {
			return ApplyResult{}, fmt.Errorf("backup active config: %w", err)
		}
		result.BackupPath = backupPath
	} else if !os.IsNotExist(err) {
		return ApplyResult{}, fmt.Errorf("inspect active config: %w", err)
	}

	if err := copyFileAtomic(stagedPath, a.activePath, 0o600); err != nil {
		return ApplyResult{}, fmt.Errorf("promote staged config: %w", err)
	}
	return result, nil
}

func copyFileAtomic(src, dst string, perm os.FileMode) error {
	input, err := os.Open(src)
	if err != nil {
		return err
	}
	defer input.Close()

	tmpPath := dst + ".tmp"
	output, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr != nil {
		_ = os.Remove(tmpPath)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmpPath)
		return closeErr
	}
	if err := os.Chmod(tmpPath, perm); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, dst); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}
