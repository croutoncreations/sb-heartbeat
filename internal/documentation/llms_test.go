package documentation_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestLLMsDiscoveryDocumentDefinesSafeAgentContract(t *testing.T) {
	contents, err := os.ReadFile(filepath.Join("..", "..", "llms.txt"))
	if err != nil {
		t.Fatalf("read llms.txt: %v", err)
	}
	text := string(contents)
	required := []string{
		"# SB Heartbeat",
		"fixed, read-only heartbeat",
		"publishable or legacy anon key",
		"exact, checksum-verified release",
		"Generate migration SQL; never apply it without explicit authorization",
		"Never modify a downstream repository's `AGENTS.md`, `CLAUDE.md`, or equivalent instruction files",
		"Never print, persist, or commit credential values",
		"Never use or request a database password, Supabase secret key, service-role key, or Management API token",
		"When the required low-privilege values are already available in the environment, run `doctor` before enabling a scheduler",
		"## Essential documentation",
		"## Agent tasks",
	}
	for _, fragment := range required {
		if !strings.Contains(text, fragment) {
			t.Errorf("llms.txt missing %q", fragment)
		}
	}
}

func TestLLMsDiscoveryFollowsStructuredIndexFormat(t *testing.T) {
	contents, err := os.ReadFile(filepath.Join("..", "..", "llms.txt"))
	if err != nil {
		t.Fatalf("read llms.txt: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(contents)), "\n")
	if len(lines) < 3 || lines[0] != "# SB Heartbeat" || !strings.HasPrefix(lines[2], "> ") {
		t.Fatalf("llms.txt must start with one H1 and a blockquote summary: %q", lines[:min(3, len(lines))])
	}
	seenSection := false
	for index, line := range lines[1:] {
		if strings.HasPrefix(line, "# ") {
			t.Fatalf("unexpected additional H1 on line %d", index+2)
		}
		if strings.HasPrefix(line, "## ") {
			seenSection = true
			continue
		}
		if seenSection && line != "" && !strings.HasPrefix(line, "- [") {
			t.Fatalf("section content on line %d is not a link-list item: %q", index+2, line)
		}
	}
	if !seenSection {
		t.Fatal("llms.txt has no H2 file-list section")
	}
}

func TestLLMsDiscoveryLinksResolveToExpectedDocumentation(t *testing.T) {
	contents, err := os.ReadFile(filepath.Join("..", "..", "llms.txt"))
	if err != nil {
		t.Fatalf("read llms.txt: %v", err)
	}
	links := regexp.MustCompile(`\[[^]]+\]\(https://github\.com/croutoncreations/sb-heartbeat/blob/main/([^)#]+)(?:#[^)]*)?\)`).FindAllStringSubmatch(string(contents), -1)
	want := map[string]bool{
		"README.md": false, "docs/quickstart.md": false, "docs/configuration.md": false,
		"docs/security.md": false, "docs/github-actions.md": false,
		"docs/troubleshooting.md": false, "docs/product-spec.md": false,
		"docs/agent-install.md": false, "docs/agent-prompts.md": false,
		"docs/agent-evaluation.md": false, "docs/local-cron.md": false,
		"docs/docker.md": false, "docs/uninstall.md": false,
	}
	if len(links) != len(want) {
		t.Fatalf("documentation links = %d, want %d", len(links), len(want))
	}
	root := filepath.Join("..", "..")
	for _, match := range links {
		linked := match[1]
		if _, ok := want[linked]; !ok {
			t.Errorf("unexpected documentation link %q", linked)
			continue
		}
		want[linked] = true
		path := filepath.Join(root, filepath.FromSlash(linked))
		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("linked documentation %q: %v", match[1], err)
		} else if !info.Mode().IsRegular() {
			t.Errorf("linked documentation %q is not a regular file", linked)
		}
	}
	for path, found := range want {
		if !found {
			t.Errorf("missing documentation link %q", path)
		}
	}
}

func TestLLMsDiscoveryIsIndexedAndTrackedAsComplete(t *testing.T) {
	root := filepath.Join("..", "..")
	readme, err := os.ReadFile(filepath.Join(root, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(readme), "[LLM and coding-agent index](llms.txt)") {
		t.Fatal("README does not index llms.txt")
	}
	spec, err := os.ReadFile(filepath.Join(root, "docs", "product-spec.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(spec), "- [x] `llms.txt`") {
		t.Fatal("product roadmap does not mark llms.txt complete")
	}
}
