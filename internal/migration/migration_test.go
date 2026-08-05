package migration_test

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"

	"github.com/jfox85/supawake/internal/migration"
)

func TestInstallIsCollisionSafeAndLeastPrivilege(t *testing.T) {
	sql := migration.InstallSQL()
	required := []string{
		"supawake:heartbeat:v1",
		"to_regclass('public.supawake_heartbeat')",
		"obj_description(c.oid, 'pg_class')",
		"raise exception",
		"revoke all on table public.supawake_heartbeat",
		"public, anon, authenticated, service_role",
		"grant select (id)",
		`create policy "supawake_read_heartbeat"`,
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
		"supawake:heartbeat:v1",
		"refusing to remove non-Supawake object",
		"drop table public.supawake_heartbeat",
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
		"install":   {migration.InstallSQL(), "ce99d969d28ba7952af6c7bbea1f971860cf7735a225fd755425f2688aef48ed"},
		"uninstall": {migration.UninstallSQL(), "705c238de5053fe43efa8189b5adbbca4991adbd0532e68dcde391056ccb9874"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if tt.sql == "" || !strings.HasSuffix(tt.sql, "\n") || strings.Contains(tt.sql, "supawake_heartbeat_is_valid_v1") {
				t.Fatalf("invalid generated SQL")
			}
			got := fmt.Sprintf("%x", sha256.Sum256([]byte(tt.sql)))
			if got != tt.hash {
				t.Fatalf("golden hash changed: %s", got)
			}
		})
	}
}
