package config

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadClientArchivedSessionsDefaultAndExplicit(t *testing.T) {
	for _, test := range []struct {
		name      string
		field     string
		wantValue bool
	}{
		{name: "default disabled"},
		{name: "explicit disabled", field: `,"include_archived_sessions":false`},
		{name: "explicit enabled", field: `,"include_archived_sessions":true`, wantValue: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "client.json")
			content := fmt.Sprintf(`{"device_id":"device","server_url":"https://tracehub.example.com","codex_dir":"/codex"%s,"device_key_id":"device-1","device_private_key":"device.key","server_key_id":"server-1","server_public_key":"server.pub"}`, test.field)
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			cfg, err := LoadClient(path)
			if err != nil {
				t.Fatal(err)
			}
			if cfg.IncludeArchivedSessions != test.wantValue {
				t.Fatalf("include_archived_sessions=%v, want %v", cfg.IncludeArchivedSessions, test.wantValue)
			}
		})
	}
}
