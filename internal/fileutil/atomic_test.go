package fileutil_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/croutoncreations/sb-heartbeat/internal/fileutil"
)

func TestWriteAtomicRefusesSymlinkEvenWithForce(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	link := filepath.Join(dir, "link")
	if err := os.WriteFile(target, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := fileutil.WriteAtomic(link, []byte("replacement"), 0o600, true); err == nil {
		t.Fatal("WriteAtomic() error = nil")
	}
	data, _ := os.ReadFile(target)
	if string(data) != "original" {
		t.Fatalf("target = %q", data)
	}
}

func TestWriteAtomicForceReplacesRegularFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := fileutil.WriteAtomic(path, []byte("new"), 0o640, true); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "new" {
		t.Fatalf("file = %q", data)
	}
}
