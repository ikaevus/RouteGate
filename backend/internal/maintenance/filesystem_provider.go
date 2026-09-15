package maintenance

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"syscall"
	"time"
)

const managerUpdateStagingRoot = "/var/lib/routegate-manager/update-staging"

var managerPartialName = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-5][0-9a-fA-F]{3}-[89aAbB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}\.partial$`)

type filesystemProvider struct {
	category Category
	root     string
	ownerUID int
}

func managerPartialProvider() Provider {
	return filesystemProvider{
		category: Category{ID: "manager_update_partials", Scope: "manager", Recommended: true, Selectable: true, RetentionDays: 1},
		root:     managerUpdateStagingRoot, ownerUID: os.Geteuid(),
	}
}

func (p filesystemProvider) Category() Category { return p.category }

func (p filesystemProvider) Analyze(_ context.Context, cutoff time.Time) (PlanItem, error) {
	names, bytes, err := p.candidates(cutoff)
	if err != nil {
		return PlanItem{}, err
	}
	metadata := map[string]any{}
	if len(names) > 0 {
		metadata["candidateNames"] = names
	}
	return PlanItem{
		CategoryID: p.category.ID, Scope: p.category.Scope, RetentionDays: p.category.RetentionDays,
		Cutoff: cutoff, CandidateCount: int64(len(names)), EstimatedBytes: bytes, Metadata: metadata,
	}, nil
}

func (p filesystemProvider) Cleanup(_ context.Context, item PlanItem) (CleanupResult, error) {
	if err := p.validateRoot(); err != nil {
		if os.IsNotExist(err) {
			return CleanupResult{}, nil
		}
		return CleanupResult{}, err
	}
	names, err := metadataNames(item.Metadata)
	if err != nil {
		return CleanupResult{}, err
	}
	var result CleanupResult
	for _, name := range names {
		if !managerPartialName.MatchString(name) {
			return result, ErrUnsafeOwnership
		}
		path := filepath.Join(p.root, name)
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || !info.ModTime().Before(item.Cutoff) {
			return result, ErrUnsafeOwnership
		}
		bytes, err := directoryBytes(path)
		if err != nil {
			return result, err
		}
		if err := os.RemoveAll(path); err != nil {
			return result, err
		}
		result.DeletedCount++
		result.ReclaimedBytes += bytes
	}
	return result, nil
}

func (p filesystemProvider) Verify(_ context.Context, item PlanItem) (int64, error) {
	names, err := metadataNames(item.Metadata)
	if err != nil {
		return 0, err
	}
	var remaining int64
	for _, name := range names {
		if _, err := os.Lstat(filepath.Join(p.root, name)); err == nil {
			remaining++
		} else if !os.IsNotExist(err) {
			return 0, err
		}
	}
	return remaining, nil
}

func (p filesystemProvider) candidates(cutoff time.Time) ([]string, int64, error) {
	if err := p.validateRoot(); err != nil {
		if os.IsNotExist(err) {
			return nil, 0, nil
		}
		return nil, 0, err
	}
	entries, err := os.ReadDir(p.root)
	if err != nil {
		return nil, 0, err
	}
	var names []string
	var bytes int64
	for _, entry := range entries {
		if !managerPartialName.MatchString(entry.Name()) || entry.Type()&os.ModeSymlink != 0 || !entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return nil, 0, err
		}
		if !info.ModTime().Before(cutoff) {
			continue
		}
		size, err := directoryBytes(filepath.Join(p.root, entry.Name()))
		if err != nil {
			return nil, 0, err
		}
		names = append(names, entry.Name())
		bytes += size
	}
	sort.Strings(names)
	return names, bytes, nil
}

func (p filesystemProvider) validateRoot() error {
	info, err := os.Lstat(p.root)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 {
		return ErrUnsafeOwnership
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) != p.ownerUID {
		return ErrUnsafeOwnership
	}
	return nil
}

func metadataNames(metadata map[string]any) ([]string, error) {
	value, ok := metadata["candidateNames"]
	if !ok {
		return nil, nil
	}
	values, ok := value.([]any)
	if ok {
		names := make([]string, 0, len(values))
		for _, value := range values {
			name, ok := value.(string)
			if !ok {
				return nil, ErrUnsafeOwnership
			}
			names = append(names, name)
		}
		return names, nil
	}
	if names, ok := value.([]string); ok {
		return names, nil
	}
	return nil, ErrUnsafeOwnership
}

func directoryBytes(root string) (int64, error) {
	var total int64
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		total += info.Size()
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("inspect maintenance candidate: %w", err)
	}
	return total, nil
}
