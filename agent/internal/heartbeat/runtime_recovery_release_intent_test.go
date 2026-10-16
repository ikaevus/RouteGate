package heartbeat

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ikaevus/routegate/agent/internal/tasks"
)

func TestFencedDispatchReleaseIntentSurvivesFlagRemoval(t *testing.T) {
	for _, ready := range []bool{false, true} {
		for _, data := range []string{"", "{}", "malformed"} {
			t.Run(data+map[bool]string{false: "/unnegotiated", true: "/negotiated"}[ready], func(t *testing.T) {
				r, a, results := fencedRunnerFixture(t, tasks.TaskKindVPNCoreService, 204)
				r.cfg.ExperimentalRuntimeMutationFencing = false
				r.runtimeMutationManagerReady = ready
				if err := os.Mkdir(r.runtimeMutationDir, 0700); err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(r.runtimeMutationDir, "recovery-fence-release-intent.json")
				if err := os.WriteFile(path, []byte(data), 0600); err != nil {
					t.Fatal(err)
				}
				if err := r.processNextTask(context.Background()); !errors.Is(err, tasks.ErrRuntimeMutationBlocked) {
					t.Fatal("standalone intent ignored after flag removal", err)
				}
				if a.calls != 0 || results.Load() != 0 {
					t.Fatal("intent-only state mutated runtime or invented result")
				}
				got, err := os.ReadFile(path)
				if err != nil || string(got) != data {
					t.Fatal("intent changed", err)
				}
			})
		}
	}
}
