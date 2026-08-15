package documentation_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const currentDocumentationVersion = "v0.3.2"

func TestCurrentUserCommandsUseCurrentRelease(t *testing.T) {
	root := filepath.Join("..", "..")
	want := map[string][]string{
		"README.md": {
			"github.com/croutoncreations/sb-heartbeat/cmd/sb-heartbeat@" + currentDocumentationVersion,
			"install github --sb-heartbeat-version " + currentDocumentationVersion,
		},
		"docs/quickstart.md": {
			"github.com/croutoncreations/sb-heartbeat/cmd/sb-heartbeat@" + currentDocumentationVersion,
		},
		"docs/github-actions.md": {
			"install github --sb-heartbeat-version " + currentDocumentationVersion,
		},
		"docs/github-observability.md": {
			"--sb-heartbeat-version " + currentDocumentationVersion,
		},
		"docs/notifications.md": {
			"--sb-heartbeat-version " + currentDocumentationVersion,
		},
		"docs/docker.md": {
			"ghcr.io/croutoncreations/sb-heartbeat:" + currentDocumentationVersion,
		},
		"docs/releasing.md": {
			"gh release verify " + currentDocumentationVersion,
			"sb-heartbeat_0.3.2_linux_amd64.tar.gz",
		},
	}

	for name, fragments := range want {
		contents, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		for _, fragment := range fragments {
			if !strings.Contains(string(contents), fragment) {
				t.Errorf("%s does not use current release in %q", name, fragment)
			}
		}
	}
}

func TestPublishedDocsHaveNoProjectSpecificFixtureNames(t *testing.T) {
	root := filepath.Join("..", "..")
	for _, name := range []string{"README.md", "llms.txt", "docs"} {
		err := filepath.WalkDir(filepath.Join(root, name), func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || filepath.Ext(path) != ".md" && filepath.Base(path) != "llms.txt" {
				return nil
			}
			contents, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			for _, project := range []string{"ToneClone", "toneclone", "Nibit", "nibit", "MyStoryMates", "mystorymates"} {
				if strings.Contains(string(contents), project) {
					t.Errorf("%s contains downstream project name %q", path, project)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("scan %s: %v", name, err)
		}
	}
}

func TestProductSpecReflectsImplementedProjectState(t *testing.T) {
	contents, err := os.ReadFile(filepath.Join("..", "..", "docs", "product-spec.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(contents)
	for _, required := range []string{
		"Status: Maintained implementation plan",
		"Last updated: 2026-08-15",
		"A basic public-project name and search review is complete",
	} {
		if !strings.Contains(text, required) {
			t.Errorf("product spec missing %q", required)
		}
	}
	for _, stale := range []string{
		"Status: Draft for implementation",
		"broader live Cloudflare coverage for both supported key forms",
		"A formal trademark check remains a release prerequisite",
	} {
		if strings.Contains(text, stale) {
			t.Errorf("product spec retains stale statement %q", stale)
		}
	}
}

func TestQuickstartDoesNotRegenerateTheMigrationWrittenByInit(t *testing.T) {
	contents, err := os.ReadFile(filepath.Join("..", "..", "docs", "quickstart.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(contents)
	if !strings.Contains(text, "## 3. Review and apply SQL") {
		t.Fatal("quickstart must review the migration already written by init")
	}
	if strings.Contains(text, "## 3. Generate and apply SQL") {
		t.Fatal("quickstart must not regenerate an existing collision-guarded migration")
	}
}

func TestRelativeMarkdownLinksResolve(t *testing.T) {
	root := filepath.Join("..", "..")
	linkPattern := regexp.MustCompile(`\[[^]]*\]\(([^)]+)\)`)
	paths := []string{filepath.Join(root, "README.md"), filepath.Join(root, "CONTRIBUTING.md"), filepath.Join(root, "SECURITY.md")}
	docs, err := filepath.Glob(filepath.Join(root, "docs", "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	paths = append(paths, docs...)

	for _, source := range paths {
		contents, readErr := os.ReadFile(source)
		if readErr != nil {
			t.Fatalf("read %s: %v", source, readErr)
		}
		markdown := regexp.MustCompile("(?s)```.*?```").ReplaceAllString(string(contents), "")
		markdown = regexp.MustCompile("`[^`\\n]*`").ReplaceAllString(markdown, "")
		for _, match := range linkPattern.FindAllStringSubmatch(markdown, -1) {
			target := strings.TrimSpace(match[1])
			if target == "" || strings.HasPrefix(target, "#") || strings.Contains(target, "://") || strings.HasPrefix(target, "mailto:") {
				continue
			}
			target = strings.SplitN(target, "#", 2)[0]
			if target == "" {
				continue
			}
			resolved := filepath.Clean(filepath.Join(filepath.Dir(source), filepath.FromSlash(target)))
			info, statErr := os.Stat(resolved)
			if statErr != nil {
				t.Errorf("%s links to missing %q: %v", source, target, statErr)
			} else if !info.Mode().IsRegular() && !info.IsDir() {
				t.Errorf("%s links to unsupported target %q", source, target)
			}
		}
	}
}
