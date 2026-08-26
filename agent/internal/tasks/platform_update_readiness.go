package tasks

import (
	"fmt"
	"os"
	"runtime"
	"syscall"
)

const (
	platformUpdateSystemctlPath = "/usr/bin/systemctl"
)

var fixedPlatformUpdateReadinessExecutables = []string{
	platformUpdateSystemdRun,
	platformUpdateSystemctlPath,
	platformUpdateAgentBinary,
	platformUpdateVerifiedUpdater,
}

// PlatformUpdateRuntimeReady reports only whether this Agent host has the fixed
// local execution prerequisites required by the already-reviewed remote update
// path. It does not verify a release and does not authorize mutation.
func PlatformUpdateRuntimeReady() bool {
	return platformUpdateRuntimeReady(os.Geteuid(), runtime.GOARCH, 0, fixedPlatformUpdateReadinessExecutables)
}

func platformUpdateRuntimeReady(euid int, arch string, expectedUID uint32, executables []string) bool {
	if euid != 0 {
		return false
	}
	if arch != "amd64" && arch != "arm64" {
		return false
	}
	if len(executables) != 4 {
		return false
	}
	for _, path := range executables {
		if err := validatePlatformUpdateReadinessExecutable(path, expectedUID); err != nil {
			return false
		}
	}
	return true
}

func validatePlatformUpdateReadinessExecutable(path string, expectedUID uint32) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("not a regular file")
	}
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
	if info.Mode().Perm()&0111 == 0 {
		return fmt.Errorf("not executable")
	}
	return nil
}
