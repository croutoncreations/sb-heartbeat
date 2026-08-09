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
)

const (
	checkoutCommit       = "3d3c42e5aac5ba805825da76410c181273ba90b1"
	uploadArtifactCommit = "043fb46d1a93c77aae656e7c1c64a875d1fc6a0a"
)

var releaseVersionPattern = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)
var configPathPattern = regexp.MustCompile(`^[A-Za-z0-9._/-]+$`)

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
}

type GitHubOptions struct {
	Annotations           bool
	ArtifactRetentionDays int
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
	if !releaseVersionPattern.MatchString(version) {
		return nil, errors.New("SB Heartbeat version must be an exact release tag such as v0.1.0")
	}
	if usesExplicitGitHubSources(cfg) && !supportsGitHubSources(version) {
		return nil, errors.New("explicit GitHub binding sources require SB Heartbeat v0.2.0 or newer")
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
		TimeoutMinutes: workflowTimeoutMinutes(cfg), Projects: cfg.Projects,
		Annotations: options.Annotations, ArtifactDays: options.ArtifactRetentionDays,
		Observability: options.Annotations || options.ArtifactRetentionDays > 0,
		ResultFilter:  observabilityJQFilter,
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

func workflowTimeoutMinutes(cfg config.Config) int {
	backoff := time.Duration(0)
	for retry := 0; retry < cfg.Defaults.Retries; retry++ {
		delay := cfg.Defaults.RetryBackoff * time.Duration(1<<retry)
		backoff += min(delay, 30*time.Second)
	}
	perBatch := cfg.Defaults.Timeout*time.Duration(cfg.Defaults.Retries+1) + backoff
	batches := (len(cfg.Projects) + cfg.Defaults.Concurrency - 1) / cfg.Defaults.Concurrency
	minutes := int((perBatch*time.Duration(batches)+time.Minute-1)/time.Minute) + 2
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

jobs:
  heartbeat:
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

      - name: Run heartbeats
        env:
{{- range .Projects }}
          {{ .URL.Env }}: ${{ "{{" }} {{ githubContext .URL.GitHub }}.{{ .URL.Env }} {{ "}}" }}
          {{ .APIKey.Env }}: ${{ "{{" }} {{ githubContext .APIKey.GitHub }}.{{ .APIKey.Env }} {{ "}}" }}
{{- end }}
        shell: bash
        run: |
          set -o pipefail
          set +e
          sb-heartbeat --config {{ .ConfigPath }} run --output json | tee "${RUNNER_TEMP}/sb-heartbeat-result.json"
          status=${PIPESTATUS[0]}
          set -e
          {
            echo '## SB Heartbeat heartbeat results'
            echo '~~~json'
            cat "${RUNNER_TEMP}/sb-heartbeat-result.json"
            echo '~~~'
          } >> "${GITHUB_STEP_SUMMARY}"
          exit "${status}"
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
`
