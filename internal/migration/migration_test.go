package migration_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jfox85/sb-heartbeat/internal/migration"
)

func TestInstallIsCollisionSafeAndLeastPrivilege(t *testing.T) {
	sql := migration.InstallSQL()
	required := []string{
		"sb-heartbeat:managed:v1",
		"to_regclass('public.sb_heartbeat')",
		"obj_description(c.oid, 'pg_class')",
		"raise exception",
		"revoke all on table public.sb_heartbeat",
		"public, anon, authenticated, service_role",
		"grant select (id)",
		`create policy "sb_heartbeat_read"`,
	}
	for _, fragment := range required {
		if !strings.Contains(strings.ToLower(sql), strings.ToLower(fragment)) {
			t.Errorf("install SQL missing %q", fragment)
		}
	}
	if strings.Contains(strings.ToLower(sql), "create table if not exists") {
		t.Error("install SQL uses unsafe CREATE TABLE IF NOT EXISTS")
	}
}

func TestUninstallRefusesUnownedObject(t *testing.T) {
	sql := migration.UninstallSQL()
	for _, fragment := range []string{
		"sb-heartbeat:managed:v1",
		"refusing to remove non-SB Heartbeat object",
		"drop table public.sb_heartbeat",
	} {
		if !strings.Contains(sql, fragment) {
			t.Errorf("uninstall SQL missing %q", fragment)
		}
	}
	if strings.Contains(strings.ToLower(sql), "drop table if exists") {
		t.Error("uninstall SQL performs an unguarded drop")
	}
}

func TestGeneratedSQLMatchesGoldenArtifacts(t *testing.T) {
	tests := map[string]string{
		"install":   migration.InstallSQL(),
		"uninstall": migration.UninstallSQL(),
	}
	for name, generated := range tests {
		t.Run(name, func(t *testing.T) {
			if generated == "" || !strings.HasSuffix(generated, "\n") || strings.Contains(generated, "sb_heartbeat_is_valid_v1") {
				t.Fatalf("invalid generated SQL")
			}
			goldenPath := filepath.Join("testdata", name+".sql.golden")
			golden, err := os.ReadFile(goldenPath)
			if err != nil {
				t.Fatal(err)
			}
			if generated != string(golden) {
				t.Fatalf("generated SQL differs from %s", goldenPath)
			}
		})
	}
}
