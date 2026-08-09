package scheduler

import (
	"bytes"
	"errors"
	"fmt"
	"path"
	"regexp"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/croutoncreations/sb-heartbeat/internal/config"
	"github.com/croutoncreations/sb-heartbeat/internal/notification"
)

const (
	checkoutCommit       = "3d3c42e5aac5ba805825da76410c181273ba90b1"
	uploadArtifactCommit = "043fb46d1a93c77aae656e7c1c64a875d1fc6a0a"
	cacheCommit          = "55cc8345863c7cc4c66a329aec7e433d2d1c52a9"
)

var releaseVersionPattern = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)
var configPathPattern = regexp.MustCompile(`^[A-Za-z0-9._/-]+$`)
var environmentNamePattern = regexp.MustCompile(`^[A-Z_][A-Z0-9_]{0,126}$`)

const observabilityJQFilter = `def exact_keys($expected): type == "object" and ((keys | sort) == ($expected | sort)); ` +
	`def integer: type == "number" and floor == .; ` +
	`def timestamp: type == "string" and test("^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(\\.[0-9]+)?(Z|[+-][0-9]{2}:[0-9]{2})$"); ` +
	`def stable_status: . as $value | ["healthy","timeout","dns_failure","tls_failure","credential_rejected","database_permission_denied","api_authentication_failed","temporary_upstream_failure","project_paused","unexpected_response","missing_input","no_matching_row","response_too_large","internal_error"] | index($value) != null; ` +
	`def invocation_code: . as $value | ["invalid_invocation","invalid_configuration","missing_input","credential_rejected","internal_error"] | index($value) != null; ` +
	`def project: . as $project | exact_keys(["name","status","http_status","latency_ms","attempts","error"]) and ($project.name | type == "string" and test("^[a-z][a-z0-9_-]{0,62}$")) and ($project.status | stable_status) and (($project.http_status == null) or ($project.http_status | integer and . >= 100 and . <= 599)) and (($project.latency_ms == null) or ($project.latency_ms | integer and . >= 0)) and ($project.attempts | integer and . >= 0 and . <= 4) and (($project.status == "healthy" and $project.error == null) or ($project.status != "healthy" and ($project.error | exact_keys(["code","message"]) and (.code == $project.status) and (.code | stable_status) and (.message | type == "string")))); ` +
	`def run_envelope: . as $run | exact_keys(["schema_version","started_at","finished_at","success","projects"]) and $run.schema_version == 1 and ($run.started_at | timestamp) and ($run.finished_at | timestamp) and ($run.success | type == "boolean") and ($run.projects | type == "array" and length > 0 and all(.[]; project)) and ($run.success == (all($run.projects[]; .status == "healthy"))); ` +
	`def failure_envelope: . as $failure | exact_keys(["schema_version","success","error"]) and $failure.schema_version == 1 and $failure.success == false and ($failure.error | exact_keys(["code","message"]) and (.code | invocation_code) and (.message | type == "string")); ` +
	`if length != 1 then error("expected one SB Heartbeat result") else .[0] | if run_envelope then {schema_version, started_at, finished_at, success, projects: [.projects[] | {name, status, http_status, latency_ms, attempts}]} elif failure_envelope then {schema_version, success, error: {code: .error.code}} else error("invalid SB Heartbeat result") end end`

type workflowData struct {
	Cron           string
	Version        string
	ConfigPath     string
	TimeoutMinutes int
	Projects       []config.Project
	Annotations    bool
	ArtifactDays   int
	Observability  bool
	ResultFilter   string
	Notifications  bool
	WebhookSecret  string
	NotifyAfter    int
}

type GitHubOptions struct {
	Annotations                  bool
	ArtifactRetentionDays        int
	NotificationWebhookSecret    string
	NotificationWebhookSecretSet bool
	NotifyAfter                  int
	NotifyAfterSet               bool
}

func GitHub(cfg config.Config, version, configPath string) ([]byte, error) {
	return GitHubWithOptions(cfg, version, configPath, GitHubOptions{})
}

func GitHubWithOptions(cfg config.Config, version, configPath string, options GitHubOptions) ([]byte, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if options.ArtifactRetentionDays < 0 || options.ArtifactRetentionDays > 90 {
		return nil, errors.New("artifact retention days must be between 0 and 90")
	}
	if options.NotificationWebhookSecretSet && options.NotificationWebhookSecret == "" {
		return nil, errors.New("notification webhook secret name must not be empty")
	}
	if options.NotificationWebhookSecret == "" {
		if options.NotifyAfterSet || options.NotifyAfter != 0 {
			return nil, errors.New("notification threshold requires a notification webhook secret")
		}
	} else {
		if !environmentNamePattern.MatchString(options.NotificationWebhookSecret) || strings.HasPrefix(options.NotificationWebhookSecret, "GITHUB_") {
			return nil, errors.New("notification webhook secret name is invalid")
		}
		if !options.NotifyAfterSet && options.NotifyAfter == 0 {
			options.NotifyAfter = 3
		}
		if options.NotifyAfter < 1 || options.NotifyAfter > 100 {
			return nil, errors.New("notification threshold must be between 1 and 100")
		}
	}
	if !releaseVersionPattern.MatchString(version) {
		return nil, errors.New("SB Heartbeat version must be an exact release tag such as v0.1.0")
	}
	if usesExplicitGitHubSources(cfg) && !supportsGitHubSources(version) {
		return nil, errors.New("explicit GitHub binding sources require SB Heartbeat v0.2.0 or newer")
	}
	if options.NotificationWebhookSecret != "" && !supportsGitHubSources(version) {
		return nil, errors.New("generated notifications require SB Heartbeat v0.2.0 or newer")
	}
	if configPath == "" || path.IsAbs(configPath) || path.Clean(configPath) != configPath ||
		!configPathPattern.MatchString(configPath) || configPath[0] == '-' || configPath == "." ||
		configPath == ".." || strings.HasPrefix(configPath, "../") {
		return nil, errors.New("workflow config path must be a clean, relative repository path")
	}
	tmpl, err := template.New("github").Funcs(template.FuncMap{
		"quote": strconv.Quote,
		"githubContext": func(source string) string {
			if source == config.GitHubSecret {
				return "secrets"
			}
			return "vars"
		},
	}).Parse(githubWorkflowTemplate)
	if err != nil {
		return nil, fmt.Errorf("parse GitHub workflow template: %w", err)
	}
	var buffer bytes.Buffer
	data := workflowData{
		Cron: cfg.Scheduler.Cron, Version: version, ConfigPath: configPath,
		TimeoutMinutes: workflowTimeoutMinutes(cfg, options.NotificationWebhookSecret != ""), Projects: cfg.Projects,
		Annotations: options.Annotations, ArtifactDays: options.ArtifactRetentionDays,
		Observability: options.Annotations || options.ArtifactRetentionDays > 0,
		ResultFilter:  observabilityJQFilter, Notifications: options.NotificationWebhookSecret != "",
		WebhookSecret: options.NotificationWebhookSecret, NotifyAfter: options.NotifyAfter,
	}
	if err := tmpl.Execute(&buffer, data); err != nil {
		return nil, fmt.Errorf("render GitHub workflow: %w", err)
	}
	return buffer.Bytes(), nil
}

func usesExplicitGitHubSources(cfg config.Config) bool {
	for _, project := range cfg.Projects {
		if project.URL.GitHub != config.GitHubVariable || project.APIKey.GitHub != config.GitHubSecret ||
			project.URL.GitHubSourceExplicit() || project.APIKey.GitHubSourceExplicit() {
			return true
		}
	}
	return false
}

func supportsGitHubSources(version string) bool {
	parts := strings.Split(strings.TrimPrefix(version, "v"), ".")
	major, majorErr := strconv.Atoi(parts[0])
	minor, minorErr := strconv.Atoi(parts[1])
	if majorErr != nil || minorErr != nil {
		return false
	}
	return major > 0 || minor >= 2
}

func workflowTimeoutMinutes(cfg config.Config, notifications bool) int {
	backoff := time.Duration(0)
	for retry := 0; retry < cfg.Defaults.Retries; retry++ {
		delay := cfg.Defaults.RetryBackoff * time.Duration(1<<retry)
		backoff += min(delay, 30*time.Second)
	}
	perBatch := cfg.Defaults.Timeout*time.Duration(cfg.Defaults.Retries+1) + backoff
	batches := (len(cfg.Projects) + cfg.Defaults.Concurrency - 1) / cfg.Defaults.Concurrency
	total := perBatch * time.Duration(batches)
	if notifications {
		total += time.Duration(len(cfg.Projects))*notification.DeliveryTimeout + 30*time.Second
	}
	minutes := int((total+time.Minute-1)/time.Minute) + 2
	return max(5, min(minutes, 360))
}

const githubWorkflowTemplate = `# Generated by SB Heartbeat. Re-run "sb-heartbeat install github" to replace it.
# Scheduled cron expressions use UTC and run the latest commit on the default branch.
# GitHub scheduled workflows may be delayed or dropped, and schedules in inactive public repositories may be disabled; workflow_dispatch remains available.
# Store project URLs as repository variables and low-privilege client keys as repository secrets by default; storage policy remains the repository owner's choice.
name: SB Heartbeat

on:
  schedule:
    - cron: {{ quote .Cron }}
  workflow_dispatch:

permissions:
  contents: read
{{- if .Notifications }}
  actions: read
{{- end }}
{{- if .Notifications }}

concurrency:
  group: sb-heartbeat-${{ "{{" }} github.workflow_ref {{ "}}" }}
  cancel-in-progress: false
{{- end }}

jobs:
{{- if .Notifications }}
  notification-ref-guard:
    if: github.ref != format('refs/heads/{0}', github.event.repository.default_branch)
    runs-on: ubuntu-latest
    steps:
      - name: Reject a non-default notification ref
        shell: bash
        run: |
          echo "Notification workflows must be dispatched from the repository default branch" >&2
          exit 2

{{- end }}
  heartbeat:
{{- if .Notifications }}
    # Durable notification state is intentionally owned by the default branch.
    if: github.ref == format('refs/heads/{0}', github.event.repository.default_branch)
{{- end }}
    runs-on: ubuntu-latest
    timeout-minutes: {{ .TimeoutMinutes }}
    env:
      SB_HEARTBEAT_VERSION: {{ .Version }}
    steps:
      - name: Check out configuration
        uses: actions/checkout@` + checkoutCommit + ` # v7.0.1

      - name: Install pinned SB Heartbeat release
        shell: bash
        run: |
          set -euo pipefail
          version="${SB_HEARTBEAT_VERSION#v}"
          archive="sb-heartbeat_${version}_linux_amd64.tar.gz"
          release="https://github.com/croutoncreations/sb-heartbeat/releases/download/${SB_HEARTBEAT_VERSION}"
          curl --fail --silent --show-error --location --output "${RUNNER_TEMP}/${archive}" "${release}/${archive}"
          curl --fail --silent --show-error --location --output "${RUNNER_TEMP}/checksums.txt" "${release}/checksums.txt"
          grep "  ${archive}$" "${RUNNER_TEMP}/checksums.txt" > "${RUNNER_TEMP}/expected-checksum.txt"
          (cd "${RUNNER_TEMP}" && sha256sum --check --strict expected-checksum.txt)
          mkdir -p "${RUNNER_TEMP}/sb-heartbeat-bin"
          tar -xzf "${RUNNER_TEMP}/${archive}" -C "${RUNNER_TEMP}/sb-heartbeat-bin" sb-heartbeat
          echo "${RUNNER_TEMP}/sb-heartbeat-bin" >> "${GITHUB_PATH}"
{{- if .Notifications }}

      # This cache contains only SB Heartbeat's strictly validated sanitized state.
      - name: Derive notification cache scope
        id: notification-cache-scope
        env:
          SB_HEARTBEAT_WORKFLOW_REF: ${{ "{{" }} github.workflow_ref {{ "}}" }}
        shell: bash
        run: |
          set -euo pipefail
          scope="$(printf '%s' "${SB_HEARTBEAT_WORKFLOW_REF}" | sha256sum | awk '{print $1}')"
          if [[ ! "${scope}" =~ ^[0-9a-f]{64}$ ]]; then
            echo "Could not derive the notification cache scope" >&2
            exit 3
          fi
          echo "scope=${scope}" >> "${GITHUB_OUTPUT}"

      - name: Resolve preceding notification run
        id: notification-predecessor
        env:
          GITHUB_TOKEN: ${{ "{{" }} github.token {{ "}}" }}
        shell: bash
        run: |
          set -euo pipefail
          if (( GITHUB_RUN_ATTEMPT > 1 )); then
            echo "run_id=${GITHUB_RUN_ID}" >> "${GITHUB_OUTPUT}"
            exit 0
          fi
          current_run_path="${RUNNER_TEMP}/sb-heartbeat-current-run.json"
          if ! curl --fail --silent --show-error \
            --connect-timeout 5 \
            --max-time 15 \
            --header "Accept: application/vnd.github+json" \
            --header "Authorization: Bearer ${GITHUB_TOKEN}" \
            --header "X-GitHub-Api-Version: 2022-11-28" \
            --output "${current_run_path}" \
            "${GITHUB_API_URL}/repos/${GITHUB_REPOSITORY}/actions/runs/${GITHUB_RUN_ID}"; then
            echo "Could not identify the notification workflow; state will reset" >&2
            echo "run_id=" >> "${GITHUB_OUTPUT}"
            exit 0
          fi
          workflow_id="$(jq -er '.workflow_id | select(type == "number")' "${current_run_path}" 2>/dev/null || true)"
          if [[ ! "${workflow_id}" =~ ^[0-9]+$ ]]; then
            echo "Could not identify the notification workflow; state will reset" >&2
            echo "run_id=" >> "${GITHUB_OUTPUT}"
            exit 0
          fi
          runs_path="${RUNNER_TEMP}/sb-heartbeat-notification-runs.json"
          if ! curl --fail --silent --show-error \
            --connect-timeout 5 \
            --max-time 15 \
            --header "Accept: application/vnd.github+json" \
            --header "Authorization: Bearer ${GITHUB_TOKEN}" \
            --header "X-GitHub-Api-Version: 2022-11-28" \
            --get \
            --data-urlencode "branch=${GITHUB_REF_NAME}" \
            --data-urlencode "per_page=100" \
            --output "${runs_path}" \
            "${GITHUB_API_URL}/repos/${GITHUB_REPOSITORY}/actions/workflows/${workflow_id}/runs"; then
            echo "Could not resolve the preceding notification run; state will reset" >&2
            echo "run_id=" >> "${GITHUB_OUTPUT}"
            exit 0
          fi
          preceding_run_id="$(jq -er --argjson current "${GITHUB_RUN_NUMBER}" '
            .workflow_runs
            | map(select((.run_number | type == "number") and .run_number < $current and (.id | type == "number")))
            | sort_by(.run_number)
            | reverse
            | (.[0].id // "")
          ' "${runs_path}" 2>/dev/null || true)"
          if [[ -n "${preceding_run_id}" && ! "${preceding_run_id}" =~ ^[0-9]+$ ]]; then
            preceding_run_id=""
          fi
          echo "run_id=${preceding_run_id}" >> "${GITHUB_OUTPUT}"

      - name: Restore sanitized notification state
        uses: actions/cache/restore@` + cacheCommit + ` # v6.1.0
        with:
          path: |
            ${{ "{{" }} runner.temp {{ "}}" }}/sb-heartbeat-notifications/notifications.json
            ${{ "{{" }} runner.temp {{ "}}" }}/sb-heartbeat-notifications/run-id
          key: sb-heartbeat-notifications-${{ "{{" }} steps.notification-cache-scope.outputs.scope {{ "}}" }}-${{ "{{" }} github.run_id {{ "}}" }}-${{ "{{" }} github.run_attempt {{ "}}" }}
          restore-keys: |
            sb-heartbeat-notifications-${{ "{{" }} steps.notification-cache-scope.outputs.scope {{ "}}" }}-
{{- end }}

      - name: Run heartbeats
{{- if .Notifications }}
        id: heartbeat
{{- end }}
        env:
{{- range .Projects }}
          {{ .URL.Env }}: ${{ "{{" }} {{ githubContext .URL.GitHub }}.{{ .URL.Env }} {{ "}}" }}
          {{ .APIKey.Env }}: ${{ "{{" }} {{ githubContext .APIKey.GitHub }}.{{ .APIKey.Env }} {{ "}}" }}
{{- end }}
{{- if .Notifications }}
          SB_HEARTBEAT_NOTIFICATION_WEBHOOK: ${{ "{{" }} secrets.{{ .WebhookSecret }} {{ "}}" }}
{{- end }}
        shell: bash
        run: |
          set -o pipefail
{{- if .Notifications }}
          state_directory="${RUNNER_TEMP}/sb-heartbeat-notifications"
          state_path="${RUNNER_TEMP}/sb-heartbeat-notifications/notifications.json"
          sequence_path="${RUNNER_TEMP}/sb-heartbeat-notifications/run-id"
          expected_sequence="${{ "{{" }} steps.notification-predecessor.outputs.run_id {{ "}}" }}"
          if (( GITHUB_RUN_ATTEMPT > 1 )); then
            expected_sequence="${GITHUB_RUN_ID}"
          fi
          restored_sequence=""
          if [[ -f "${sequence_path}" && ! -L "${sequence_path}" ]] &&
             [[ "$(wc -l < "${sequence_path}")" -eq 1 ]] &&
             [[ "$(wc -c < "${sequence_path}")" -le 32 ]] &&
             grep -Eq '^[0-9]+$' "${sequence_path}"; then
            IFS= read -r restored_sequence < "${sequence_path}"
          fi
          if [[ -e "${state_path}" || -L "${state_path}" || -e "${sequence_path}" || -L "${sequence_path}" ]]; then
            if [[ ! -f "${state_path}" || -L "${state_path}" || "${restored_sequence}" != "${expected_sequence}" ]]; then
              rm -rf -- "${state_path}" "${sequence_path}"
            fi
          fi
          state_before=missing
          if [[ -f "${state_path}" && ! -L "${state_path}" ]]; then
            state_before="$(sha256sum "${state_path}" | awk '{print $1}')"
          fi
{{- end }}
          set +e
          sb-heartbeat --config {{ .ConfigPath }} run --output json{{ if .Notifications }} --notification-state "${state_path}" --notification-webhook-env SB_HEARTBEAT_NOTIFICATION_WEBHOOK --notify-after {{ .NotifyAfter }}{{ end }} | tee "${RUNNER_TEMP}/sb-heartbeat-result.json"
          status=${PIPESTATUS[0]}
          set -e
{{- if .Notifications }}
          state_changed=false
          if [[ -f "${state_path}" && ! -L "${state_path}" ]]; then
            state_after="$(sha256sum "${state_path}" | awk '{print $1}')"
            if [[ "${state_after}" != "${state_before}" ]]; then
              state_changed=true
            fi
          fi
          if [[ "${state_changed}" == true ]]; then
            mkdir -p "${state_directory}"
            printf '%s\n' "${GITHUB_RUN_ID}" > "${sequence_path}.tmp"
            mv "${sequence_path}.tmp" "${sequence_path}"
          fi
          echo "status=${status}" >> "${GITHUB_OUTPUT}"
          echo "state_changed=${state_changed}" >> "${GITHUB_OUTPUT}"
{{- end }}
          {
            echo '## SB Heartbeat heartbeat results'
            echo '~~~json'
            cat "${RUNNER_TEMP}/sb-heartbeat-result.json"
            echo '~~~'
          } >> "${GITHUB_STEP_SUMMARY}"
{{- if not .Notifications }}
          exit "${status}"
{{- end }}
{{- if .Notifications }}

      - name: Save sanitized notification state
        if: always() && steps.heartbeat.outputs.state_changed == 'true'
        uses: actions/cache/save@` + cacheCommit + ` # v6.1.0
        with:
          path: |
            ${{ "{{" }} runner.temp {{ "}}" }}/sb-heartbeat-notifications/notifications.json
            ${{ "{{" }} runner.temp {{ "}}" }}/sb-heartbeat-notifications/run-id
          key: sb-heartbeat-notifications-${{ "{{" }} steps.notification-cache-scope.outputs.scope {{ "}}" }}-${{ "{{" }} github.run_id {{ "}}" }}-${{ "{{" }} github.run_attempt {{ "}}" }}
{{- end }}
{{- if .Observability }}

      - name: Prepare sanitized observability result
        if: always()
        shell: bash
        run: |
          set -euo pipefail
          raw_result="${RUNNER_TEMP}/sb-heartbeat-result.json"
          safe_result="${RUNNER_TEMP}/sb-heartbeat-observability.json"
          candidate_result="${RUNNER_TEMP}/sb-heartbeat-observability.json.tmp"
          rm -f "${safe_result}" "${candidate_result}"
          if [[ ! -s "${raw_result}" ]]; then
{{- if .Annotations }}
            echo '::error title=SB Heartbeat invocation failure::workflow_missing_result'
{{- end }}
            exit 0
          fi
          if ! jq --exit-status --slurp '{{ .ResultFilter }}' "${raw_result}" > "${candidate_result}"; then
            rm -f "${candidate_result}"
{{- if .Annotations }}
            echo '::error title=SB Heartbeat observability failure::observability_sanitization_failed'
{{- end }}
            exit 1
          fi
          mv "${candidate_result}" "${safe_result}"
{{- if .Annotations }}
          escape_workflow_command() {
            local value="$1"
            value=${value//'%'/'%25'}
            value=${value//$'\r'/'%0D'}
            value=${value//$'\n'/'%0A'}
            printf '%s' "${value}"
          }
          while IFS=$'\t' read -r name status; do
            [[ -z "${name}" || -z "${status}" ]] && continue
            printf '::error title=SB Heartbeat project failure::%s: %s\n' "$(escape_workflow_command "${name}")" "$(escape_workflow_command "${status}")"
          done < <(jq -r '.projects[]? | select(.status != "healthy") | [.name, .status] | @tsv' "${safe_result}")
          failure_code="$(jq -r '.error.code? // empty' "${safe_result}")"
          if [[ -n "${failure_code}" ]]; then
            printf '::error title=SB Heartbeat invocation failure::%s\n' "$(escape_workflow_command "${failure_code}")"
          fi
{{- end }}
{{- end }}
{{- if gt .ArtifactDays 0 }}

      - name: Upload sanitized heartbeat result
        if: always()
        uses: actions/upload-artifact@` + uploadArtifactCommit + ` # v7.0.1
        with:
          name: sb-heartbeat-results-${{ "{{" }} github.run_id {{ "}}" }}-${{ "{{" }} github.run_attempt {{ "}}" }}
          path: ${{ "{{" }} runner.temp {{ "}}" }}/sb-heartbeat-observability.json
          if-no-files-found: warn
          retention-days: {{ .ArtifactDays }}
{{- end }}
{{- if .Notifications }}

      - name: Propagate heartbeat exit status
        if: always()
        shell: bash
        run: |
          set -euo pipefail
          status="${{ "{{" }} steps.heartbeat.outputs.status {{ "}}" }}"
          if [[ ! "${status}" =~ ^[0-3]$ ]]; then
            echo "SB Heartbeat did not produce a valid exit status" >&2
            exit 3
          fi
          exit "${status}"
{{- end }}
`
