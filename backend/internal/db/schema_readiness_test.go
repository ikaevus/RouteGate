package db

import (
	"context"
	"testing"
)

func TestSchemaReadinessRejectsInvalidModeBeforeAccess(t *testing.T) {
	for _, mode := range []string{"externl", "EXTERNAL", "disabled", "external "} {
		if err := PrepareSchema(context.Background(), nil, "migrations", mode, nil); err == nil {
			t.Fatalf("accepted invalid mode %q", mode)
		}
	}
}

func TestSchemaReadinessRejectsEmptyManifestBeforeAccess(t *testing.T) {
	if err := CheckSchema(context.Background(), nil, t.TempDir()); err == nil {
		t.Fatal("empty manifest accepted")
	}
}
