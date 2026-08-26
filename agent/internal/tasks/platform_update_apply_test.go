package tasks

import (
	"reflect"
	"strings"
	"testing"
)

func TestDetachedPlatformUpdateCommandIsFixedPolicy(t *testing.T) {
	taskID := "550e8400-e29b-41d4-a716-446655440000"
	path, argv, err := DetachedPlatformUpdateCommand(taskID)
	if err != nil {
		t.Fatalf("DetachedPlatformUpdateCommand: %v", err)
	}
	if path != "/usr/bin/systemd-run" {
		t.Fatalf("unexpected systemd-run path %q", path)
	}
	want := []string{
		"/usr/bin/systemd-run",
		"--unit=routegate-vpn-update-" + taskID,
		"--collect",
		"--no-block",
		"--property=UMask=0077",
		"--property=NoNewPrivileges=yes",
		"/usr/local/bin/routegate-agent",
		"--platform-update-worker-task=" + taskID,
	}
	if !reflect.DeepEqual(argv, want) {
		t.Fatalf("unexpected detached argv\n got: %#v\nwant: %#v", argv, want)
	}
	joined := strings.Join(argv, " ")
	for _, forbidden := range []string{"http://", "https://", "--role", "--bundle", "--manifest", "sh -c", "bash -c"} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("detached command contains caller-controlled privileged selector %q: %s", forbidden, joined)
		}
	}
}

func TestDetachedPlatformUpdateCommandRejectsNonCanonicalIDs(t *testing.T) {
	for _, taskID := range []string{
		"",
		"not-a-uuid",
		"550E8400-E29B-41D4-A716-446655440000",
		"550e8400-e29b-11d4-a716-446655440000",
		"550e8400-e29b-41d4-c716-446655440000",
		"../../etc/passwd",
	} {
		if _, _, err := DetachedPlatformUpdateCommand(taskID); err == nil {
			t.Fatalf("expected task ID %q to be rejected", taskID)
		}
	}
}

func TestCanonicalPlatformUpdateBundleNames(t *testing.T) {
	for _, name := range []string{
		"routegate-v1.2.3-linux-amd64.tar.gz",
		"routegate-1.2.3-rc.1-linux-arm64.tar.gz",
	} {
		if !isCanonicalPlatformUpdateBundleName(name) {
			t.Fatalf("expected canonical bundle %q", name)
		}
	}
	for _, name := range []string{
		"routegate-v1.2.3-linux-386.tar.gz",
		"../routegate-v1.2.3-linux-amd64.tar.gz",
		"routegate--linux-amd64.tar.gz",
		"evil-routegate-v1.2.3-linux-amd64.tar.gz",
	} {
		if isCanonicalPlatformUpdateBundleName(name) {
			t.Fatalf("expected non-canonical bundle %q to be rejected", name)
		}
	}
}
