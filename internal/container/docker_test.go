package container_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestDockerfileIsPinnedNonRootAndReadOnlyCompatible(t *testing.T) {
	root := filepath.Join("..", "..")
	contents, err := os.ReadFile(filepath.Join(root, "Dockerfile"))
	if err != nil {
		t.Fatalf("read Dockerfile: %v", err)
	}
	text := string(contents)
	required := []string{
		"# syntax=docker/dockerfile:1.7@sha256:a57df69d0ea827fb7266491f2813635de6f17269be881f696fbfdf2d83dda33e",
		"golang:1.26.5-alpine3.23@sha256:622e56dbc11a8cfe87cafa2331e9a201877271cbff918af53d3be315f3da88cc",
		"ARG TARGETOS",
		"ARG TARGETARCH",
		"CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH}",
		"-trimpath",
		"FROM scratch",
		"COPY --from=build /etc/ssl/certs/ca-certificates.crt",
		"COPY --from=build --chown=65532:65532 /out/sb-heartbeat /usr/local/bin/sb-heartbeat",
		"USER 65532:65532",
		`ENTRYPOINT ["/usr/local/bin/sb-heartbeat"]`,
	}
	for _, fragment := range required {
		if !strings.Contains(text, fragment) {
			t.Errorf("Dockerfile missing %q", fragment)
		}
	}
	for _, forbidden := range []string{"latest", "apk add", "curl ", "ADD ", "COPY . .", "VOLUME", "HEALTHCHECK"} {
		if strings.Contains(text, forbidden) {
			t.Errorf("Dockerfile contains unsafe or misleading instruction %q", forbidden)
		}
	}
}

func TestDockerBuildContextExcludesDevelopmentAndCredentialMaterial(t *testing.T) {
	contents, err := os.ReadFile(filepath.Join("..", "..", ".dockerignore"))
	if err != nil {
		t.Fatalf("read .dockerignore: %v", err)
	}
	text := string(contents)
	for _, pattern := range []string{"*", "!Dockerfile", "!.dockerignore", "!go.mod", "!go.sum", "!cmd/", "!cmd/**", "!internal/", "!internal/**"} {
		if !strings.Contains(text, pattern) {
			t.Errorf(".dockerignore missing %q", pattern)
		}
	}
	if strings.Contains(text, ".env") || strings.Contains(text, "release-fixture") {
		t.Fatal(".dockerignore must use an allowlist rather than credential-path denylisting")
	}
}

func TestDockerIntegrationCoversRuntimeAndArchitectures(t *testing.T) {
	root := filepath.Join("..", "..")
	script, err := os.ReadFile(filepath.Join(root, "scripts", "integration-docker.sh"))
	if err != nil {
		t.Fatalf("read integration script: %v", err)
	}
	text := string(script)
	for _, fragment := range []string{
		"set -euo pipefail",
		"docker build",
		"--read-only",
		"65532:65532",
		"linux/amd64,linux/arm64",
		"docker-container",
		"moby/buildkit:v0.23.1@sha256:dbc2dfd9342fd5c891ea94e9774c15cab985681e5ff995a9e366066aa0b9b2b4",
		"type=oci",
		"migration install",
		"inspect-docker-oci.py",
		"builder_created",
		"image_created",
	} {
		if !strings.Contains(text, fragment) {
			t.Errorf("Docker integration script missing %q", fragment)
		}
	}
	docs, err := os.ReadFile(filepath.Join(root, "docs", "docker.md"))
	if err != nil {
		t.Fatalf("read Docker docs: %v", err)
	}
	for _, fragment := range []string{"--read-only", ":ro", "publishable or legacy anon key", "outside the repository", "linux/amd64", "linux/arm64"} {
		if !strings.Contains(string(docs), fragment) {
			t.Errorf("Docker docs missing %q", fragment)
		}
	}
}

func TestDockerIntegrationGatesPullRequestsAndReleases(t *testing.T) {
	workflow, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "test.yml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(workflow)
	for _, fragment := range []string{
		"docker-integration:",
		"runs-on: ubuntu-latest",
		"scripts/integration-docker.sh",
	} {
		if !strings.Contains(text, fragment) {
			t.Errorf("test workflow missing %q", fragment)
		}
	}
	release, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "release.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var parsedRelease struct {
		Jobs map[string]struct {
			Uses  string `yaml:"uses"`
			Needs any    `yaml:"needs"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(release, &parsedRelease); err != nil {
		t.Fatalf("parse release workflow: %v", err)
	}
	verify, verifyExists := parsedRelease.Jobs["verify"]
	publish, publishExists := parsedRelease.Jobs["release"]
	if !verifyExists || verify.Uses != "./.github/workflows/test.yml" || !publishExists ||
		!workflowNeeds(publish.Needs, "verify") || !workflowNeeds(publish.Needs, "hosted-supabase") {
		t.Fatal("release workflow does not depend on the reusable Docker-gated test workflow")
	}
	inspector, err := os.ReadFile(filepath.Join("..", "..", "scripts", "inspect-docker-oci.py"))
	if err != nil {
		t.Fatalf("read OCI inspector: %v", err)
	}
	for _, fragment := range []string{"linux/amd64", "linux/arm64", "65532:65532", "PT_INTERP", "e_machine", "org.opencontainers.image.version", "attestation-manifest", "runnable_platforms"} {
		if !strings.Contains(string(inspector), fragment) {
			t.Errorf("OCI inspector missing %q", fragment)
		}
	}
}

func workflowNeeds(value any, expected string) bool {
	switch needs := value.(type) {
	case string:
		return needs == expected
	case []any:
		for _, need := range needs {
			if need == expected {
				return true
			}
		}
	}
	return false
}

func TestReleasePublishesAttestedMultiArchitectureGHCRImage(t *testing.T) {
	release, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "release.yml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(release)
	required := []string{
		"group: release-${{ github.ref }}",
		"cancel-in-progress: false",
		"publish-container:",
		"needs: release",
		"packages: write",
		"attestations: write",
		"artifact-metadata: write",
		"id-token: write",
		"docker/login-action@dbcb813823bdd20940b903addbd779551569679f # v4.6.0",
		"docker/setup-buildx-action@bb05f3f5519dd87d3ba754cc423b652a5edd6d2c # v4.2.0",
		"image=moby/buildkit:v0.23.1@sha256:dbc2dfd9342fd5c891ea94e9774c15cab985681e5ff995a9e366066aa0b9b2b4",
		"docker/build-push-action@53b7df96c91f9c12dcc8a07bcb9ccacbed38856a # v7.3.0",
		"actions/attest@1e69f48acb82d1966a394da916b4c1698aa569d6 # v4.2.2",
		"platforms: linux/amd64,linux/arm64",
		"outputs: type=image,name=${{ env.IMAGE_NAME }},push-by-digest=true,name-canonical=true,push=true",
		"provenance: mode=max",
		"sbom: true",
		"subject-digest: ${{ steps.push.outputs.digest }}",
		"push-to-registry: true",
		"docker buildx imagetools create --tag",
	}
	for _, fragment := range required {
		if !strings.Contains(text, fragment) {
			t.Errorf("release workflow missing %q", fragment)
		}
	}
	if count := strings.Count(text, "docker buildx imagetools inspect"); count != 2 {
		t.Errorf("release workflow has %d version-tag collision checks, want 2", count)
	}
	if count := strings.Count(text, `grep -Eq '(^|: )(manifest unknown|name unknown|not found)(:|$)'`); count != 2 {
		t.Errorf("release workflow has %d affirmative not-found checks, want 2", count)
	}
	if strings.Contains(text, `imagetools inspect "${IMAGE_NAME}:${RELEASE_TAG}" >/dev/null 2>&1`) {
		t.Fatal("release workflow treats indeterminate registry errors as tag absence")
	}
	for _, forbidden := range []string{"ghcr.io/croutoncreations/sb-heartbeat:latest", "GHCR_PAT", "PERSONAL_ACCESS_TOKEN"} {
		if strings.Contains(text, forbidden) {
			t.Errorf("release workflow contains %q", forbidden)
		}
	}
}

func TestAttestationFailurePreventsVersionTagPromotion(t *testing.T) {
	contents, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "release.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var workflow struct {
		Concurrency struct {
			Group            string `yaml:"group"`
			CancelInProgress bool   `yaml:"cancel-in-progress"`
		} `yaml:"concurrency"`
		Jobs map[string]struct {
			Needs       any               `yaml:"needs"`
			Permissions map[string]string `yaml:"permissions"`
			Steps       []struct {
				Name string         `yaml:"name"`
				ID   string         `yaml:"id"`
				Uses string         `yaml:"uses"`
				If   string         `yaml:"if"`
				Run  string         `yaml:"run"`
				With map[string]any `yaml:"with"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(contents, &workflow); err != nil {
		t.Fatalf("parse release workflow: %v", err)
	}
	if workflow.Concurrency.Group != "release-${{ github.ref }}" || workflow.Concurrency.CancelInProgress {
		t.Fatalf("unsafe release concurrency: %+v", workflow.Concurrency)
	}
	job, ok := workflow.Jobs["publish-container"]
	if !ok || job.Needs != "release" {
		t.Fatalf("container job dependency = %#v, exists = %v", job.Needs, ok)
	}
	wantPermissions := map[string]string{"contents": "read", "packages": "write", "attestations": "write", "artifact-metadata": "write", "id-token": "write"}
	if len(job.Permissions) != len(wantPermissions) {
		t.Fatalf("container permissions = %#v", job.Permissions)
	}
	for name, want := range wantPermissions {
		if job.Permissions[name] != want {
			t.Errorf("permission %s = %q, want %q", name, job.Permissions[name], want)
		}
	}
	buildIndex, attestIndex, promoteIndex := -1, -1, -1
	for index, step := range job.Steps {
		switch step.Name {
		case "Build and push release digest":
			buildIndex = index
			if _, exists := step.With["tags"]; exists {
				t.Fatal("digest build must not publish a tag")
			}
			if _, exists := step.With["push"]; exists {
				t.Fatal("digest build must use explicit push-by-digest output")
			}
			if step.With["outputs"] != "type=image,name=${{ env.IMAGE_NAME }},push-by-digest=true,name-canonical=true,push=true" {
				t.Fatalf("digest output = %#v", step.With["outputs"])
			}
		case "Attest release digest":
			attestIndex = index
		case "Promote attested digest to version tag":
			promoteIndex = index
			if step.If != "success()" {
				t.Fatalf("promotion condition = %q", step.If)
			}
			if !strings.Contains(step.Run, `docker buildx imagetools create --tag "${IMAGE_NAME}:${RELEASE_TAG}" "${IMAGE_NAME}@${IMAGE_DIGEST}"`) {
				t.Fatalf("unsafe promotion command:\n%s", step.Run)
			}
		}
	}
	if buildIndex < 0 || attestIndex <= buildIndex || promoteIndex <= attestIndex {
		t.Fatalf("unsafe publish order: build=%d attest=%d promote=%d", buildIndex, attestIndex, promoteIndex)
	}
}

func TestDockerDocumentationCoversPinnedGHCRConsumptionAndVerification(t *testing.T) {
	docs, err := os.ReadFile(filepath.Join("..", "..", "docs", "docker.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(docs)
	for _, fragment := range []string{
		"ghcr.io/croutoncreations/sb-heartbeat:v0.2.0",
		"@sha256:",
		"linux/amd64",
		"linux/arm64",
		"SBOM",
		"gh attestation verify oci://ghcr.io/croutoncreations/sb-heartbeat:v0.2.0 -R croutoncreations/sb-heartbeat",
		"package visibility",
	} {
		if !strings.Contains(text, fragment) {
			t.Errorf("Docker docs missing %q", fragment)
		}
	}
	spec, err := os.ReadFile(filepath.Join("..", "..", "docs", "product-spec.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(spec), "- [x] GHCR multi-architecture image with SBOM and build provenance.") ||
		!strings.Contains(string(spec), "- [ ] Homebrew tap") {
		t.Fatal("product roadmap does not independently track GHCR completion and Homebrew")
	}
}
