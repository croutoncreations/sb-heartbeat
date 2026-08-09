package documentation_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStopHeartbeatGuideIsSafeOrderedAndComplete(t *testing.T) {
	root := filepath.Join("..", "..")
	contents, err := os.ReadFile(filepath.Join(root, "docs", "uninstall.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(contents)
	normalized := strings.Join(strings.Fields(text), " ")
	for _, required := range []string{
		"Stop heartbeats and uninstall",
		"Disable scheduling first",
		"one project",
		"all projects",
		"shared",
		"sb-heartbeat migration uninstall --output",
		"generates SQL only",
		"does not apply",
		"archive",
		"GitHub Actions",
		"local cron",
		"keep the shared cron entry",
		"currently pinned exact version",
		"--force",
		"migration path deploys only to the identified Supabase project",
		"do not add uninstall SQL to that shared migration stream",
		"derive `variable` or `secret`",
		"permanently delete",
		"does not provide a separate archive operation",
	} {
		if !strings.Contains(normalized, required) {
			t.Errorf("lifecycle guide missing %q", required)
		}
	}
	if scheduler, migration := strings.Index(text, "Disable scheduling first"), strings.Index(text, "migration uninstall"); scheduler < 0 || migration < 0 || scheduler > migration {
		t.Fatal("lifecycle guide does not disable scheduling before database cleanup")
	}
}

func TestStopHeartbeatLifecycleIsDiscoverableAndAgentReady(t *testing.T) {
	root := filepath.Join("..", "..")
	readme := readEvaluationFile(t, root, "README.md")
	if !strings.Contains(readme, "[Stop heartbeats and uninstall](docs/uninstall.md)") {
		t.Fatal("README does not index lifecycle guidance")
	}
	llms := readEvaluationFile(t, root, "llms.txt")
	if !strings.Contains(llms, "docs/uninstall.md") {
		t.Fatal("llms.txt does not index lifecycle guidance")
	}
	prompts := readEvaluationFile(t, root, "docs", "agent-prompts.md")
	for _, required := range []string{"## Stop heartbeats safely", "disable only the identified project's scheduling first", "never apply uninstall SQL", "shared scheduler", "binding source"} {
		if !strings.Contains(prompts, required) {
			t.Errorf("agent lifecycle prompt missing %q", required)
		}
	}
	spec := readEvaluationFile(t, root, "docs", "product-spec.md")
	if !strings.Contains(spec, "- [x] Make it easy to stop heartbeats for projects users should archive.") {
		t.Fatal("product roadmap does not mark lifecycle guidance complete")
	}
}
