package scheduler_test

import (
	"strings"
	"testing"

	"github.com/jfox85/sb-heartbeat/internal/config"
	"github.com/jfox85/sb-heartbeat/internal/scheduler"
)

func workflowConfig(t *testing.T) config.Config {
	t.Helper()
	cfg, err := config.New(config.Project{
		Name:   "demo",
		URL:    config.EnvReference{Env: "DEMO_URL"},
		APIKey: config.EnvReference{Env: "DEMO_KEY"},
	}, config.DefaultCron)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestGitHubWorkflowIsPinnedAndChecksumVerified(t *testing.T) {
	workflow, err := scheduler.GitHub(workflowConfig(t), "v0.1.0", "config/sb-heartbeat.yaml")
	if err != nil {
		t.Fatal(err)
	}
	text := string(workflow)
	required := []string{
		`cron: "37 3,11,19 * * *"`,
		"workflow_dispatch:",
		"permissions:\n  contents: read",
		"actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1",
		"SB_HEARTBEAT_VERSION: v0.1.0",
		"checksums.txt",
		"sha256sum --check --strict",
		"DEMO_URL: ${{ vars.DEMO_URL }}",
		"DEMO_KEY: ${{ secrets.DEMO_KEY }}",
		"sb-heartbeat --config config/sb-heartbeat.yaml run --output json",
		"GITHUB_STEP_SUMMARY",
	}
	for _, fragment := range required {
		if !strings.Contains(text, fragment) {
			t.Errorf("workflow missing %q\n%s", fragment, text)
		}
	}
	if strings.Contains(text, "curl | sh") || strings.Contains(text, "@v7") {
		t.Errorf("workflow contains an unsafe floating installer/action\n%s", text)
	}
}

func TestGitHubWorkflowRejectsDevelopmentOrInvalidVersion(t *testing.T) {
	for _, version := range []string{"", "devel", "v0.1.0-dev", "latest", "v1; echo bad"} {
		if _, err := scheduler.GitHub(workflowConfig(t), version, "sb-heartbeat.yaml"); err == nil {
			t.Fatalf("GitHub(version=%q) error = nil", version)
		}
	}
}

func TestGitHubWorkflowRejectsUnsafeConfigPath(t *testing.T) {
	for _, configPath := range []string{"", "/tmp/sb-heartbeat.yaml", "../sb-heartbeat.yaml", "./sb-heartbeat.yaml", "-config"} {
		if _, err := scheduler.GitHub(workflowConfig(t), "v0.1.0", configPath); err == nil {
			t.Fatalf("GitHub(configPath=%q) error = nil", configPath)
		}
	}
}
