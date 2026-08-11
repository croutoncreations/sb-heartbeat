package envfile_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/croutoncreations/sb-heartbeat/internal/envfile"
)

func TestLoadParsesLiteralValuesWithoutShellEvaluation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "heartbeat.env")
	contents := "# dedicated SB Heartbeat values\nPROJECT_URL=https://demo.supabase.co\nPROJECT_KEY=literal $HOME $(whoami) = padding\n\n"
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		if _, err := envfile.Load(path); err == nil {
			t.Fatal("Load() accepted an environment file where private permissions cannot be verified")
		}
		return
	}

	values, err := envfile.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := values["PROJECT_URL"]; got != "https://demo.supabase.co" {
		t.Fatalf("PROJECT_URL = %q", got)
	}
	if got := values["PROJECT_KEY"]; got != "literal $HOME $(whoami) = padding" {
		t.Fatalf("PROJECT_KEY = %q", got)
	}
}

func TestLoadRejectsUnsafeFilesAndSyntax(t *testing.T) {
	tests := map[string]string{
		"missing separator":        "PROJECT_URL\n",
		"invalid name":             "project-url=value\n",
		"duplicate":                "PROJECT_URL=one\nPROJECT_URL=two\n",
		"export syntax":            "export PROJECT_URL=value\n",
		"leading space":            " PROJECT_URL=value\n",
		"nul byte":                 "PROJECT_URL=value\x00tail\n",
		"embedded carriage return": "PROJECT_URL=value\rtail\n",
	}
	for name, contents := range tests {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "heartbeat.env")
			if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := envfile.Load(path); err == nil {
				t.Fatal("Load() error = nil")
			}
		})
	}
}

func TestLoadRejectsSymlinkNonRegularPermissiveAndOversizedFiles(t *testing.T) {
	dir := t.TempDir()
	regular := filepath.Join(dir, "regular.env")
	if err := os.WriteFile(regular, []byte("PROJECT_URL=value\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	symlink := filepath.Join(dir, "symlink.env")
	if err := os.Symlink(regular, symlink); err == nil {
		if _, err := envfile.Load(symlink); err == nil {
			t.Fatal("Load(symlink) error = nil")
		}
	}
	if _, err := envfile.Load(dir); err == nil {
		t.Fatal("Load(directory) error = nil")
	}
	if runtime.GOOS != "windows" {
		permissive := filepath.Join(dir, "permissive.env")
		if err := os.WriteFile(permissive, []byte("PROJECT_URL=value\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := envfile.Load(permissive); err == nil {
			t.Fatal("Load(permissive) error = nil")
		}
	}
	oversized := filepath.Join(dir, "oversized.env")
	if err := os.WriteFile(oversized, []byte("VALUE="+strings.Repeat("x", envfile.MaxFileBytes)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := envfile.Load(oversized); err == nil {
		t.Fatal("Load(oversized) error = nil")
	}
}
