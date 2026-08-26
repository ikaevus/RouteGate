package tasks

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
)

const platformUpdateSystemctlPath = "/usr/bin/systemctl"

var fixedPlatformUpdateReadinessExecutables = []string{
	platformUpdateSystemdRun,
	platformUpdateSystemctlPath,
	platformUpdateAgentBinary,
	platformUpdateVerifiedUpdater,
}

func PlatformUpdateRuntimeReady() bool {
	return platformUpdateRuntimeReady(os.Geteuid(), runtime.GOARCH, 0, fixedPlatformUpdateReadinessExecutables)
}

func platformUpdateRuntimeReady(euid int, arch string, expectedUID uint32, executables []string) bool {
	if euid != 0 || (arch != "amd64" && arch != "arm64") || len(executables) != 4 {
		return false
	}
	for _, path := range executables {
		if err := validatePlatformUpdateReadinessExecutable(path, expectedUID); err != nil {
			return false
		}
	}
	return true
}

// validatePlatformUpdateReadinessExecutable validates both the executable and
// every directory used to reach it. A writable or symlinked parent would let a
// different principal replace a previously checked binary by rename.
func validatePlatformUpdateReadinessExecutable(path string, expectedUID uint32) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("not a regular file")
	}
	if err := validatePlatformUpdateOwnedNonWritable(info, expectedUID); err != nil {
		return err
	}
	if info.Mode().Perm()&0111 == 0 {
		return fmt.Errorf("not executable")
	}
	return validatePlatformUpdateParentChain(path, expectedUID)
}

func validatePlatformUpdateParentChain(path string, expectedUID uint32) error {
	clean := filepath.Clean(path)
	if !filepath.IsAbs(clean) {
		return fmt.Errorf("path is not absolute")
	}
	for dir := filepath.Dir(clean); ; dir = filepath.Dir(dir) {
		info, err := os.Lstat(dir)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("unsafe parent directory %s", dir)
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			return fmt.Errorf("unsafe parent directory %s: filesystem ownership metadata unavailable", dir)
		}
		// Production passes expectedUID=0. Allowing root ancestors as well as the
		// fixture owner keeps this helper testable as an unprivileged user without
		// weakening the production root-only policy.
		if stat.Uid != 0 && stat.Uid != expectedUID {
			return fmt.Errorf("unsafe parent directory %s: unexpected owner", dir)
		}
		if info.Mode().Perm()&0022 != 0 {
			return fmt.Errorf("unsafe parent directory %s: group/world writable", dir)
		}
		if dir == string(filepath.Separator) {
			break
		}
	}
	return nil
}

func validatePlatformUpdateOwnedNonWritable(info os.FileInfo, expectedUID uint32) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("filesystem ownership metadata unavailable")
	}
	if stat.Uid != expectedUID {
		return fmt.Errorf("unexpected owner")
	}
	if info.Mode().Perm()&0022 != 0 {
		return fmt.Errorf("group/world writable")
	}
	return nil
}
