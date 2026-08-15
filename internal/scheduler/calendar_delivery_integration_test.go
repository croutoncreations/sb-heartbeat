package scheduler_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLaunchdCalendarSmokeObservesScheduledDeliveryAndCleansUp(t *testing.T) {
	text := readRepositoryFile(t, "scripts", "integration-launchd-calendar.sh")
	for _, required := range []string{
		"set -euo pipefail",
		"uname -s",
		"mktemp -d",
		"trap cleanup EXIT",
		"install launchd",
		"launchctl bootstrap",
		"launchctl print",
		`grep -F -- "${probe_path}"`,
		"StartCalendarInterval",
		"calendar delivery",
		"launchctl bootout",
		"SB_HEARTBEAT_CALENDAR_TIMEOUT_SECONDS",
		"timeout_seconds < 240",
	} {
		if !strings.Contains(text, required) {
			t.Errorf("launchd calendar smoke harness missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"launchctl kickstart",
		"loaded=1\nlaunchctl bootstrap",
		"SB_HEARTBEAT_HOSTED_",
		"service_role",
	} {
		if strings.Contains(text, forbidden) {
			t.Errorf("launchd calendar smoke harness contains forbidden %q", forbidden)
		}
	}
}

func TestSystemdCalendarSmokeUsesDisposablePinnedContainerAndNoManualServiceStart(t *testing.T) {
	text := readRepositoryFile(t, "scripts", "integration-systemd-calendar.sh")
	for _, required := range []string{
		"set -euo pipefail",
		"mktemp -d",
		"trap cleanup EXIT",
		"@sha256:",
		"--privileged",
		"--iidfile",
		"--cidfile",
		`docker image inspect --format '{{.Id}}' "${image_name}"`,
		`docker image rm --force "${image_name}"`,
		`CMD ["/usr/lib/systemd/systemd"]`,
		"libpam-systemd",
		"install systemd",
		"user@2000.service",
		`grep -qx 'CapabilityBoundingSet='`,
		`sed -i '/^CapabilityBoundingSet=$/d'`,
		"XDG_RUNTIME_DIR=/run/user/2000",
		"systemctl --user",
		"enable --now",
		`chmod 755 "${integration_dir}/calendar-probe"`,
		"OnCalendar=",
		"calendar delivery",
		"SB_HEARTBEAT_CALENDAR_TIMEOUT_SECONDS",
		"timeout_seconds < 240",
	} {
		if !strings.Contains(text, required) {
			t.Errorf("systemd calendar smoke harness missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"systemctl start sb-heartbeat-calendar.service",
		"SB_HEARTBEAT_HOSTED_",
		"service_role",
	} {
		if strings.Contains(text, forbidden) {
			t.Errorf("systemd calendar smoke harness contains forbidden %q", forbidden)
		}
	}
}

func TestCalendarSmokeWorkflowRunsBothSchedulersAndGatesRelease(t *testing.T) {
	workflow := readRepositoryFile(t, ".github", "workflows", "calendar-smoke.yml")
	for _, required := range []string{
		"workflow_call:",
		"workflow_dispatch:",
		"macos-15",
		"ubuntu-24.04",
		"scripts/integration-launchd-calendar.sh",
		"scripts/integration-systemd-calendar.sh",
		"timeout-minutes:",
		"actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1",
		"actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e",
	} {
		if !strings.Contains(workflow, required) {
			t.Errorf("calendar smoke workflow missing %q", required)
		}
	}

	release := readRepositoryFile(t, ".github", "workflows", "release.yml")
	for _, required := range []string{
		"calendar-delivery:",
		"uses: ./.github/workflows/calendar-smoke.yml",
		"- calendar-delivery",
	} {
		if !strings.Contains(release, required) {
			t.Errorf("release workflow does not gate publication on calendar delivery: missing %q", required)
		}
	}
}

func readRepositoryFile(t *testing.T, elements ...string) string {
	t.Helper()
	path := filepath.Join(append([]string{"..", ".."}, elements...)...)
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(contents)
}
