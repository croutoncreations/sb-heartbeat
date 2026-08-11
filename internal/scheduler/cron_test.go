package scheduler_test

import (
	"strings"
	"testing"

	"github.com/croutoncreations/sb-heartbeat/internal/scheduler"
)

func TestLocalCronPrintsShellSafeReadOnlySuggestion(t *testing.T) {
	entry, err := scheduler.LocalCron(
		workflowConfig(t),
		"/Applications/SB Heartbeat/bin/sb-heartbeat",
		"/Users/example/project's config/sb-heartbeat.yaml",
		"/Users/example/Library/Logs/sb heartbeat.log",
	)
	if err != nil {
		t.Fatal(err)
	}
	required := []string{
		"never edits your crontab",
		"DEMO_URL",
		"DEMO_KEY",
		"37 3,11,19 * * *",
		"'/Applications/SB Heartbeat/bin/sb-heartbeat'",
		`'/Users/example/project'"'"'s config/sb-heartbeat.yaml'`,
		"run --output json",
		"2>&1",
	}
	for _, fragment := range required {
		if !strings.Contains(entry, fragment) {
			t.Errorf("entry missing %q:\n%s", fragment, entry)
		}
	}
	if strings.Contains(entry, "sb_publishable_") || strings.Contains(entry, "crontab -") {
		t.Fatalf("unsafe cron suggestion:\n%s", entry)
	}
}

func TestLocalCronRequiresAbsolutePaths(t *testing.T) {
	for _, tt := range []struct {
		binary string
		config string
		log    string
	}{
		{binary: "sb-heartbeat", config: "/tmp/config.yaml"},
		{binary: "/usr/local/bin/sb-heartbeat", config: "config.yaml"},
		{binary: "/usr/local/bin/sb-heartbeat", config: "/tmp/config.yaml", log: "heartbeat.log"},
		{binary: "/usr/local/bin/sb-heartbeat\n* * * * * bad", config: "/tmp/config.yaml"},
		{binary: "/usr/local/bin/sb-heartbeat", config: "/tmp/config.yaml\n* * * * * bad"},
		{binary: "/usr/local/bin/sb%heartbeat", config: "/tmp/config.yaml"},
	} {
		if _, err := scheduler.LocalCron(workflowConfig(t), tt.binary, tt.config, tt.log); err == nil {
			t.Fatalf("LocalCron(%q, %q, %q) error = nil", tt.binary, tt.config, tt.log)
		}
	}
}

func TestLocalCronWithEnvironmentFileReferencesItWithoutReadingValues(t *testing.T) {
	entry, err := scheduler.LocalCronWithEnvFile(
		workflowConfig(t),
		"/usr/local/bin/sb-heartbeat",
		"/Users/example/project/sb-heartbeat.yaml",
		"/Users/example/.config/sb-heartbeat/heartbeat.env",
		"",
	)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(entry, "--env-file '/Users/example/.config/sb-heartbeat/heartbeat.env'") {
		t.Fatalf("entry does not reference environment file:\n%s", entry)
	}
	if strings.Contains(entry, "PROJECT_URL=") || strings.Contains(entry, "PROJECT_KEY=") {
		t.Fatalf("entry embeds a value:\n%s", entry)
	}
	if _, err := scheduler.LocalCronWithEnvFile(workflowConfig(t), "/usr/local/bin/sb-heartbeat", "/tmp/config", "relative.env", ""); err == nil {
		t.Fatal("LocalCronWithEnvFile() accepted a relative environment file")
	}
	if _, err := scheduler.LocalCronWithEnvFile(workflowConfig(t), "/usr/local/bin/sb-heartbeat", "/tmp/config", "/tmp/heartbeat.env", "/tmp/heartbeat.env"); err == nil {
		t.Fatal("LocalCronWithEnvFile() accepted an environment/log path collision")
	}
}
