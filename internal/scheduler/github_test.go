package scheduler_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
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

func TestGitHubWorkflowUsesConfiguredBindingSources(t *testing.T) {
	cfg, err := config.New(config.Project{
		Name:   "demo",
		URL:    config.EnvReference{Env: "DEMO_URL", GitHub: "secret"},
		APIKey: config.EnvReference{Env: "DEMO_KEY", GitHub: "variable"},
	}, config.DefaultCron)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := scheduler.GitHub(cfg, "v0.1.1", "sb-heartbeat.yaml"); err == nil || !strings.Contains(err.Error(), "v0.2.0") {
		t.Fatalf("GitHub(v0.1.1 custom sources) error = %v", err)
	}
	workflow, err := scheduler.GitHub(cfg, "v0.2.0", "sb-heartbeat.yaml")
	if err != nil {
		t.Fatal(err)
	}
	text := string(workflow)
	if !strings.Contains(text, "DEMO_URL: ${{ secrets.DEMO_URL }}") ||
		!strings.Contains(text, "DEMO_KEY: ${{ vars.DEMO_KEY }}") {
		t.Fatalf("configured GitHub bindings missing\n%s", text)
	}
}

func TestGitHubWorkflowGeneratesOptionalSanitizedObservability(t *testing.T) {
	workflow, err := scheduler.GitHubWithOptions(workflowConfig(t), "v0.1.1", "sb-heartbeat.yaml", scheduler.GitHubOptions{
		Annotations: true, ArtifactRetentionDays: 7,
	})
	if err != nil {
		t.Fatal(err)
	}
	text := string(workflow)
	if strings.ContainsRune(text, '\t') {
		t.Fatalf("workflow contains a YAML-invalid tab character\n%s", text)
	}
	for _, required := range []string{
		"Prepare sanitized observability result",
		"if: always()",
		"jq --exit-status",
		"--slurp",
		`candidate_result="${RUNNER_TEMP}/sb-heartbeat-observability.json.tmp"`,
		`mv "${candidate_result}" "${safe_result}"`,
		`value=${value//'%'/'%25'}`,
		`value=${value//$'\r'/'%0D'}`,
		`value=${value//$'\n'/'%0A'}`,
		`{name, status, http_status, latency_ms, attempts}`,
		"::error title=SB Heartbeat project failure::",
		"::error title=SB Heartbeat invocation failure::",
		"workflow_missing_result",
		"observability_sanitization_failed",
		"actions/upload-artifact@043fb46d1a93c77aae656e7c1c64a875d1fc6a0a # v7.0.1",
		"retention-days: 7",
		"if-no-files-found: warn",
	} {
		if !strings.Contains(text, required) {
			t.Errorf("observability workflow missing %q\n%s", required, text)
		}
	}
	if strings.Contains(text, "message: .error.message") || strings.Contains(text, "authorization") {
		t.Fatalf("observability artifact includes sensitive diagnostic fields\n%s", text)
	}
	for _, forbidden := range []string{
		"No heartbeat result was produced",
		"The bounded result could not be sanitized",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("annotation includes diagnostic message %q\n%s", forbidden, text)
		}
	}
	if strings.Contains(text, `> "${safe_result}"`) {
		t.Fatalf("sanitizer writes directly to the upload path\n%s", text)
	}
}

func TestGitHubWorkflowLeavesObservabilityDisabledByDefault(t *testing.T) {
	workflow, err := scheduler.GitHub(workflowConfig(t), "v0.1.1", "sb-heartbeat.yaml")
	if err != nil {
		t.Fatal(err)
	}
	text := string(workflow)
	if strings.Contains(text, "upload-artifact") || strings.Contains(text, "::error title=") {
		t.Fatalf("default workflow unexpectedly enables optional observability\n%s", text)
	}
}

func TestGitHubWorkflowGeneratesDurableRepeatedFailureNotifications(t *testing.T) {
	workflow, err := scheduler.GitHubWithOptions(workflowConfig(t), "v0.2.0", "sb-heartbeat.yaml", scheduler.GitHubOptions{
		NotificationWebhookSecret: "SB_HEARTBEAT_NOTIFICATION_WEBHOOK",
		NotifyAfter:               3,
	})
	if err != nil {
		t.Fatal(err)
	}
	text := string(workflow)
	for _, required := range []string{
		"notification-ref-guard:",
		`if: github.ref != format('refs/heads/{0}', github.event.repository.default_branch)`,
		"Notification workflows must be dispatched from the repository default branch",
		`if: github.ref == format('refs/heads/{0}', github.event.repository.default_branch)`,
		"actions: read",
		"concurrency:",
		"cancel-in-progress: false",
		"Derive notification cache scope",
		"SB_HEARTBEAT_WORKFLOW_REF: ${{ github.workflow_ref }}",
		`scope="$(printf '%s' "${SB_HEARTBEAT_WORKFLOW_REF}" | sha256sum | awk '{print $1}')"`,
		"Restore sanitized notification state",
		"Resolve preceding notification run",
		`id: notification-predecessor`,
		`GITHUB_TOKEN: ${{ github.token }}`,
		`/actions/runs/${GITHUB_RUN_ID}`,
		`.workflow_id`,
		`--connect-timeout 5`,
		`--max-time 15`,
		`--data-urlencode "branch=${GITHUB_REF_NAME}"`,
		`.run_number < $current`,
		`expected_sequence="${{ steps.notification-predecessor.outputs.run_id }}"`,
		"actions/cache/restore@55cc8345863c7cc4c66a329aec7e433d2d1c52a9 # v6.1.0",
		"actions/cache/save@55cc8345863c7cc4c66a329aec7e433d2d1c52a9 # v6.1.0",
		"SB_HEARTBEAT_NOTIFICATION_WEBHOOK: ${{ secrets.SB_HEARTBEAT_NOTIFICATION_WEBHOOK }}",
		`state_path="${RUNNER_TEMP}/sb-heartbeat-notifications/notifications.json"`,
		`sequence_path="${RUNNER_TEMP}/sb-heartbeat-notifications/run-id"`,
		`expected_sequence="${GITHUB_RUN_ID}"`,
		`rm -rf -- "${state_path}" "${sequence_path}"`,
		`--notification-state "${state_path}"`,
		"--notification-webhook-env SB_HEARTBEAT_NOTIFICATION_WEBHOOK",
		"--notify-after 3",
		`id: heartbeat`,
		`echo "status=${status}" >> "${GITHUB_OUTPUT}"`,
		`echo "state_changed=${state_changed}" >> "${GITHUB_OUTPUT}"`,
		`printf '%s\n' "${GITHUB_RUN_ID}" > "${sequence_path}.tmp"`,
		"Save sanitized notification state",
		"steps.heartbeat.outputs.state_changed == 'true'",
		"Propagate heartbeat exit status",
		`status="${{ steps.heartbeat.outputs.status }}"`,
	} {
		if !strings.Contains(text, required) {
			t.Errorf("notification workflow missing %q\n%s", required, text)
		}
	}
	for _, forbidden := range []string{"expected_sequence=$((GITHUB_RUN_NUMBER - 1))", "/run-number", "workflow_file="} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("notification workflow uses global workflow sequence %q\n%s", forbidden, text)
		}
	}
	if !strings.Contains(text, `sb-heartbeat-notifications-${{ steps.notification-cache-scope.outputs.scope }}-`) {
		t.Fatalf("cache key does not use the hashed full workflow/ref identity\n%s", text)
	}
	if strings.Contains(text, `sb-heartbeat-notifications-${{ github.ref_name }}`) || strings.Contains(text, `sb-heartbeat-notifications-${{ github.ref_type }}`) {
		t.Fatalf("cache key uses a prefix-ambiguous raw ref identity\n%s", text)
	}
	scope := strings.Index(text, "Derive notification cache scope")
	predecessor := strings.Index(text, "Resolve preceding notification run")
	restore := strings.Index(text, "Restore sanitized notification state")
	run := strings.Index(text, "Run heartbeats")
	save := strings.Index(text, "Save sanitized notification state")
	final := strings.Index(text, "Propagate heartbeat exit status")
	if scope < 0 || predecessor <= scope || restore <= predecessor || run <= restore || save <= run || final <= save {
		t.Fatalf("notification workflow steps are out of order\n%s", text)
	}
	secret := strings.Index(text, "SB_HEARTBEAT_NOTIFICATION_WEBHOOK: ${{ secrets.SB_HEARTBEAT_NOTIFICATION_WEBHOOK }}")
	if secret < run || secret > save {
		t.Fatalf("notification webhook is scoped outside the heartbeat step\n%s", text)
	}
	if strings.Count(text, "SB_HEARTBEAT_NOTIFICATION_WEBHOOK: ${{ secrets.SB_HEARTBEAT_NOTIFICATION_WEBHOOK }}") != 1 {
		t.Fatalf("notification webhook secret is mapped more than once\n%s", text)
	}
	outputs := strings.Index(text, `echo "state_changed=${state_changed}" >> "${GITHUB_OUTPUT}"`)
	summary := strings.Index(text, `} >> "${GITHUB_STEP_SUMMARY}"`)
	if outputs < 0 || summary < 0 || outputs > summary {
		t.Fatalf("state outputs are emitted after the nonessential job summary\n%s", text)
	}
}

func TestGitHubWorkflowValidatesNotificationOptionsAndRuntimeVersion(t *testing.T) {
	tests := []scheduler.GitHubOptions{
		{NotifyAfter: 3},
		{NotificationWebhookSecret: "bad-name", NotifyAfter: 3},
		{NotificationWebhookSecret: "GITHUB_WEBHOOK", NotifyAfter: 3},
		{NotificationWebhookSecretSet: true},
		{NotificationWebhookSecret: "WEBHOOK", NotifyAfterSet: true},
		{NotificationWebhookSecret: "WEBHOOK", NotifyAfter: -1},
		{NotificationWebhookSecret: "WEBHOOK", NotifyAfter: 101},
	}
	for _, options := range tests {
		if _, err := scheduler.GitHubWithOptions(workflowConfig(t), "v0.2.0", "sb-heartbeat.yaml", options); err == nil {
			t.Fatalf("invalid notification options accepted: %+v", options)
		}
	}
	if _, err := scheduler.GitHubWithOptions(workflowConfig(t), "v0.1.1", "sb-heartbeat.yaml", scheduler.GitHubOptions{
		NotificationWebhookSecret: "WEBHOOK", NotifyAfter: 3,
	}); err == nil || !strings.Contains(err.Error(), "v0.2.0") {
		t.Fatalf("legacy notification runtime error=%v", err)
	}
	workflow, err := scheduler.GitHubWithOptions(workflowConfig(t), "v0.2.0", "sb-heartbeat.yaml", scheduler.GitHubOptions{
		NotificationWebhookSecret: "WEBHOOK",
	})
	if err != nil || !strings.Contains(string(workflow), "--notify-after 3") {
		t.Fatalf("default notification threshold workflow=%q err=%v", workflow, err)
	}
}

func TestGitHubWorkflowTimeoutIncludesWorstCaseNotificationDelivery(t *testing.T) {
	projects := make([]config.Project, 30)
	for index := range projects {
		name := "project_" + strconv.Itoa(index)
		projects[index] = config.Project{
			Name: name, URL: config.EnvReference{Env: "URL_" + strconv.Itoa(index)},
			APIKey: config.EnvReference{Env: "KEY_" + strconv.Itoa(index)},
		}
	}
	cfg, err := config.NewProjects(projects, config.DefaultCron)
	if err != nil {
		t.Fatal(err)
	}
	workflow, err := scheduler.GitHubWithOptions(cfg, "v0.2.0", "sb-heartbeat.yaml", scheduler.GitHubOptions{
		NotificationWebhookSecret: "WEBHOOK", NotifyAfter: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(workflow), "timeout-minutes: 11") {
		t.Fatalf("workflow timeout does not include 30 sequential delivery attempts\n%s", workflow)
	}
}

func TestGitHubWorkflowRejectsInvalidArtifactRetention(t *testing.T) {
	for _, days := range []int{-1, 91} {
		if _, err := scheduler.GitHubWithOptions(workflowConfig(t), "v0.1.1", "sb-heartbeat.yaml", scheduler.GitHubOptions{ArtifactRetentionDays: days}); err == nil {
			t.Fatalf("retention %d was accepted", days)
		}
	}
}

func TestGitHubWorkflowRejectsExplicitDefaultSourcesForLegacyRuntime(t *testing.T) {
	for _, githubValue := range []string{"variable", "null", `""`, ""} {
		t.Run("value="+githubValue, func(t *testing.T) {
			input := "version: 1\nprojects:\n  - name: demo\n    url:\n      env: DEMO_URL\n      github: " + githubValue + "\n    api_key: {env: DEMO_KEY}\n"
			cfg, err := config.Load(strings.NewReader(input))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := scheduler.GitHub(cfg, "v0.1.1", "sb-heartbeat.yaml"); err == nil || !strings.Contains(err.Error(), "v0.2.0") {
				t.Fatalf("GitHub(v0.1.1 explicit source) error = %v", err)
			}
		})
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
	if hostedJob < 0 || releaseJob < 0 || hostedJob >= releaseJob ||
		!strings.Contains(text[hostedJob:releaseJob], "    environment: hosted-supabase-release") {
		t.Fatalf("hosted integration job does not use the documented protected environment\n%s", text)
	}
}

func TestHostedIntegrationRequiresReleaseFixtureBeforeCleanupAndRestoresHeartbeat(t *testing.T) {
	script, err := os.ReadFile(filepath.Join("..", "..", "scripts", "integration-hosted-supabase.sh"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(script)
	preflight := strings.Index(text, "sb-heartbeat:release-fixture:v1")
	cleanupTrap := strings.Index(text, "trap cleanup EXIT")
	if preflight < 0 || cleanupTrap < 0 || preflight > cleanupTrap {
		t.Fatalf("hosted integration must verify the exact release fixture before enabling cleanup\n%s", text)
	}
	if !strings.Contains(text, "dedicated SB Heartbeat release fixture") {
		t.Fatalf("hosted integration refusal must explain the dedicated-project requirement\n%s", text)
	}
	for _, fragment := range []string{
		"pg_attribute",
		"sb_heartbeat_release_fixture_pkey",
		"sb_heartbeat_release_fixture_value",
	} {
		if !strings.Contains(text[:cleanupTrap], fragment) {
			t.Fatalf("hosted integration fixture preflight does not validate %q\n%s", fragment, text[:cleanupTrap])
		}
	}
	cleanup := text[strings.Index(text, "cleanup() {"):cleanupTrap]
	if !strings.Contains(cleanup, `"${binary}" migration install`) {
		t.Fatalf("hosted integration cleanup must restore the managed heartbeat table\n%s", cleanup)
	}
	if strings.Contains(cleanup, `"${binary}" migration uninstall`) {
		t.Fatalf("hosted integration cleanup must not leave the fixture without a heartbeat table\n%s", cleanup)
	}
	finalRestore := strings.LastIndex(text, `"${binary}" migration install`)
	postRestoreHeartbeat := strings.LastIndex(text, `wait_for_heartbeat "${SB_HEARTBEAT_HOSTED_PUBLISHABLE_KEY}" "restored heartbeat table"`)
	if finalRestore < cleanupTrap || postRestoreHeartbeat < finalRestore {
		t.Fatalf("hosted integration must prove the restored heartbeat table is healthy\n%s", text)
	}
	for _, fragment := range []string{
		"for attempt in 1 2 3 4 5 6",
		`wait_for_heartbeat "${SB_HEARTBEAT_HOSTED_PUBLISHABLE_KEY}" "publishable key heartbeat"`,
		`wait_for_heartbeat "${SB_HEARTBEAT_HOSTED_ANON_KEY}" "anon key heartbeat"`,
		`waiting for the ${description}`,
		`${description} did not become healthy`,
	} {
		if !strings.Contains(text, fragment) {
			t.Fatalf("hosted integration restoration retry missing %q\n%s", fragment, text)
		}
	}
}

func TestReleaseFixtureSetupAndScheduledHeartbeatAreSafe(t *testing.T) {
	setup, err := os.ReadFile(filepath.Join("..", "..", "scripts", "release-fixture-install.sql"))
	if err != nil {
		t.Fatal(err)
	}
	setupText := string(setup)
	for _, fragment := range []string{
		"sb_heartbeat_release",
		"sb-heartbeat:release-fixture:v1",
		"comment on schema sb_heartbeat_release",
		"revoke all on schema sb_heartbeat_release from public, anon, authenticated, service_role",
		"revoke all on table sb_heartbeat_release.fixture from public, anon, authenticated, service_role",
		"refusing to use non-SB Heartbeat schema sb_heartbeat_release",
		"refusing to replace non-SB Heartbeat object sb_heartbeat_release.fixture",
	} {
		if !strings.Contains(setupText, fragment) {
			t.Errorf("release fixture setup missing %q", fragment)
		}
	}

	workflow, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "release-fixture-heartbeat.yml"))
	if err != nil {
		t.Fatal(err)
	}
	workflowText := string(workflow)
	for _, fragment := range []string{
		`cron: "37 3,11,19 * * *"`,
		"workflow_dispatch:",
		"if: vars.SB_HEARTBEAT_RELEASE_FIXTURE_ENABLED == 'true'",
		"actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1",
		"SB_HEARTBEAT_VERSION: v0.1.1",
		"sha256sum --check --strict",
		"SB_HEARTBEAT_RELEASE_FIXTURE_URL: ${{ vars.SB_HEARTBEAT_RELEASE_FIXTURE_URL }}",
		"SB_HEARTBEAT_RELEASE_FIXTURE_API_KEY: ${{ secrets.SB_HEARTBEAT_RELEASE_FIXTURE_API_KEY }}",
		"sb-heartbeat --config release-fixture/sb-heartbeat.yaml run --output json",
	} {
		if !strings.Contains(workflowText, fragment) {
			t.Errorf("release fixture workflow missing %q", fragment)
		}
	}
	if strings.Contains(workflowText, "SB_HEARTBEAT_HOSTED_DATABASE_URL") {
		t.Fatal("scheduled fixture heartbeat must not receive database credentials")
	}

	configFile, err := os.ReadFile(filepath.Join("..", "..", "release-fixture", "sb-heartbeat.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	configText := string(configFile)
	for _, fragment := range []string{
		"env: SB_HEARTBEAT_RELEASE_FIXTURE_URL",
		"env: SB_HEARTBEAT_RELEASE_FIXTURE_API_KEY",
		`cron: "37 3,11,19 * * *"`,
	} {
		if !strings.Contains(configText, fragment) {
			t.Errorf("release fixture config missing %q", fragment)
		}
	}
	loadedConfig, err := config.Load(bytes.NewReader(configFile))
	if err != nil {
		t.Fatalf("release fixture configuration is invalid: %v", err)
	}
	if len(loadedConfig.Projects) != 1 || loadedConfig.Projects[0].Name != "sb-heartbeat-release-fixture" {
		t.Fatalf("release fixture configuration loaded unexpected projects: %+v", loadedConfig.Projects)
	}
}

func TestReleaseToolchainIncludesCurrentSecurityFix(t *testing.T) {
	module, err := os.ReadFile(filepath.Join("..", "..", "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(module), "toolchain go1.26.6\n") {
		t.Fatalf("go.mod must select Go 1.26.6 or a deliberately reviewed successor")
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
