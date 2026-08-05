package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jfox85/supawake/internal/cli"
	"github.com/jfox85/supawake/internal/config"
	"github.com/jfox85/supawake/internal/heartbeat"
)

type harness struct {
	stdin  *strings.Reader
	stdout bytes.Buffer
	stderr bytes.Buffer
	env    map[string]string
	called bool
	seen   []heartbeat.Project
	result []heartbeat.Result
}

func (h *harness) dependencies() cli.Dependencies {
	dependencies := cli.Dependencies{
		Stdout: &h.stdout,
		Stderr: &h.stderr,
		LookupEnv: func(name string) (string, bool) {
			value, ok := h.env[name]
			return value, ok
		},
		RunProjects: func(_ context.Context, projects []heartbeat.Project, _ config.Defaults) []heartbeat.Result {
			h.called = true
			h.seen = projects
			if h.result != nil {
				return h.result
			}
			results := make([]heartbeat.Result, len(projects))
			for i, project := range projects {
				results[i] = heartbeat.Result{Name: project.Name, Status: heartbeat.Healthy, Attempts: 1}
			}
			return results
		},
		Now: func() time.Time { return time.Date(2026, 8, 4, 10, 0, 0, 0, time.UTC) },
	}
	if h.stdin != nil {
		dependencies.Stdin = h.stdin
	}
	return dependencies
}

func TestInteractiveInitPromptsOnlyForNonSecretMetadata(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "supawake.yaml")
	h := &harness{stdin: strings.NewReader("demo\nDEMO_URL\nDEMO_KEY\n\n")}
	code := cli.Execute(context.Background(), []string{"init", "--output-path", path}, h.dependencies())
	if code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, h.stderr.String())
	}
	if strings.Contains(strings.ToLower(h.stdout.String()), "key value") {
		t.Fatalf("prompt requested a key value: %q", h.stdout.String())
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(file)
	file.Close()
	if err != nil || cfg.Projects[0].APIKey.Env != "DEMO_KEY" {
		t.Fatalf("config = %+v, err = %v", cfg, err)
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

func TestInternalOutputFailureUsesExitThree(t *testing.T) {
	dir := t.TempDir()
	path := writeConfig(t, dir, strings.ReplaceAll(twoProjectConfig(), "  - name: second\n    url: {env: SECOND_URL}\n    api_key: {env: SECOND_KEY}\n", ""))
	h := &harness{env: map[string]string{
		"FIRST_URL": "https://abcdefghijklmnopqrst.supabase.co",
		"FIRST_KEY": "sb_publishable_abcdefghijklmnopqrstuv_12345678",
	}}
	deps := h.dependencies()
	deps.Stdout = failingWriter{}
	if code := cli.Execute(context.Background(), []string{"--config", path, "--output", "json", "run"}, deps); code != 3 {
		t.Fatalf("code = %d, want 3", code)
	}
}

func TestRunnerResultCountMismatchUsesExitThree(t *testing.T) {
	dir := t.TempDir()
	path := writeConfig(t, dir, strings.ReplaceAll(twoProjectConfig(), "  - name: second\n    url: {env: SECOND_URL}\n    api_key: {env: SECOND_KEY}\n", ""))
	h := &harness{env: map[string]string{
		"FIRST_URL": "https://abcdefghijklmnopqrst.supabase.co",
		"FIRST_KEY": "sb_publishable_abcdefghijklmnopqrstuv_12345678",
	}, result: []heartbeat.Result{}}
	if code := cli.Execute(context.Background(), []string{"--config", path, "run"}, h.dependencies()); code != 3 {
		t.Fatalf("code = %d, want 3", code)
	}
}

func writeConfig(t *testing.T, dir, body string) string {
	t.Helper()
	path := filepath.Join(dir, "supawake.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func twoProjectConfig() string {
	return `
version: 1
projects:
  - name: first
    url: {env: FIRST_URL}
    api_key: {env: FIRST_KEY}
  - name: second
    url: {env: SECOND_URL}
    api_key: {env: SECOND_KEY}
`
}

func TestRunPreflightCollectsErrorsAndMakesNoRequests(t *testing.T) {
	dir := t.TempDir()
	path := writeConfig(t, dir, twoProjectConfig())
	h := &harness{env: map[string]string{
		"FIRST_URL":  "https://abcdefghijklmnopqrst.supabase.co",
		"FIRST_KEY":  "sb_publishable_abcdefghijklmnopqrstuv_12345678",
		"SECOND_URL": "https://bad.example.com",
	}}

	code := cli.Execute(context.Background(), []string{"--config", path, "--output", "json", "run"}, h.dependencies())
	if code != 2 || h.called {
		t.Fatalf("code = %d, runner called = %v", code, h.called)
	}
	var envelope map[string]any
	if err := json.Unmarshal(h.stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("output is not JSON: %v; %q", err, h.stdout.String())
	}
	if envelope["success"] != false || !strings.Contains(h.stdout.String(), "SECOND_KEY") || !strings.Contains(h.stdout.String(), "project URL") {
		t.Fatalf("output = %s", h.stdout.String())
	}
}

func TestRunResolvesProjectsAndUsesStableExitCode(t *testing.T) {
	dir := t.TempDir()
	path := writeConfig(t, dir, twoProjectConfig())
	h := &harness{env: map[string]string{
		"FIRST_URL":  "https://abcdefghijklmnopqrst.supabase.co",
		"FIRST_KEY":  "sb_publishable_abcdefghijklmnopqrstuv_12345678",
		"SECOND_URL": "https://bcdefghijklmnopqrstu.supabase.co",
		"SECOND_KEY": "sb_publishable_bcdefghijklmnopqrstuvw_12345678",
	}, result: []heartbeat.Result{
		{Name: "first", Status: heartbeat.Healthy, Attempts: 1},
		{Name: "second", Status: heartbeat.ProjectPaused, Attempts: 1, Error: &heartbeat.Error{Code: heartbeat.ProjectPaused, Message: "paused"}},
	}}

	code := cli.Execute(context.Background(), []string{"--config", path, "run"}, h.dependencies())
	if code != 1 || !h.called || len(h.seen) != 2 {
		t.Fatalf("code = %d, called = %v, projects = %d", code, h.called, len(h.seen))
	}
	if !strings.Contains(h.stdout.String(), "project_paused") {
		t.Fatalf("output = %q", h.stdout.String())
	}
}

func TestRunProjectFilter(t *testing.T) {
	dir := t.TempDir()
	path := writeConfig(t, dir, twoProjectConfig())
	h := &harness{env: map[string]string{
		"SECOND_URL": "https://bcdefghijklmnopqrstu.supabase.co",
		"SECOND_KEY": "sb_publishable_bcdefghijklmnopqrstuvw_12345678",
	}}

	code := cli.Execute(context.Background(), []string{"--config", path, "run", "--project", "second"}, h.dependencies())
	if code != 0 || len(h.seen) != 1 || h.seen[0].Name != "second" {
		t.Fatalf("code = %d, projects = %+v", code, h.seen)
	}
}

func TestDoctorRunsSameNonMutatingHeartbeat(t *testing.T) {
	dir := t.TempDir()
	path := writeConfig(t, dir, strings.ReplaceAll(twoProjectConfig(), "  - name: second\n    url: {env: SECOND_URL}\n    api_key: {env: SECOND_KEY}\n", ""))
	h := &harness{env: map[string]string{
		"FIRST_URL": "https://abcdefghijklmnopqrst.supabase.co",
		"FIRST_KEY": "sb_publishable_abcdefghijklmnopqrstuv_12345678",
	}}

	code := cli.Execute(context.Background(), []string{"--config", path, "doctor"}, h.dependencies())
	if code != 0 || !h.called || !strings.Contains(h.stdout.String(), "healthy") {
		t.Fatalf("code = %d, output = %q", code, h.stdout.String())
	}
}

func TestDoctorExplainsPermissionAmbiguity(t *testing.T) {
	dir := t.TempDir()
	path := writeConfig(t, dir, strings.ReplaceAll(twoProjectConfig(), "  - name: second\n    url: {env: SECOND_URL}\n    api_key: {env: SECOND_KEY}\n", ""))
	h := &harness{env: map[string]string{
		"FIRST_URL": "https://abcdefghijklmnopqrst.supabase.co",
		"FIRST_KEY": "sb_publishable_abcdefghijklmnopqrstuv_12345678",
	}, result: []heartbeat.Result{{
		Name: "first", Status: heartbeat.DatabasePermissionDenied, Attempts: 1,
		Error: &heartbeat.Error{Code: heartbeat.DatabasePermissionDenied, Message: "denied"},
	}}}

	code := cli.Execute(context.Background(), []string{"--config", path, "doctor"}, h.dependencies())
	if code != 1 || !strings.Contains(h.stdout.String(), "cannot distinguish") || !strings.Contains(h.stdout.String(), "RLS") {
		t.Fatalf("code = %d, output = %q", code, h.stdout.String())
	}
}

func TestNonInteractiveInitWritesSafeConfigAndRefusesOverwrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "supawake.yaml")
	h := &harness{}
	args := []string{
		"init", "--non-interactive", "--output-path", path,
		"--project-name", "demo", "--url-env", "DEMO_URL", "--api-key-env", "DEMO_KEY",
	}
	if code := cli.Execute(context.Background(), args, h.dependencies()); code != 0 {
		t.Fatalf("first init code = %d, stderr = %q", code, h.stderr.String())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "cron: 37 3,11,19 * * *") || strings.Contains(text, "api_key: sb_") {
		t.Fatalf("config = %s", text)
	}
	h.stdout.Reset()
	h.stderr.Reset()
	if code := cli.Execute(context.Background(), args, h.dependencies()); code != 2 {
		t.Fatalf("overwrite init code = %d", code)
	}
}

func TestMigrationCommandsPrintOrWrite(t *testing.T) {
	h := &harness{}
	if code := cli.Execute(context.Background(), []string{"migration", "install"}, h.dependencies()); code != 0 || !strings.Contains(h.stdout.String(), "supawake:heartbeat:v1") {
		t.Fatalf("code = %d, output = %q", code, h.stdout.String())
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "uninstall.sql")
	h.stdout.Reset()
	if code := cli.Execute(context.Background(), []string{"migration", "uninstall", "--output", path}, h.dependencies()); code != 0 {
		t.Fatalf("code = %d", code)
	}
	data, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(data), "refusing to remove non-Supawake object") {
		t.Fatalf("file = %q, err = %v", data, err)
	}
}
