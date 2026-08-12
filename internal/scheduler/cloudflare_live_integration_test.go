package scheduler_test

import (
	"encoding/base64"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestCloudflareLiveHarnessRequiresBothKeyFormsAndOwnsCleanup(t *testing.T) {
	text := readRepositoryFile(t, "scripts", "integration-cloudflare-live.sh")
	for _, required := range []string{
		"set -euo pipefail",
		`mode="${1:-}"`,
		`prepare)`,
		`live)`,
		"npm ci --ignore-scripts --no-audit --no-fund",
		"cloudflare-live-package/package-lock.json",
		"SB_HEARTBEAT_CLOUDFLARE_ACCOUNT_ID",
		"SB_HEARTBEAT_CLOUDFLARE_API_TOKEN",
		"SB_HEARTBEAT_CLOUDFLARE_PUBLISHABLE_URL",
		"SB_HEARTBEAT_CLOUDFLARE_PUBLISHABLE_KEY",
		"SB_HEARTBEAT_CLOUDFLARE_ANON_URL",
		"SB_HEARTBEAT_CLOUDFLARE_ANON_KEY",
		"sb_publishable_",
		"install cloudflare",
		"npm test -- --run",
		"npm run check",
		"wrangler deploy --strict",
		"--secrets-file",
		"wrangler tail",
		"actual deployed Cron Trigger",
		"publishable-fixture",
		"anon-fixture",
		"/subdomain",
		"previews_enabled",
		"/schedules",
		"/settings",
		`type == "secret_text"`,
		"distinct hosted Supabase projects",
		"ownership marker",
		"trap cleanup EXIT",
		"wrangler delete",
	} {
		if !strings.Contains(text, required) {
			t.Errorf("Cloudflare live harness missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"npm install",
		"wrangler dev --remote",
		"/__scheduled",
		"service_role",
		"echo ${SB_HEARTBEAT_CLOUDFLARE_",
		"set -x",
	} {
		if strings.Contains(text, forbidden) {
			t.Errorf("Cloudflare live harness contains forbidden %q", forbidden)
		}
	}
}

func TestCloudflareLiveWorkflowFailsClosedAndGatesRelease(t *testing.T) {
	workflow := readRepositoryFile(t, ".github", "workflows", "cloudflare-live.yml")
	for _, required := range []string{
		"workflow_call:",
		"workflow_dispatch:",
		"environment: cloudflare-live-release",
		`integration-cloudflare-live.sh prepare`,
		`integration-cloudflare-live.sh live`,
		`if: startsWith(github.ref, 'refs/tags/v') || (github.event_name == 'workflow_dispatch' && github.ref == 'refs/heads/main')`,
		"SB_HEARTBEAT_CLOUDFLARE_ACCOUNT_ID: ${{ secrets.SB_HEARTBEAT_CLOUDFLARE_ACCOUNT_ID }}",
		"SB_HEARTBEAT_CLOUDFLARE_API_TOKEN: ${{ secrets.SB_HEARTBEAT_CLOUDFLARE_API_TOKEN }}",
		"SB_HEARTBEAT_CLOUDFLARE_PUBLISHABLE_KEY: ${{ secrets.SB_HEARTBEAT_CLOUDFLARE_PUBLISHABLE_KEY }}",
		"SB_HEARTBEAT_CLOUDFLARE_ANON_KEY: ${{ secrets.SB_HEARTBEAT_CLOUDFLARE_ANON_KEY }}",
		"timeout-minutes:",
	} {
		if !strings.Contains(workflow, required) {
			t.Errorf("Cloudflare live workflow missing %q", required)
		}
	}
	prepareIndex := strings.Index(workflow, "integration-cloudflare-live.sh prepare")
	secretIndex := strings.Index(workflow, "SB_HEARTBEAT_CLOUDFLARE_API_TOKEN: ${{ secrets.")
	liveIndex := strings.Index(workflow, "integration-cloudflare-live.sh live")
	if prepareIndex < 0 || secretIndex < 0 || liveIndex < 0 || !(prepareIndex < liveIndex && liveIndex < secretIndex) {
		t.Error("Cloudflare dependencies and contract tests must run before credentials enter the step environment")
	}

	release := readRepositoryFile(t, ".github", "workflows", "release.yml")
	for _, required := range []string{
		"cloudflare-live:",
		"uses: ./.github/workflows/cloudflare-live.yml",
		"- cloudflare-live",
	} {
		if !strings.Contains(release, required) {
			t.Errorf("release workflow does not gate publication on live Cloudflare coverage: missing %q", required)
		}
	}
}

func TestCloudflareLiveDependencyGraphIsCheckedIn(t *testing.T) {
	manifest := readRepositoryFile(t, "internal", "scheduler", "testdata", "cloudflare-live-package", "package.json")
	lock := readRepositoryFile(t, "internal", "scheduler", "testdata", "cloudflare-live-package", "package-lock.json")
	for _, version := range []string{`"wrangler": "4.120.1"`, `"vitest": "4.1.10"`} {
		if !strings.Contains(manifest, version) {
			t.Errorf("Cloudflare live dependency manifest missing exact version %s", version)
		}
	}
	if !strings.Contains(lock, `"lockfileVersion": 3`) || !strings.Contains(lock, `"integrity": "sha512-`) {
		t.Error("Cloudflare live dependency lock must be a checked npm lock with integrity hashes")
	}
}

func TestCloudflareLiveReleaseEnvironmentIsDocumented(t *testing.T) {
	docs := readRepositoryFile(t, "docs", "releasing.md")
	for _, required := range []string{
		"cloudflare-live-release",
		"SB_HEARTBEAT_CLOUDFLARE_ACCOUNT_ID",
		"SB_HEARTBEAT_CLOUDFLARE_API_TOKEN",
		"SB_HEARTBEAT_CLOUDFLARE_PUBLISHABLE_URL",
		"SB_HEARTBEAT_CLOUDFLARE_PUBLISHABLE_KEY",
		"SB_HEARTBEAT_CLOUDFLARE_ANON_URL",
		"SB_HEARTBEAT_CLOUDFLARE_ANON_KEY",
		"Workers Scripts: Edit",
		"Workers Tail: Read",
		"two distinct dedicated Supabase projects",
		"required reviewers",
		"protected branches and tags",
	} {
		if !strings.Contains(docs, required) {
			t.Errorf("release guide missing Cloudflare live setup %q", required)
		}
	}
}

func TestCloudflareLiveRejectsNonAnonFixtureBeforeDeployment(t *testing.T) {
	tests := []struct {
		name string
		key  string
	}{
		{name: "malformed", key: "not-a-jwt"},
		{name: "publishable", key: "sb_publishable_not_legacy_anon"},
		{name: "service role", key: liveFixtureJWT("service_role")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			command := exec.Command("bash", "../../scripts/integration-cloudflare-live.sh", "live", t.TempDir())
			command.Env = append(os.Environ(),
				"SB_HEARTBEAT_CLOUDFLARE_ACCOUNT_ID=dummy",
				"SB_HEARTBEAT_CLOUDFLARE_API_TOKEN=dummy",
				"SB_HEARTBEAT_CLOUDFLARE_PUBLISHABLE_URL=https://publishable-fixture.supabase.co",
				"SB_HEARTBEAT_CLOUDFLARE_PUBLISHABLE_KEY=sb_publishable_dummy",
				"SB_HEARTBEAT_CLOUDFLARE_ANON_URL=https://anon-fixture.supabase.co",
				"SB_HEARTBEAT_CLOUDFLARE_ANON_KEY="+tt.key,
			)
			output, err := command.CombinedOutput()
			var exitError *exec.ExitError
			if !errors.As(err, &exitError) || exitError.ExitCode() != 2 {
				t.Fatalf("exit = %v, output = %s; want exit 2", err, output)
			}
			if !strings.Contains(string(output), "actual legacy anon-key fixture") {
				t.Fatalf("output = %q; want stable anon-key diagnostic", output)
			}
			if strings.Contains(string(output), tt.key) {
				t.Fatal("diagnostic leaked rejected credential")
			}
		})
	}
}

func liveFixtureJWT(role string) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"role":"` + role + `"}`))
	return header + "." + payload + ".signature"
}
