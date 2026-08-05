package fileutil

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestCommitTemporaryDoesNotReplaceConcurrentTarget(t *testing.T) {
	dir := t.TempDir()
	temporaryPath := filepath.Join(dir, "temporary")
	targetPath := filepath.Join(dir, "target")
	if err := os.WriteFile(temporaryPath, []byte("generated"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(targetPath, []byte("concurrent"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := commitTemporary(temporaryPath, targetPath, false)
	if err == nil || !IsTargetError(err) || !errors.Is(err, os.ErrExist) {
		t.Fatalf("commitTemporary() error = %v", err)
	}
	data, readErr := os.ReadFile(targetPath)
	if readErr != nil || string(data) != "concurrent" {
		t.Fatalf("target = %q, err = %v", data, readErr)
	}
}

func TestCommitTemporaryForceReplacesTarget(t *testing.T) {
	dir := t.TempDir()
	temporaryPath := filepath.Join(dir, "temporary")
	targetPath := filepath.Join(dir, "target")
	if err := os.WriteFile(temporaryPath, []byte("generated"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(targetPath, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := commitTemporary(temporaryPath, targetPath, true); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(targetPath)
	if err != nil || string(data) != "generated" {
		t.Fatalf("target = %q, err = %v", data, err)
	}
}
