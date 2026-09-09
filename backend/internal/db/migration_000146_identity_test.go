package db

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestMigration000146Identity(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{
			name: "000146_managed_routing_rule_sets.up.sql",
			want: "9634575c7f05a82e30258501f44ea0f2f16698d7a0e3ea1107f105a8b5e4d4f9",
		},
		{
			name: "000146_managed_routing_rule_sets.down.sql",
			want: "7472d22d3adb15c199f5ef10f168419dc3496cd42be36d0dd7f3e186c4835ea0",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("..", "..", "migrations", test.name))
			if err != nil {
				t.Fatalf("read canonical migration: %v", err)
			}
			got := fmt.Sprintf("%x", sha256.Sum256(data))
			if got != test.want {
				t.Fatalf("migration identity changed: sha256=%s, want %s", got, test.want)
			}
		})
	}
}
