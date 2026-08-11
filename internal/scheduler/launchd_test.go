package scheduler_test

import (
	"strings"
	"testing"

	"github.com/croutoncreations/sb-heartbeat/internal/scheduler"
)

func TestLaunchdGeneratesAUserAgentWithoutCredentialsOrShell(t *testing.T) {
	plist, err := scheduler.Launchd(workflowConfig(t), scheduler.LaunchdOptions{
		Label:       "io.github.croutoncreations.sb-heartbeat",
		BinaryPath:  "/Applications/SB Heartbeat/bin/sb-heartbeat",
		ConfigPath:  "/Users/example/Tone & Clone/sb-heartbeat.yaml",
		EnvFilePath: "/Users/example/.config/sb-heartbeat/heartbeat.env",
		StdoutPath:  "/Users/example/Library/Logs/sb-heartbeat.log",
		StderrPath:  "/Users/example/Library/Logs/sb-heartbeat.error.log",
	})
	if err != nil {
		t.Fatal(err)
	}
	text := string(plist)
	for _, required := range []string{
		`<string>io.github.croutoncreations.sb-heartbeat</string>`,
		`<string>/Applications/SB Heartbeat/bin/sb-heartbeat</string>`,
		`<string>/Users/example/Tone &amp; Clone/sb-heartbeat.yaml</string>`,
		`<string>--env-file</string>`,
		`<string>/Users/example/.config/sb-heartbeat/heartbeat.env</string>`,
		`<key>Minute</key>`, `<integer>37</integer>`,
		`<key>Hour</key>`, `<integer>3</integer>`, `<integer>11</integer>`, `<integer>19</integer>`,
		`<key>StandardOutPath</key>`, `<key>StandardErrorPath</key>`,
		`<key>ProcessType</key>`, `<string>Background</string>`,
	} {
		if !strings.Contains(text, required) {
			t.Errorf("plist missing %q:\n%s", required, text)
		}
	}
	for _, forbidden := range []string{"sb_publishable_", "<key>EnvironmentVariables</key>", "/bin/sh", "RunAtLoad"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("plist contains unsafe or unexpected %q:\n%s", forbidden, text)
		}
	}
	if got := strings.Count(text, "<key>Hour</key>"); got != 3 {
		t.Fatalf("Hour dictionaries = %d, want 3", got)
	}
}

func TestLaunchdRejectsUnsafeOptionsAndUnboundedExpansion(t *testing.T) {
	base := scheduler.LaunchdOptions{
		Label:       "io.github.croutoncreations.sb-heartbeat",
		BinaryPath:  "/usr/local/bin/sb-heartbeat",
		ConfigPath:  "/Users/example/sb-heartbeat.yaml",
		EnvFilePath: "/Users/example/.config/sb-heartbeat/heartbeat.env",
	}
	tests := map[string]func(*scheduler.LaunchdOptions){
		"invalid label":   func(o *scheduler.LaunchdOptions) { o.Label = "bad label" },
		"relative binary": func(o *scheduler.LaunchdOptions) { o.BinaryPath = "sb-heartbeat" },
		"relative config": func(o *scheduler.LaunchdOptions) { o.ConfigPath = "sb-heartbeat.yaml" },
		"relative env":    func(o *scheduler.LaunchdOptions) { o.EnvFilePath = "heartbeat.env" },
		"line break":      func(o *scheduler.LaunchdOptions) { o.StdoutPath = "/tmp/good\n<key>bad</key>" },
		"xml control":     func(o *scheduler.LaunchdOptions) { o.ConfigPath = "/tmp/bad\x01path" },
		"invalid utf8": func(o *scheduler.LaunchdOptions) {
			o.ConfigPath = "/tmp/" + string([]byte{0xff, 0xfe})
		},
		"config env collision": func(o *scheduler.LaunchdOptions) {
			o.EnvFilePath = o.ConfigPath
		},
		"stdout env collision": func(o *scheduler.LaunchdOptions) {
			o.StdoutPath = o.EnvFilePath
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			options := base
			mutate(&options)
			if _, err := scheduler.Launchd(workflowConfig(t), options); err == nil {
				t.Fatal("Launchd() error = nil")
			}
		})
	}

	cfg := workflowConfig(t)
	cfg.Scheduler.Cron = "0-59 0-23 1-31 1-12 0-6"
	if _, err := scheduler.Launchd(cfg, base); err == nil {
		t.Fatal("Launchd() accepted an unbounded calendar expansion")
	}
}

func TestLaunchdPreservesSteppedCronFields(t *testing.T) {
	cfg := workflowConfig(t)
	cfg.Scheduler.Cron = "*/15 */6 * * *"
	plist, err := scheduler.Launchd(cfg, scheduler.LaunchdOptions{
		Label:       "io.github.croutoncreations.sb-heartbeat",
		BinaryPath:  "/usr/local/bin/sb-heartbeat",
		ConfigPath:  "/Users/example/sb-heartbeat.yaml",
		EnvFilePath: "/Users/example/heartbeat.env",
	})
	if err != nil {
		t.Fatal(err)
	}
	text := string(plist)
	if got := strings.Count(text, "<key>Minute</key>"); got != 16 {
		t.Fatalf("minute constraints = %d, want 16:\n%s", got, text)
	}
	for _, expected := range []string{"<integer>0</integer>", "<integer>15</integer>", "<integer>30</integer>", "<integer>45</integer>"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("stepped schedule missing %q:\n%s", expected, text)
		}
	}
}
