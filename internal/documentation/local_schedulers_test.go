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
		"never loads", "never enables", "local timezone", "day-of-month", "weekday",
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
		"Strict local environment files", "macOS `launchd` generator", "[x] macOS `launchd` generator",
	} {
		if !strings.Contains(text, required) {
			t.Errorf("product specification missing %q", required)
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
}
