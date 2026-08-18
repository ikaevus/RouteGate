package tasks

import (
	"context"
	"testing"

	"github.com/ikaevus/routegate/agent/internal/platform"
)

func TestSingBoxVLESSAdapterDescriptorMatchesManagedCapability(t *testing.T) {
	adapter := NewSingBoxVLESSAdapter(t.TempDir(), "sing-box", "sing-box")
	descriptor := adapter.Descriptor()
	if descriptor.Core != platform.VPNCoreSingBox || descriptor.Protocol != platform.VPNProtocolVLESS {
		t.Fatalf("unexpected adapter descriptor: %+v", descriptor)
	}
}

func TestSingBoxVLESSAdapterDelegatesStageAndValidation(t *testing.T) {
	adapter := NewSingBoxVLESSAdapter(t.TempDir(), "sing-box-test", "sing-box")
	result, err := adapter.Stage(ConfigTask{
		ID:              "task-id",
		ConfigVersionID: "version-id",
		RenderedConfig:  []byte(`{"schemaVersion":"routegate.config.v1","singBox":{"log":{"level":"info"}}}`),
	})
	if err != nil {
		t.Fatalf("stage through adapter: %v", err)
	}
	if result.ConfigVersionID != "version-id" || result.StagedPath == "" {
		t.Fatalf("unexpected stage result: %+v", result)
	}

	concrete := adapter.(singBoxVLESSAdapter)
	concrete.validator.run = func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name != "sing-box-test" || len(args) != 3 || args[0] != "check" || args[1] != "-c" {
			t.Fatalf("unexpected validation command: %s %v", name, args)
		}
		return nil, nil
	}
	if _, err := concrete.Validate(context.Background(), result.StagedPath); err != nil {
		t.Fatalf("validate through adapter: %v", err)
	}
}
