package scheduler_test

import (
	"strings"
	"testing"

	"github.com/croutoncreations/sb-heartbeat/internal/scheduler"
)

func TestSystemdGeneratesHardenedUserServiceAndTimer(t *testing.T) {
	service, timer, err := scheduler.Systemd(workflowConfig(t), scheduler.SystemdOptions{
		UnitName:    "sb-heartbeat",
		BinaryPath:  `/home/example/SB "Heartbeat":bin\sb-heartbeat`,
		ConfigPath:  "/home/example/Tone Clone/%config/sb-heartbeat.yaml",
		EnvFilePath: "/home/example/.config/sb-heartbeat/$private.env",
	})
	if err != nil {
		t.Fatal(err)
	}
	serviceText := string(service)
	for _, required := range []string{
		"[Service]", "Type=oneshot", "ExecStart=:",
		`"/home/example/SB \"Heartbeat\":bin\\sb-heartbeat"`,
		`"/home/example/Tone Clone/%%config/sb-heartbeat.yaml"`,
		`"/home/example/.config/sb-heartbeat/$private.env"`,
		`"--env-file"`, `"run"`, `"--output"`, `"json"`,
		"NoNewPrivileges=true", "ProtectSystem=strict", "ProtectHome=read-only",
		"PrivateUsers=true", "PrivateDevices=true", "PrivateTmp=true",
		"RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6", "UMask=0077",
	} {
		if !strings.Contains(serviceText, required) {
			t.Errorf("service missing %q:\n%s", required, serviceText)
		}
	}
	for _, forbidden := range []string{
		"sb_publishable_", "Environment=", "/bin/sh", "systemctl",
		"DevicePolicy=", "ProtectControlGroups=",
	} {
		if strings.Contains(serviceText, forbidden) {
			t.Fatalf("service contains unsafe or unexpected %q:\n%s", forbidden, serviceText)
		}
	}

	timerText := string(timer)
	for _, required := range []string{
		"[Timer]", "Unit=sb-heartbeat.service", "Persistent=true", "AccuracySec=1m",
		"OnCalendar=*-*-* 03:37:00", "OnCalendar=*-*-* 11:37:00", "OnCalendar=*-*-* 19:37:00",
		"[Install]", "WantedBy=timers.target",
	} {
		if !strings.Contains(timerText, required) {
			t.Errorf("timer missing %q:\n%s", required, timerText)
		}
	}
	if got := strings.Count(timerText, "OnCalendar="); got != 3 {
		t.Fatalf("OnCalendar entries = %d, want 3", got)
	}
}

func TestSystemdRejectsUnsafeOptionsAndUnboundedSchedules(t *testing.T) {
	base := scheduler.SystemdOptions{
		UnitName: "sb-heartbeat", BinaryPath: "/usr/local/bin/sb-heartbeat",
		ConfigPath: "/home/example/sb-heartbeat.yaml", EnvFilePath: "/home/example/heartbeat.env",
	}
	tests := map[string]func(*scheduler.SystemdOptions){
		"bad unit":        func(o *scheduler.SystemdOptions) { o.UnitName = "../bad" },
		"template unit":   func(o *scheduler.SystemdOptions) { o.UnitName = "sb-heartbeat@" },
		"instance unit":   func(o *scheduler.SystemdOptions) { o.UnitName = "sb-heartbeat@daily" },
		"relative binary": func(o *scheduler.SystemdOptions) { o.BinaryPath = "sb-heartbeat" },
		"relative config": func(o *scheduler.SystemdOptions) { o.ConfigPath = "config.yaml" },
		"relative env":    func(o *scheduler.SystemdOptions) { o.EnvFilePath = "heartbeat.env" },
		"line break":      func(o *scheduler.SystemdOptions) { o.ConfigPath = "/tmp/good\nExecStart=/bad" },
		"control":         func(o *scheduler.SystemdOptions) { o.ConfigPath = "/tmp/bad\x01path" },
		"config env collision": func(o *scheduler.SystemdOptions) {
			o.EnvFilePath = o.ConfigPath
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			options := base
			mutate(&options)
			if _, _, err := scheduler.Systemd(workflowConfig(t), options); err == nil {
				t.Fatal("Systemd() error = nil")
			}
		})
	}
	cfg := workflowConfig(t)
	cfg.Scheduler.Cron = "0-59 0-23 1-31 1-12 0-6"
	if _, _, err := scheduler.Systemd(cfg, base); err == nil {
		t.Fatal("Systemd() accepted an unbounded schedule")
	}
}

func TestSystemdFormatsWeekdayAndCalendarRestrictions(t *testing.T) {
	cfg := workflowConfig(t)
	cfg.Scheduler.Cron = "15 4 * 1,7 1,5"
	_, timer, err := scheduler.Systemd(cfg, scheduler.SystemdOptions{
		UnitName: "sb-heartbeat", BinaryPath: "/usr/local/bin/sb-heartbeat",
		ConfigPath: "/home/example/sb-heartbeat.yaml", EnvFilePath: "/home/example/heartbeat.env",
	})
	if err != nil {
		t.Fatal(err)
	}
	text := string(timer)
	for _, expected := range []string{"OnCalendar=Mon *-01-* 04:15:00", "OnCalendar=Fri *-07-* 04:15:00"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("timer missing %q:\n%s", expected, text)
		}
	}
}
