package documentation_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/croutoncreations/sb-heartbeat/internal/config"
	"github.com/croutoncreations/sb-heartbeat/internal/migration"
)

type evaluationManifest struct {
	SchemaVersion  int               `json:"schema_version"`
	Agent          string            `json:"agent"`
	Model          string            `json:"model"`
	Reasoning      string            `json:"reasoning"`
	SourceRevision string            `json:"source_revision"`
	BinarySHA256   string            `json:"binary_sha256"`
	LegacySHA256   string            `json:"legacy_binary_sha256"`
	FixtureSHA256  string            `json:"fixture_sha256"`
	ResponseSHA256 string            `json:"final_response_sha256"`
	Generated      map[string]string `json:"generated_files"`
	Checks         map[string]bool   `json:"checks"`
}

var unsafeEvaluationMaterial = regexp.MustCompile(`(?i)(sb_publishable_|sb_secret_|postgres(?:ql)?://|eyJ[A-Za-z0-9_-]{20,}\.|https://[a-z]{20}\.supabase\.co)`)

const (
	evaluationSourceRevision  = "657c1924e158d1ea0e1727445995f6c20ed8d39f"
	evaluatorBinarySHA256     = "636c6d20baff6c2a06d364d937e82d03f5a345f6ab8c61b78a9af987ae1c7b28"
	publishedV011BinarySHA256 = "79aea2eebe163c290d76136aedb4caa9bf1171b769f09bf76740cd5086f679dd"
)

func TestAgentInstallationEvaluationFixtureIsCredentialFreeAndBounded(t *testing.T) {
	root := filepath.Join("..", "..")
	fixture := filepath.Join(root, "testdata", "agent-install")
	want := map[string]bool{
		"downstream-instructions.txt": false,
		"project-brief.md":            false,
	}
	total := int64(0)
	err := filepath.WalkDir(fixture, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == fixture || entry.IsDir() && filepath.Base(path) == "results" || strings.Contains(path, string(filepath.Separator)+"results"+string(filepath.Separator)) {
			if entry.IsDir() && filepath.Base(path) == "results" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(fixture, path)
		if err != nil {
			return err
		}
		if _, ok := want[rel]; !ok {
			t.Errorf("unexpected fixture entry %q", rel)
			return nil
		}
		want[rel] = true
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Size() > 16*1024 {
			t.Errorf("fixture entry %q is not a bounded regular file", rel)
		}
		total += info.Size()
		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if unsafeEvaluationMaterial.Match(contents) {
			t.Errorf("fixture entry %q contains credential-like material", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if total > 32*1024 {
		t.Fatalf("fixture size = %d, want at most 32 KiB", total)
	}
	for path, found := range want {
		if !found {
			t.Errorf("missing fixture file %q", path)
		}
	}
	brief := readEvaluationFile(t, root, "testdata", "agent-install", "project-brief.md")
	for _, guide := range []string{"docs/agent-install.md", "docs/agent-prompts.md", "Prepare an installation"} {
		if !strings.Contains(brief, guide) {
			t.Errorf("evaluation brief does not direct agents to %q", guide)
		}
	}
}

func TestAgentInstallationEvaluationRetainsVerifiableIndependentRuns(t *testing.T) {
	root := filepath.Join("..", "..")
	fixtureRoot := filepath.Join(root, "testdata", "agent-install")
	resultsRoot := filepath.Join(fixtureRoot, "results")
	fixtureHash := hashFiles(t, fixtureRoot, []string{"downstream-instructions.txt", "project-brief.md"})
	for run, expectedAgent := range map[string]string{"agent-a": "Agent A", "agent-b": "Agent B"} {
		t.Run(run, func(t *testing.T) {
			resultRoot := filepath.Join(resultsRoot, run)
			var manifest evaluationManifest
			decodeEvaluationJSON(t, filepath.Join(resultRoot, "manifest.json"), &manifest)
			if manifest.SchemaVersion != 2 || manifest.Agent != expectedAgent || manifest.Model != "gpt-5.6-sol" || manifest.Reasoning != "high" {
				t.Fatalf("manifest identity = %+v", manifest)
			}
			if !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(manifest.SourceRevision) ||
				!regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(manifest.BinarySHA256) ||
				!regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(manifest.LegacySHA256) {
				t.Fatalf("manifest provenance is invalid: %+v", manifest)
			}
			if manifest.SourceRevision != evaluationSourceRevision || manifest.BinarySHA256 != evaluatorBinarySHA256 ||
				manifest.LegacySHA256 != publishedV011BinarySHA256 {
				t.Fatalf("manifest provenance does not match the reviewed evaluation: %+v", manifest)
			}
			if manifest.FixtureSHA256 != fixtureHash {
				t.Fatalf("fixture hash = %s, want %s", manifest.FixtureSHA256, fixtureHash)
			}
			if len(manifest.Generated) != 3 {
				t.Fatalf("generated files = %v", manifest.Generated)
			}
			for path, wantHash := range manifest.Generated {
				contents := readEvaluationFile(t, snapshotPath(resultsRoot, path))
				if got := sha256Hex([]byte(contents)); got != wantHash {
					t.Errorf("%s hash = %s, want %s", path, got, wantHash)
				}
				if unsafeEvaluationMaterial.MatchString(contents) {
					t.Errorf("%s contains credential-like material", path)
				}
			}
			assertEvaluationArtifacts(t, resultsRoot, manifest)
			for _, check := range []string{
				"published_guides_read", "instructions_unchanged", "migration_exact", "workflow_actionlint",
				"actual_pinned_release_parsed_config", "no_credentials", "no_additional_commit", "manual_steps_reported",
			} {
				if !manifest.Checks[check] {
					t.Errorf("check %q did not pass", check)
				}
			}
			response := readEvaluationFile(t, resultRoot, "final-response.md")
			if sha256Hex([]byte(response)) != manifest.ResponseSHA256 {
				t.Fatal("sanitized final-response hash does not match manifest")
			}
			for _, required := range []string{"Changed files", "Remaining manual steps", "doctor", "on-demand"} {
				if !strings.Contains(response, required) {
					t.Errorf("final response missing %q", required)
				}
			}
		})
	}
}

func TestAgentInstallationEvaluationResultSetIsBounded(t *testing.T) {
	root := filepath.Join("..", "..", "testdata", "agent-install", "results")
	want := map[string]bool{
		"artifacts/install.sql": false, "artifacts/sb-heartbeat.yaml": false,
		"artifacts/sb-heartbeat.yml": false,
		"agent-a/manifest.json":      false, "agent-a/final-response.md": false,
		"agent-b/manifest.json": false, "agent-b/final-response.md": false,
	}
	var total int64
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if _, ok := want[rel]; !ok {
			t.Errorf("unexpected retained result %q", rel)
			return nil
		}
		want[rel] = true
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Size() > 128*1024 {
			t.Errorf("retained result %q is not a bounded regular file", rel)
		}
		total += info.Size()
		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if unsafeEvaluationMaterial.Match(contents) {
			t.Errorf("retained result %q contains credential-like material", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if total > 256*1024 {
		t.Fatalf("retained results size = %d, want at most 256 KiB", total)
	}
	for path, found := range want {
		if !found {
			t.Errorf("missing retained result %q", path)
		}
	}
}

func assertEvaluationArtifacts(t *testing.T, resultsRoot string, manifest evaluationManifest) {
	t.Helper()
	configPath := filepath.Join(resultsRoot, "artifacts", "sb-heartbeat.yaml")
	file, err := os.Open(configPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg, loadErr := config.Load(file)
	closeErr := file.Close()
	if loadErr != nil || closeErr != nil {
		t.Fatalf("load config = %v, close = %v", loadErr, closeErr)
	}
	project := cfg.Projects[0]
	if len(cfg.Projects) != 1 || project.Name != "evaluation-stage" ||
		project.URL.Env != "EVALUATION_SUPABASE_URL" || project.URL.GitHub != config.GitHubVariable ||
		project.APIKey.Env != "EVALUATION_SUPABASE_API_KEY" || project.APIKey.GitHub != config.GitHubSecret {
		t.Fatalf("evaluated config = %+v", cfg)
	}
	var migrationPath string
	for path := range manifest.Generated {
		if strings.HasPrefix(path, "supabase/migrations/") {
			migrationPath = path
		}
	}
	if migrationPath == "" || readEvaluationFile(t, snapshotPath(resultsRoot, migrationPath)) != migration.InstallSQL() {
		t.Fatal("retained migration does not exactly match generated install SQL")
	}
	workflow := readEvaluationFile(t, resultsRoot, "artifacts", "sb-heartbeat.yml")
	for _, required := range []string{
		"SB_HEARTBEAT_VERSION: v0.1.1", "sha256sum --check --strict",
		"EVALUATION_SUPABASE_URL: ${{ vars.EVALUATION_SUPABASE_URL }}",
		"EVALUATION_SUPABASE_API_KEY: ${{ secrets.EVALUATION_SUPABASE_API_KEY }}",
	} {
		if !strings.Contains(workflow, required) {
			t.Errorf("workflow missing %q", required)
		}
	}
}

func snapshotPath(resultsRoot, generatedPath string) string {
	switch {
	case generatedPath == "sb-heartbeat.yaml":
		return filepath.Join(resultsRoot, "artifacts", "sb-heartbeat.yaml")
	case generatedPath == ".github/workflows/sb-heartbeat.yml":
		return filepath.Join(resultsRoot, "artifacts", "sb-heartbeat.yml")
	case strings.HasPrefix(generatedPath, "supabase/migrations/") && strings.HasSuffix(generatedPath, ".sql"):
		return filepath.Join(resultsRoot, "artifacts", "install.sql")
	default:
		return filepath.Join(resultsRoot, "artifacts", "unexpected")
	}
}

func hashFiles(t *testing.T, root string, paths []string) string {
	t.Helper()
	sort.Strings(paths)
	hash := sha256.New()
	for _, path := range paths {
		hash.Write([]byte(path + "\n"))
		hash.Write([]byte(readEvaluationFile(t, root, filepath.FromSlash(path))))
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func sha256Hex(contents []byte) string {
	sum := sha256.Sum256(contents)
	return hex.EncodeToString(sum[:])
}

func decodeEvaluationJSON(t *testing.T, path string, target any) {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if err := json.Unmarshal(contents, target); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
}

func TestAgentInstallationEvaluationIsIndexedAndTrackedAsComplete(t *testing.T) {
	root := filepath.Join("..", "..")
	readme := readEvaluationFile(t, root, "README.md")
	if !strings.Contains(readme, "[Coding-agent installation evaluation](docs/agent-evaluation.md)") {
		t.Fatal("README does not index the evaluation")
	}
	spec := readEvaluationFile(t, root, "docs", "product-spec.md")
	if !strings.Contains(spec, "- [x] Agent installation tests with at least two coding agents.") {
		t.Fatal("product roadmap does not mark the two-agent evaluation complete")
	}
}

func readEvaluationFile(t *testing.T, root string, parts ...string) string {
	t.Helper()
	path := filepath.Join(append([]string{root}, parts...)...)
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(contents)
}
