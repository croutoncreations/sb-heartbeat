package documentation_test

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestNotificationDocumentationCoversSafeOperation(t *testing.T) {
	root := filepath.Join("..", "..")
	doc := readEvaluationFile(t, root, "docs", "notifications.md")
	for _, required := range []string{
		"--notification-state",
		"--notification-webhook-env",
		"--notify-after",
		"HTTPS",
		"consecutive",
		"at least once",
		"environment variable",
		"does not contain",
		"single writer",
		"Windows",
		"no recovery notification",
	} {
		if !strings.Contains(doc, required) {
			t.Errorf("notification docs missing %q", required)
		}
	}
	readme := readEvaluationFile(t, root, "README.md")
	llms := readEvaluationFile(t, root, "llms.txt")
	if !strings.Contains(readme, "[Repeated-failure notifications](docs/notifications.md)") || !strings.Contains(llms, "docs/notifications.md") {
		t.Fatal("notification documentation is not indexed for people and agents")
	}
	spec := readEvaluationFile(t, root, "docs", "product-spec.md")
	if !strings.Contains(spec, "- [x] Notifications after configurable repeated failures.") {
		t.Fatal("notification roadmap item is not complete")
	}
}
