package documentation_test

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestGitHubObservabilityDocumentationDefinesOptInSanitizedContract(t *testing.T) {
	root := filepath.Join("..", "..")
	doc := readEvaluationFile(t, root, "docs", "github-observability.md")
	normalized := strings.Join(strings.Fields(doc), " ")
	for _, required := range []string{
		"--github-annotations",
		"--github-artifact-retention-days",
		"disabled by default",
		"1 through 90",
		"project name",
		"stable status",
		"HTTP status",
		"latency",
		"attempt count",
		"never contains URLs",
		"API keys",
		"response bodies",
		"error messages",
		"workflow_missing_result",
		"observability_sanitization_failed",
		"strictly validates",
		"atomically published",
		"workflow-command escaped",
		"GitHub.com",
		"GitHub Enterprise Server",
		"043fb46d1a93c77aae656e7c1c64a875d1fc6a0a",
	} {
		if !strings.Contains(normalized, required) {
			t.Errorf("GitHub observability docs missing %q", required)
		}
	}
	readme := readEvaluationFile(t, root, "README.md")
	llms := readEvaluationFile(t, root, "llms.txt")
	if !strings.Contains(readme, "[GitHub observability](docs/github-observability.md)") || !strings.Contains(llms, "docs/github-observability.md") {
		t.Fatal("GitHub observability is not indexed")
	}
	spec := readEvaluationFile(t, root, "docs", "product-spec.md")
	if !strings.Contains(spec, "- [x] Richer GitHub annotations and optional durable result artifacts.") {
		t.Fatal("product roadmap does not mark GitHub observability complete")
	}
}
