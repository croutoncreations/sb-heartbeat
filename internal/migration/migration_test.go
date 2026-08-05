package migration_test

import (
	"crypto/sha256"
	"fmt"
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

func TestGeneratedSQLIsDeterministicAndTerminated(t *testing.T) {
	tests := map[string]struct {
		sql  string
		hash string
	}{
		"install":   {migration.InstallSQL(), "8e014da925747719a787d7b5a51f67c4bd39d76a2d0ecfe52685360513809dc7"},
		"uninstall": {migration.UninstallSQL(), "be5e483b3ee9cc02c8f8d622d6114493de0ffca5dfe27a555a66b00d59d3dc82"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if tt.sql == "" || !strings.HasSuffix(tt.sql, "\n") || strings.Contains(tt.sql, "sb_heartbeat_is_valid_v1") {
				t.Fatalf("invalid generated SQL")
			}
			got := fmt.Sprintf("%x", sha256.Sum256([]byte(tt.sql)))
			if got != tt.hash {
				t.Fatalf("golden hash changed: %s", got)
			}
		})
	}
}
