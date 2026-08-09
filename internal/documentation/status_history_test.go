package documentation_test

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestStatusHistoryDocumentationDefinesSafeOperationalContract(t *testing.T) {
	root := filepath.Join("..", "..")
	doc := readEvaluationFile(t, root, "docs", "status-history.md")
	for _, required := range []string{
		"--history /absolute/private/path/history.json",
		"--history-limit",
		"100",
		"1,000",
		"1 MiB",
		"0600",
		"atomic",
		"last writer wins",
		"never includes",
		"URLs",
		"API keys",
		"response bodies",
		"error messages",
		"outside the repository",
		"unavailable on Windows",
	} {
		if !strings.Contains(doc, required) {
			t.Errorf("status history docs missing %q", required)
		}
	}
	readme := readEvaluationFile(t, root, "README.md")
	llms := readEvaluationFile(t, root, "llms.txt")
	if !strings.Contains(readme, "[Local status history](docs/status-history.md)") || !strings.Contains(llms, "docs/status-history.md") {
		t.Fatal("status history is not indexed for people and agents")
	}
	spec := readEvaluationFile(t, root, "docs", "product-spec.md")
	if !strings.Contains(spec, "- [x] Local status history with atomic writes and no sensitive data.") {
		t.Fatal("product roadmap does not mark local history complete")
	}
}
