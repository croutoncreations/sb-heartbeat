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

	"github.com/jfox85/sb-heartbeat/internal/cli"
	"github.com/jfox85/sb-heartbeat/internal/config"
	"github.com/jfox85/sb-heartbeat/internal/heartbeat"
)

type harness struct {
	stdin      *strings.Reader
	stdout     bytes.Buffer
	stderr     bytes.Buffer
	env        map[string]string
	called     bool
	seen       []heartbeat.Project
	result     []heartbeat.Result
	executable string
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
		Executable: func() (string, error) {
			if h.executable == "" {
				return "/usr/local/bin/sb-heartbeat", nil
			}
			return h.executable, nil
		},
	}
	if h.stdin != nil {
		dependencies.Stdin = h.stdin
	}
	return dependencies
}

func TestInteractiveInitPromptsOnlyForNonSecretMetadata(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sb-heartbeat.yaml")
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
	if code := cli.Execute(context.Background(), []string{"--config", path, "run", "--output", "json"}, deps); code != 3 {
		t.Fatalf("code = %d, want 3", code)
	}
}

func TestConfiguredJSONModeAppliesToPreflightFailures(t *testing.T) {
	dir := t.TempDir()
	path := writeConfig(t, dir, `
version: 1
defaults:
  output: json
projects:
  - name: demo
`)
	h := &harness{env: map[string]string{}}

	code := cli.Execute(context.Background(), []string{"--config", path, "run"}, h.dependencies())
	if code != 2 || h.called {
		t.Fatalf("code = %d, runner called = %v", code, h.called)
	}
	var envelope struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(h.stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not JSON: %v; stdout = %q, stderr = %q", err, h.stdout.String(), h.stderr.String())
	}
	if envelope.Error.Code != "missing_input" || h.stderr.Len() != 0 {
		t.Fatalf("error code = %q, stderr = %q", envelope.Error.Code, h.stderr.String())
	}
}

func TestConfiguredJSONModeAppliesToCredentialRejection(t *testing.T) {
	dir := t.TempDir()
	path := writeConfig(t, dir, `
version: 1
defaults:
  output: json
projects:
  - name: demo
`)
	rejectedKey := "sb_secret_review_fixture_not_a_real_key"
	h := &harness{env: map[string]string{
		"SB_HEARTBEAT_DEMO_URL":     "https://abcdefghijklmnopqrst.supabase.co",
		"SB_HEARTBEAT_DEMO_API_KEY": rejectedKey,
	}}

	code := cli.Execute(context.Background(), []string{"--config", path, "run"}, h.dependencies())
	if code != 2 || h.called {
		t.Fatalf("code = %d, runner called = %v", code, h.called)
	}
	var envelope struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(h.stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not JSON: %v; %q", err, h.stdout.String())
	}
	if envelope.Error.Code != "credential_rejected" {
		t.Fatalf("error code = %q", envelope.Error.Code)
	}
	if strings.Contains(h.stdout.String(), rejectedKey) || h.stderr.Len() != 0 {
		t.Fatalf("credential leaked or stderr used: stdout = %q, stderr = %q", h.stdout.String(), h.stderr.String())
	}
}

func TestFailureJSONWriteFailureUsesExitThree(t *testing.T) {
	dir := t.TempDir()
	path := writeConfig(t, dir, `
version: 1
projects:
  - name: demo
`)
	h := &harness{env: map[string]string{}}
	deps := h.dependencies()
	deps.Stdout = failingWriter{}

	if code := cli.Execute(context.Background(), []string{"--config", path, "run", "--output", "json"}, deps); code != 3 {
		t.Fatalf("code = %d, want 3; stderr = %q", code, h.stderr.String())
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

func TestOutputFormatFlagIsRejectedOutsideCheckCommands(t *testing.T) {
	h := &harness{}
	if code := cli.Execute(context.Background(), []string{"version", "--output", "json"}, h.dependencies()); code != 2 {
		t.Fatalf("code = %d, stdout = %q, stderr = %q", code, h.stdout.String(), h.stderr.String())
	}
}

func writeConfig(t *testing.T, dir, body string) string {
	t.Helper()
	path := filepath.Join(dir, "sb-heartbeat.yaml")
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

	code := cli.Execute(context.Background(), []string{"--config", path, "run", "--output", "json"}, h.dependencies())
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

func TestRunPreflightElevatedKeyUsesCredentialRejectedCode(t *testing.T) {
	dir := t.TempDir()
	path := writeConfig(t, dir, strings.ReplaceAll(twoProjectConfig(), "  - name: second\n    url: {env: SECOND_URL}\n    api_key: {env: SECOND_KEY}\n", ""))
	h := &harness{env: map[string]string{
		"FIRST_URL": "https://abcdefghijklmnopqrst.supabase.co",
		"FIRST_KEY": "sb_secret_review_fixture_not_a_real_key",
	}}

	code := cli.Execute(context.Background(), []string{"--config", path, "run", "--output", "json"}, h.dependencies())
	if code != 2 || h.called {
		t.Fatalf("code = %d, runner called = %v", code, h.called)
	}
	var envelope struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(h.stdout.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Error.Code != "credential_rejected" {
		t.Fatalf("code = %q, output = %s", envelope.Error.Code, h.stdout.String())
	}
	if strings.Contains(h.stdout.String(), h.env["FIRST_KEY"]) {
		t.Fatal("output contains complete rejected key")
	}
}

func TestMissingConfigurationDiagnosticUsesAbsolutePath(t *testing.T) {
	relativePath := filepath.Join("missing", "sb-heartbeat.yaml")
	absolutePath, err := filepath.Abs(relativePath)
	if err != nil {
		t.Fatal(err)
	}
	h := &harness{}
	if code := cli.Execute(context.Background(), []string{"--config", relativePath, "run"}, h.dependencies()); code != 2 {
		t.Fatalf("code = %d", code)
	}
	if !strings.Contains(h.stderr.String(), absolutePath) {
		t.Fatalf("diagnostic = %q, want absolute path %q", h.stderr.String(), absolutePath)
	}
}

func TestInvalidConfigurationDiagnosticUsesAbsolutePath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "invalid.yaml")
	if err := os.WriteFile(path, []byte("version: nope\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	h := &harness{}
	if code := cli.Execute(context.Background(), []string{"--config", path, "run"}, h.dependencies()); code != 2 {
		t.Fatalf("code = %d", code)
	}
	if !strings.Contains(h.stderr.String(), path) {
		t.Fatalf("diagnostic = %q, want path %q", h.stderr.String(), path)
	}
}

func TestGeneratedFileOperationalFailureUsesExitThree(t *testing.T) {
	dir := t.TempDir()
	parentFile := filepath.Join(dir, "not-a-directory")
	if err := os.WriteFile(parentFile, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	outputPath := filepath.Join(parentFile, "migration.sql")
	h := &harness{}
	if code := cli.Execute(context.Background(), []string{"migration", "install", "--output", outputPath}, h.dependencies()); code != 3 {
		t.Fatalf("code = %d, stderr = %q", code, h.stderr.String())
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
	path := filepath.Join(dir, "sb-heartbeat.yaml")
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

func TestNonInteractiveInitUsesDerivedEnvironmentBindings(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sb-heartbeat.yaml")
	h := &harness{}
	args := []string{
		"init", "--non-interactive", "--output-path", path,
		"--project-name", "my-stage",
	}
	if code := cli.Execute(context.Background(), args, h.dependencies()); code != 0 {
		t.Fatalf("init code = %d, stderr = %q", code, h.stderr.String())
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg, loadErr := config.Load(file)
	closeErr := file.Close()
	if loadErr != nil || closeErr != nil {
		t.Fatalf("load error = %v, close error = %v", loadErr, closeErr)
	}
	project := cfg.Projects[0]
	if project.URL.Env != "SB_HEARTBEAT_MY_STAGE_URL" || project.APIKey.Env != "SB_HEARTBEAT_MY_STAGE_API_KEY" {
		t.Fatalf("project = %+v", project)
	}
}

func TestMigrationCommandsPrintOrWrite(t *testing.T) {
	h := &harness{}
	if code := cli.Execute(context.Background(), []string{"migration", "install"}, h.dependencies()); code != 0 || !strings.Contains(h.stdout.String(), "sb-heartbeat:managed:v1") {
		t.Fatalf("code = %d, output = %q", code, h.stdout.String())
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "uninstall.sql")
	h.stdout.Reset()
	if code := cli.Execute(context.Background(), []string{"migration", "uninstall", "--output", path}, h.dependencies()); code != 0 {
		t.Fatalf("code = %d", code)
	}
	data, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(data), "refusing to remove non-SB Heartbeat object") {
		t.Fatalf("file = %q, err = %v", data, err)
	}
}

func TestInstallGitHubGeneratesWorkflowAndRefusesOverwrite(t *testing.T) {
	dir := t.TempDir()
	path := writeConfig(t, dir, strings.ReplaceAll(twoProjectConfig(), "  - name: second\n    url: {env: SECOND_URL}\n    api_key: {env: SECOND_KEY}\n", ""))
	workflow := filepath.Join(dir, ".github", "workflows", "sb-heartbeat.yml")
	h := &harness{}
	args := []string{"--config", path, "install", "github", "--sb-heartbeat-version", "v0.1.0", "--output-path", workflow}
	if code := cli.Execute(context.Background(), args, h.dependencies()); code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, h.stderr.String())
	}
	data, err := os.ReadFile(workflow)
	if err != nil || !strings.Contains(string(data), "sha256sum --check --strict") {
		t.Fatalf("workflow = %q, err = %v", data, err)
	}
	if code := cli.Execute(context.Background(), args, h.dependencies()); code != 2 {
		t.Fatalf("overwrite code = %d", code)
	}
}

func TestInstallGitHubUsesCustomConfigPathByDefault(t *testing.T) {
	dir := t.TempDir()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })
	if err := os.MkdirAll("config", 0o755); err != nil {
		t.Fatal(err)
	}
	writeConfig(t, "config", strings.ReplaceAll(twoProjectConfig(), "  - name: second\n    url: {env: SECOND_URL}\n    api_key: {env: SECOND_KEY}\n", ""))
	h := &harness{}
	args := []string{"--config", "config/sb-heartbeat.yaml", "install", "github", "--sb-heartbeat-version", "v0.1.0"}
	if code := cli.Execute(context.Background(), args, h.dependencies()); code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, h.stderr.String())
	}
	data, err := os.ReadFile(filepath.Join(".github", "workflows", "sb-heartbeat.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "--config config/sb-heartbeat.yaml") {
		t.Fatalf("workflow uses wrong config path:\n%s", data)
	}
}

func TestInitGitHubUsesCustomOutputPathByDefault(t *testing.T) {
	dir := t.TempDir()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })
	h := &harness{}
	args := []string{
		"init", "--non-interactive", "--output-path", "config/custom.yaml",
		"--project-name", "demo", "--scheduler", "github", "--sb-heartbeat-version", "v0.1.0",
	}
	if code := cli.Execute(context.Background(), args, h.dependencies()); code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, h.stderr.String())
	}
	data, err := os.ReadFile(filepath.Join(".github", "workflows", "sb-heartbeat.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "--config config/custom.yaml") {
		t.Fatalf("workflow uses wrong config path:\n%s", data)
	}
}

func TestInitGitHubPreflightsAllTargets(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "sb-heartbeat.yaml")
	workflow := filepath.Join(dir, ".github", "workflows", "sb-heartbeat.yml")
	if err := os.MkdirAll(filepath.Dir(workflow), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(workflow, []byte("existing"), 0o600); err != nil {
		t.Fatal(err)
	}
	h := &harness{}
	args := []string{
		"init", "--non-interactive", "--output-path", configPath,
		"--project-name", "demo", "--url-env", "DEMO_URL", "--api-key-env", "DEMO_KEY",
		"--scheduler", "github", "--workflow-output", workflow, "--sb-heartbeat-version", "v0.1.0",
	}
	if code := cli.Execute(context.Background(), args, h.dependencies()); code != 2 {
		t.Fatalf("code = %d", code)
	}
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatalf("configuration was written despite workflow collision: %v", err)
	}
}

func TestInitGitHubRejectsSameOutputTarget(t *testing.T) {
	dir := t.TempDir()
	sharedPath := filepath.Join(dir, "sb-heartbeat.yaml")
	h := &harness{}
	args := []string{
		"init", "--non-interactive", "--output-path", sharedPath,
		"--project-name", "demo", "--url-env", "DEMO_URL", "--api-key-env", "DEMO_KEY",
		"--scheduler", "github", "--workflow-output", filepath.Join(dir, ".", "sb-heartbeat.yaml"),
		"--sb-heartbeat-version", "v0.1.0",
	}
	if code := cli.Execute(context.Background(), args, h.dependencies()); code != 2 {
		t.Fatalf("code = %d", code)
	}
	if _, err := os.Stat(sharedPath); !os.IsNotExist(err) {
		t.Fatalf("shared output was written: %v", err)
	}
}

func TestInitWritesInstallMigration(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "sb-heartbeat.yaml")
	migrationPath := filepath.Join(dir, "migrations", "sb-heartbeat.sql")
	h := &harness{}
	args := []string{
		"init", "--non-interactive", "--output-path", configPath,
		"--project-name", "demo", "--url-env", "DEMO_URL", "--api-key-env", "DEMO_KEY",
		"--migration-output", migrationPath,
	}
	if code := cli.Execute(context.Background(), args, h.dependencies()); code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, h.stderr.String())
	}
	migrationData, err := os.ReadFile(migrationPath)
	if err != nil || !strings.Contains(string(migrationData), "sb-heartbeat:managed:v1") {
		t.Fatalf("migration = %q, err = %v", migrationData, err)
	}
	if !strings.Contains(h.stdout.String(), configPath) || !strings.Contains(h.stdout.String(), migrationPath) {
		t.Fatalf("output = %q", h.stdout.String())
	}
}

func TestInitMigrationPreflightsAllTargets(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "sb-heartbeat.yaml")
	migrationPath := filepath.Join(dir, "migration.sql")
	if err := os.WriteFile(migrationPath, []byte("existing"), 0o600); err != nil {
		t.Fatal(err)
	}
	h := &harness{}
	args := []string{
		"init", "--non-interactive", "--output-path", configPath,
		"--project-name", "demo", "--url-env", "DEMO_URL", "--api-key-env", "DEMO_KEY",
		"--migration-output", migrationPath,
	}
	if code := cli.Execute(context.Background(), args, h.dependencies()); code != 2 {
		t.Fatalf("code = %d", code)
	}
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatalf("configuration was written despite migration collision: %v", err)
	}
}

func TestInitRejectsAliasedMigrationTarget(t *testing.T) {
	dir := t.TempDir()
	sharedPath := filepath.Join(dir, "sb-heartbeat.yaml")
	h := &harness{}
	args := []string{
		"init", "--non-interactive", "--output-path", sharedPath,
		"--project-name", "demo", "--url-env", "DEMO_URL", "--api-key-env", "DEMO_KEY",
		"--migration-output", filepath.Join(dir, ".", "sb-heartbeat.yaml"),
	}
	if code := cli.Execute(context.Background(), args, h.dependencies()); code != 2 {
		t.Fatalf("code = %d", code)
	}
	if _, err := os.Stat(sharedPath); !os.IsNotExist(err) {
		t.Fatalf("shared output was written: %v", err)
	}
}

func TestInstallCronPrintsSuggestionWithoutWriting(t *testing.T) {
	dir := t.TempDir()
	configPath := writeConfig(t, dir, strings.ReplaceAll(twoProjectConfig(), "  - name: second\n    url: {env: SECOND_URL}\n    api_key: {env: SECOND_KEY}\n", ""))
	logPath := filepath.Join(dir, "heartbeat.log")
	h := &harness{executable: "/Applications/SB Heartbeat/sb-heartbeat"}
	args := []string{"--config", configPath, "install", "cron", "--log-path", logPath}
	if code := cli.Execute(context.Background(), args, h.dependencies()); code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, h.stderr.String())
	}
	if !strings.Contains(h.stdout.String(), "37 3,11,19 * * *") ||
		!strings.Contains(h.stdout.String(), "DEMO") && !strings.Contains(h.stdout.String(), "FIRST_URL") {
		t.Fatalf("output = %q", h.stdout.String())
	}
	if _, err := os.Stat(logPath); !os.IsNotExist(err) {
		t.Fatalf("cron command wrote log path: %v", err)
	}
}

func TestInstallCronResolvesDetectedExecutableToAbsolutePath(t *testing.T) {
	dir := t.TempDir()
	configPath := writeConfig(t, dir, strings.ReplaceAll(twoProjectConfig(), "  - name: second\n    url: {env: SECOND_URL}\n    api_key: {env: SECOND_KEY}\n", ""))
	h := &harness{executable: filepath.Join("bin", "sb-heartbeat")}
	if code := cli.Execute(context.Background(), []string{"--config", configPath, "install", "cron"}, h.dependencies()); code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, h.stderr.String())
	}
	expected, err := filepath.Abs(h.executable)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.stdout.String(), expected) {
		t.Fatalf("output = %q, want %q", h.stdout.String(), expected)
	}
}
