package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVersionCompatibilityIntegrationExercisesPublishedLegacyReader(t *testing.T) {
	script, err := os.ReadFile(filepath.Join("..", "..", "scripts", "integration-version-compatibility.sh"))
	if err != nil {
		t.Fatalf("read compatibility integration: %v", err)
	}
	text := string(script)
	for _, fragment := range []string{
		"set -euo pipefail",
		"legacy_binary",
		"current_binary",
		"init --non-interactive",
		"--sb-heartbeat-version v0.1.1",
		"install github",
		"Version compatibility integration: PASS",
	} {
		if !strings.Contains(text, fragment) {
			t.Errorf("compatibility integration missing %q", fragment)
		}
	}
}

func TestVersionCompatibilityIntegrationRunsInCIWithPinnedReleaseChecksum(t *testing.T) {
	workflow, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "test.yml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(workflow)
	for _, fragment := range []string{
		"version-compatibility:",
		"sb-heartbeat_0.1.1_linux_amd64.tar.gz",
		"8785aa8bb3c0549b204bd9e3f5720412fa05af8f06141f0070f48be97221b314",
		"sha256sum --check",
		"scripts/integration-version-compatibility.sh",
	} {
		if !strings.Contains(text, fragment) {
			t.Errorf("test workflow missing compatibility gate %q", fragment)
		}
	}
}
