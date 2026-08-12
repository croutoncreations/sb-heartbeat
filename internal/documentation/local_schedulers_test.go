package documentation_test

import (
	"os"
	"strings"
	"testing"
)

func TestLocalSchedulerDocumentationCoversSecureGeneratedOperation(t *testing.T) {
	contents, err := os.ReadFile("../../docs/local-schedulers.md")
	if err != nil {
		t.Fatal(err)
	}
	text := string(contents)
	for _, required := range []string{
		"--env-file", "chmod 600", "NAME=value", "not shell syntax",
		"install launchd", "LaunchAgents", "launchctl bootstrap", "launchctl bootout", "launchctl kickstart",
		"install systemd", "systemd/user", "systemctl --user daemon-reload", "systemctl --user enable --now", "systemctl --user disable --now",
		"systemd 244", "unprivileged user namespaces",
		"never loads", "never enables", "local timezone", "day-of-month", "weekday",
		"integration-launchd-calendar.sh", "integration-systemd-calendar.sh", "calendar delivery",
		"--privileged", "host cgroup namespace", "240 through 600",
	} {
		if !strings.Contains(text, required) {
			t.Errorf("local scheduler documentation missing %q", required)
		}
	}
	for _, forbidden := range []string{"sb_publishable_", "service_role", "launchctl setenv"} {
		if strings.Contains(text, forbidden) {
			t.Errorf("local scheduler documentation contains unsafe example %q", forbidden)
		}
	}
}

func TestProductRoadmapTracksLaunchdAndStrictEnvironmentFiles(t *testing.T) {
	contents, err := os.ReadFile("../../docs/product-spec.md")
	if err != nil {
		t.Fatal(err)
	}
	text := string(contents)
	for _, required := range []string{
		"Strict local environment files", "macOS `launchd` generator", "[x] macOS `launchd` generator", "[x] `systemd` timer generator",
		"[x] Cloudflare Worker generator and Cron Triggers", "generated executable contract tests",
		"[x] Automate release smoke tests that observe actual `launchd` and `systemd`",
	} {
		if !strings.Contains(text, required) {
			t.Errorf("product specification missing %q", required)
		}
	}
}

func TestCloudflareDocumentationCoversSafeLocalValidationAndDeployment(t *testing.T) {
	contents, err := os.ReadFile("../../docs/cloudflare.md")
	if err != nil {
		t.Fatal(err)
	}
	text := string(contents)
	for _, required := range []string{
		"install cloudflare", "UTC", "publishable", "legacy anon", "secret/service-role",
		"workers_dev", "preview_urls", "/cdn-cgi/handler/scheduled?format=json",
		"wrangler deploy --secrets-file", "npm test", "npm run check", "64 KiB",
		"free plan", "25", "never deploys", "never stores credential values",
	} {
		if !strings.Contains(text, required) {
			t.Errorf("Cloudflare documentation missing %q", required)
		}
	}
	for _, forbidden := range []string{"sb_publishable_", "https://example.supabase.co"} {
		if strings.Contains(text, forbidden) {
			t.Errorf("Cloudflare documentation contains credential-like example %q", forbidden)
		}
	}
}

func TestReadmeLinksLocalSchedulerGuide(t *testing.T) {
	contents, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(contents), "[Local scheduler generators](docs/local-schedulers.md)") {
		t.Fatal("README does not link the local scheduler guide")
	}
	if !strings.Contains(string(contents), "[Cloudflare Worker generator](docs/cloudflare.md)") {
		t.Fatal("README does not link the Cloudflare Worker guide")
	}
}
