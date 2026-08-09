package container_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	releaseText := string(release)
	if !strings.Contains(releaseText, "verify:\n    uses: ./.github/workflows/test.yml") ||
		!strings.Contains(releaseText, "needs:\n      - verify\n      - hosted-supabase") {
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
