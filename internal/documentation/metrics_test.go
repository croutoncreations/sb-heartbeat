package documentation_test

import (
	"os"
	"strings"
	"testing"
)

func TestMetricsDocumentationDefinesSanitizedPrometheusContract(t *testing.T) {
	contents, err := os.ReadFile("../../docs/metrics.md")
	if err != nil {
		t.Fatal(err)
	}
	doc := string(contents)
	for _, required := range []string{
		"--metrics",
		"Prometheus textfile collector",
		"atomically",
		"sb_heartbeat_run_success",
		"sb_heartbeat_project_status",
		"diagnostic messages",
		"does not start",
	} {
		if !strings.Contains(doc, required) {
			t.Errorf("metrics docs missing %q", required)
		}
	}
}
