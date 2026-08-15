package release_test

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const releaseVersion = "0.2.0"

func TestReleaseWorkflowAttestsDraftBeforePublishingImmutableRelease(t *testing.T) {
	root := filepath.Join("..", "..")
	contents := readFile(t, filepath.Join(root, ".github", "workflows", "release.yml"))
	var workflow struct {
		Jobs map[string]struct {
			Permissions map[string]string `yaml:"permissions"`
			Steps       []struct {
				Name string         `yaml:"name"`
				Uses string         `yaml:"uses"`
				Run  string         `yaml:"run"`
				With map[string]any `yaml:"with"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal([]byte(contents), &workflow); err != nil {
		t.Fatalf("parse release workflow: %v", err)
	}
	job := workflow.Jobs["release"]
	wantPermissions := map[string]string{
		"contents": "write", "id-token": "write", "attestations": "write", "artifact-metadata": "write",
	}
	if len(job.Permissions) != len(wantPermissions) {
		t.Fatalf("release permissions = %#v", job.Permissions)
	}
	for name, want := range wantPermissions {
		if job.Permissions[name] != want {
			t.Errorf("permission %s = %q, want %q", name, job.Permissions[name], want)
		}
	}

	positions := map[string]int{}
	for index, step := range job.Steps {
		positions[step.Name] = index
		switch step.Name {
		case "Attest release archives":
			if step.Uses != "actions/attest@1e69f48acb82d1966a394da916b4c1698aa569d6" {
				t.Errorf("attestation action = %q", step.Uses)
			}
			if step.With["subject-checksums"] != "dist/checksums.txt" {
				t.Errorf("archive attestation is not bound to the verified manifest: %#v", step.With)
			}
			if _, exists := step.With["subject-path"]; exists {
				t.Errorf("archive attestation uses a broader path input: %#v", step.With)
			}
		case "Attest release checksum manifest":
			if step.Uses != "actions/attest@1e69f48acb82d1966a394da916b4c1698aa569d6" {
				t.Errorf("checksum attestation action = %q", step.Uses)
			}
			if step.With["subject-path"] != "dist/checksums.txt" {
				t.Errorf("checksum attestation does not identify the exact manifest: %#v", step.With)
			}
		case "Verify draft release assets":
			for _, required := range []string{"gh release download", "cmp", `scripts/verify-release-artifacts.sh "${remote_assets}" "${RELEASE_TAG#v}" --exact`} {
				if !strings.Contains(step.Run, required) {
					t.Errorf("remote verification missing %q", required)
				}
			}
		case "Publish attested immutable release":
			for _, required := range []string{
				"git ls-remote", `"${remote_tag_sha}" != "${GITHUB_SHA}"`,
				`is_draft="$(gh release view "${RELEASE_TAG}" --json isDraft --jq .isDraft)"`,
				`"${is_draft}" != true`,
				`immutable="$(gh api "repos/${GITHUB_REPOSITORY}/releases/tags/${RELEASE_TAG}" --jq '.immutable // false')"`,
				`"${immutable}" != true`,
			} {
				if !strings.Contains(step.Run, required) {
					t.Errorf("final publication guard missing %q", required)
				}
			}
			if !strings.Contains(step.Run, `gh release edit "${RELEASE_TAG}" --draft=false --verify-tag`) {
				t.Errorf("unsafe publish command:\n%s", step.Run)
			}
		}
	}
	ordered := []string{
		"Validate release tag",
		"Build draft release",
		"Verify local release artifacts",
		"Require complete draft release",
		"Attest release archives",
		"Attest release checksum manifest",
		"Verify draft release assets",
		"Publish attested immutable release",
	}
	for index, name := range ordered {
		position, exists := positions[name]
		if !exists {
			t.Errorf("release step %q is missing", name)
			continue
		}
		if index > 0 && position <= positions[ordered[index-1]] {
			t.Errorf("release step %q is out of order", name)
		}
	}
	if strings.Index(contents, "--draft=false") < strings.Index(contents, "actions/attest@") {
		t.Fatal("release can be published before provenance is created")
	}
	if strings.Contains(contents, `repos/${GITHUB_REPOSITORY}/immutable-releases`) {
		t.Fatal("workflow GITHUB_TOKEN cannot call the repository administration endpoint")
	}
	if !strings.Contains(contents, "actions/attest@1e69f48acb82d1966a394da916b4c1698aa569d6 # v4.2.2") {
		t.Fatal("attestation action lacks an exact reviewed version comment")
	}
}

func TestGoReleaserStagesDraftForFailClosedAttestation(t *testing.T) {
	root := filepath.Join("..", "..")
	contents := readFile(t, filepath.Join(root, ".goreleaser.yml"))
	var configuration struct {
		Release struct {
			Draft                    bool `yaml:"draft"`
			UseExistingDraft         bool `yaml:"use_existing_draft"`
			ReplaceExistingArtifacts bool `yaml:"replace_existing_artifacts"`
		} `yaml:"release"`
	}
	if err := yaml.Unmarshal([]byte(contents), &configuration); err != nil {
		t.Fatal(err)
	}
	if !configuration.Release.Draft || !configuration.Release.UseExistingDraft || !configuration.Release.ReplaceExistingArtifacts {
		t.Fatalf("release is not safely resumable as a draft: %+v", configuration.Release)
	}
}

func TestReleaseArtifactVerifierAcceptsExactSet(t *testing.T) {
	dir := makeReleaseArtifacts(t)
	runVerifier(t, dir, true)
}

func TestReleaseArtifactVerifierRejectsUnsafeOrIncompleteSets(t *testing.T) {
	tests := map[string]func(*testing.T, string){
		"missing archive": func(t *testing.T, dir string) {
			t.Helper()
			if err := os.Remove(filepath.Join(dir, expectedArchives()[0])); err != nil {
				t.Fatal(err)
			}
		},
		"bad checksum": func(t *testing.T, dir string) {
			t.Helper()
			path := filepath.Join(dir, expectedArchives()[0])
			if err := os.WriteFile(path, []byte("changed"), 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"unexpected archive": func(t *testing.T, dir string) {
			t.Helper()
			if err := os.WriteFile(filepath.Join(dir, "sb-heartbeat_0.2.0_linux_386.tar.gz"), []byte("unexpected"), 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"unexpected release asset": func(t *testing.T, dir string) {
			t.Helper()
			if err := os.WriteFile(filepath.Join(dir, "install.sh"), []byte("unexpected"), 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"path traversal checksum": func(t *testing.T, dir string) {
			t.Helper()
			file, err := os.OpenFile(filepath.Join(dir, "checksums.txt"), os.O_APPEND|os.O_WRONLY, 0)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := fmt.Fprintf(file, "%064x  ../outside\n", 1); err != nil {
				t.Fatal(err)
			}
			if err := file.Close(); err != nil {
				t.Fatal(err)
			}
		},
		"symlink archive": func(t *testing.T, dir string) {
			t.Helper()
			path := filepath.Join(dir, expectedArchives()[0])
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(filepath.Join(dir, expectedArchives()[1]), path); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			dir := makeReleaseArtifacts(t)
			mutate(t, dir)
			runVerifier(t, dir, false)
		})
	}
}

func TestReleaseDocumentationDefinesVerificationAndOneTimeSetup(t *testing.T) {
	root := filepath.Join("..", "..")
	docs := readFile(t, filepath.Join(root, "docs", "releasing.md"))
	normalized := strings.Join(strings.Fields(docs), " ")
	for _, required := range []string{
		"Enable release immutability",
		"draft",
		"gh release verify v0.3.1",
		"gh release verify-asset v0.3.1",
		"gh attestation verify",
		"no release is published if attestation fails",
	} {
		if !strings.Contains(normalized, required) {
			t.Errorf("release docs missing %q", required)
		}
	}
	spec := readFile(t, filepath.Join(root, "docs", "product-spec.md"))
	if !strings.Contains(spec, "- [x] Signed artifacts and build provenance.") {
		t.Fatal("signed artifact roadmap item is not complete")
	}
}

func TestReusableTestWorkflowScansReachableGoVulnerabilities(t *testing.T) {
	root := filepath.Join("..", "..")
	workflow := readFile(t, filepath.Join(root, ".github", "workflows", "test.yml"))
	if !strings.Contains(workflow, "go run golang.org/x/vuln/cmd/govulncheck@v1.7.0 ./...") {
		t.Fatal("reusable test workflow must run the reviewed govulncheck version")
	}
}

func makeReleaseArtifacts(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	var checksums strings.Builder
	for index, name := range expectedArchives() {
		contents := []byte(fmt.Sprintf("archive-%d", index))
		if err := os.WriteFile(filepath.Join(dir, name), contents, 0o600); err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(contents)
		fmt.Fprintf(&checksums, "%x  %s\n", digest, name)
	}
	if err := os.WriteFile(filepath.Join(dir, "checksums.txt"), []byte(checksums.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func expectedArchives() []string {
	var result []string
	for _, platform := range []string{"darwin_amd64", "darwin_arm64", "linux_amd64", "linux_arm64"} {
		result = append(result, "sb-heartbeat_"+releaseVersion+"_"+platform+".tar.gz")
	}
	for _, platform := range []string{"windows_amd64", "windows_arm64"} {
		result = append(result, "sb-heartbeat_"+releaseVersion+"_"+platform+".zip")
	}
	return result
}

func runVerifier(t *testing.T, dir string, wantSuccess bool) {
	t.Helper()
	root := filepath.Join("..", "..")
	command := exec.Command("bash", filepath.Join(root, "scripts", "verify-release-artifacts.sh"), dir, releaseVersion, "--exact")
	output, err := command.CombinedOutput()
	if wantSuccess && err != nil {
		t.Fatalf("verifier failed: %v\n%s", err, output)
	}
	if !wantSuccess && err == nil {
		t.Fatalf("verifier accepted unsafe artifacts:\n%s", output)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(contents)
}
