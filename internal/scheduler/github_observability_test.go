package scheduler

import (
	"bytes"
	"os/exec"
	"strings"
	"testing"
)

func runObservabilityFilter(t *testing.T, input string) ([]byte, error) {
	t.Helper()
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq is not installed")
	}
	command := exec.Command("jq", "--compact-output", "--slurp", observabilityJQFilter)
	command.Stdin = strings.NewReader(input)
	return command.Output()
}

func TestObservabilityFilterRemovesMessagesAndUnapprovedFields(t *testing.T) {
	input := `{
  "schema_version": 1,
  "started_at": "2026-08-09T00:00:00Z",
  "finished_at": "2026-08-09T00:00:01Z",
  "success": false,
  "projects": [{
    "name": "stage",
    "status": "unexpected_response",
    "http_status": 500,
    "latency_ms": 20,
    "attempts": 2,
    "error": {"code": "unexpected_response", "message": "sb_secret_must_not_persist https://abcdefghijklmnopqrst.supabase.co"}
  }]
}`
	output, err := runObservabilityFilter(t, input)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range [][]byte{[]byte("sb_secret"), []byte("supabase.co"), []byte("message")} {
		if bytes.Contains(output, forbidden) {
			t.Fatalf("sanitized output contains %q: %s", forbidden, output)
		}
	}
	for _, required := range [][]byte{[]byte(`"name":"stage"`), []byte(`"status":"unexpected_response"`)} {
		if !bytes.Contains(output, required) {
			t.Fatalf("sanitized output missing %q: %s", required, output)
		}
	}
}

func TestObservabilityFilterRetainsOnlyFailureCode(t *testing.T) {
	input := `{"schema_version":1,"success":false,"error":{"code":"missing_input","message":"SECRET_ENV is missing"}}`
	output, err := runObservabilityFilter(t, input)
	if err != nil {
		t.Fatal(err)
	}
	if string(output) != "{\"schema_version\":1,\"success\":false,\"error\":{\"code\":\"missing_input\"}}\n" {
		t.Fatalf("sanitized failure = %s", output)
	}
}

func TestObservabilityFilterRejectsMalformedOrInjectableEnvelopes(t *testing.T) {
	validRun := `{"schema_version":1,"started_at":"2026-08-09T00:00:00Z","finished_at":"2026-08-09T00:00:01Z","success":false,"projects":[{"name":"stage","status":"unexpected_response","http_status":500,"latency_ms":20,"attempts":2,"error":{"code":"unexpected_response","message":"safe diagnostic"}}]}`
	tests := map[string]string{
		"unsupported schema":  strings.Replace(validRun, `"schema_version":1`, `"schema_version":999`, 1),
		"non boolean success": strings.Replace(validRun, `"success":false`, `"success":"false"`, 1),
		"invalid timestamp":   strings.Replace(validRun, `"2026-08-09T00:00:00Z"`, `"not-a-time"`, 1),
		"name injection":      strings.Replace(validRun, `"name":"stage"`, `"name":"stage%0A::error::injected"`, 1),
		"status injection":    strings.Replace(validRun, `"status":"unexpected_response"`, `"status":"unexpected_response\n::error::injected"`, 1),
		"status mismatch":     strings.Replace(validRun, `"code":"unexpected_response"`, `"code":"timeout"`, 1),
		"bad http status":     strings.Replace(validRun, `"http_status":500`, `"http_status":700`, 1),
		"negative latency":    strings.Replace(validRun, `"latency_ms":20`, `"latency_ms":-1`, 1),
		"too many attempts":   strings.Replace(validRun, `"attempts":2`, `"attempts":5`, 1),
		"extra project field": strings.Replace(validRun, `"message":"safe diagnostic"`, `"message":"safe diagnostic","url":"https://secret.supabase.co"`, 1),
		"unstable error code": `{"schema_version":1,"success":false,"error":{"code":"SECRET_ENV is missing","message":"diagnostic"}}`,
		"multiple documents":  validRun + "\n" + validRun,
	}
	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			if output, err := runObservabilityFilter(t, input); err == nil {
				t.Fatalf("malformed input accepted: %s", output)
			}
		})
	}
}
