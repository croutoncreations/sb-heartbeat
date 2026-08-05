package scheduler_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/croutoncreations/sb-heartbeat/internal/config"
	"github.com/croutoncreations/sb-heartbeat/internal/scheduler"
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
		"Scheduled cron expressions use UTC",
		"latest commit on the default branch",
		"may be delayed or dropped",
		"inactive public repositories",
		"Store project URLs as repository variables",
		"low-privilege client keys as repository secrets",
		"storage policy remains the repository owner's choice",
		`cron: "37 3,11,19 * * *"`,
		"workflow_dispatch:",
		"permissions:\n  contents: read",
		"actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1",
		"SB_HEARTBEAT_VERSION: v0.1.0",
		"https://github.com/croutoncreations/sb-heartbeat/releases/download/${SB_HEARTBEAT_VERSION}",
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
	runStep := strings.Index(text, "      - name: Run heartbeats")
	keyBinding := strings.Index(text, "DEMO_KEY: ${{ secrets.DEMO_KEY }}")
	if runStep < 0 || keyBinding < runStep {
		t.Errorf("client key is exposed before the heartbeat step\n%s", text)
	}
	goldenPath := filepath.Join("testdata", "github-workflow.yml.golden")
	golden, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatal(err)
	}
	if text != string(golden) {
		t.Fatalf("generated workflow differs from %s", goldenPath)
	}
}

func TestReleaseWorkflowUsesProtectedHostedEnvironment(t *testing.T) {
	workflow, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "release.yml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(workflow)
	hostedJob := strings.Index(text, "  hosted-supabase:")
	releaseJob := strings.Index(text, "  release:")
	protectedEnvironment := strings.Index(text, "    environment: hosted-supabase-release")
	if hostedJob < 0 || releaseJob < 0 || protectedEnvironment < hostedJob || protectedEnvironment > releaseJob {
		t.Fatalf("hosted integration job does not use the documented protected environment\n%s", text)
	}
}

func TestHostedIntegrationRefusesPreExistingHeartbeatBeforeCleanup(t *testing.T) {
	script, err := os.ReadFile(filepath.Join("..", "..", "scripts", "integration-hosted-supabase.sh"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(script)
	preflight := strings.Index(text, "select to_regclass('public.sb_heartbeat') is null")
	cleanupTrap := strings.Index(text, "trap cleanup EXIT")
	if preflight < 0 || cleanupTrap < 0 || preflight > cleanupTrap {
		t.Fatalf("hosted integration must verify the heartbeat object is absent before enabling cleanup\n%s", text)
	}
	if !strings.Contains(text, "dedicated disposable Supabase project") {
		t.Fatalf("hosted integration refusal must explain the dedicated-project requirement\n%s", text)
	}
}

func TestReleaseToolchainIncludesCurrentSecurityFix(t *testing.T) {
	module, err := os.ReadFile(filepath.Join("..", "..", "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(module), "toolchain go1.26.5\n") {
		t.Fatalf("go.mod must select Go 1.26.5 or a deliberately reviewed successor")
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
