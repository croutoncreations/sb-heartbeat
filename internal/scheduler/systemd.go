package scheduler

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/croutoncreations/sb-heartbeat/internal/config"
)

var systemdUnitNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)

type SystemdOptions struct {
	UnitName    string
	BinaryPath  string
	ConfigPath  string
	EnvFilePath string
}

func Systemd(cfg config.Config, options SystemdOptions) ([]byte, []byte, error) {
	if err := cfg.Validate(); err != nil {
		return nil, nil, err
	}
	if !systemdUnitNamePattern.MatchString(options.UnitName) || strings.HasSuffix(options.UnitName, ".service") || strings.HasSuffix(options.UnitName, ".timer") {
		return nil, nil, errors.New("systemd unit name must be a non-template base name containing only letters, digits, dots, underscores, and hyphens")
	}
	for label, path := range map[string]string{
		"binary": options.BinaryPath, "configuration": options.ConfigPath, "environment file": options.EnvFilePath,
	} {
		if !validLaunchdPath(path) {
			return nil, nil, fmt.Errorf("systemd %s path must be a safe absolute UTF-8 path", label)
		}
	}
	if cleanPathsEqual(options.ConfigPath, options.EnvFilePath) {
		return nil, nil, errors.New("systemd configuration and environment file paths must differ")
	}
	entries, err := launchdCalendarEntries(cfg.Scheduler.Cron)
	if err != nil {
		return nil, nil, errors.New(strings.NewReplacer("launchd", "systemd", "calendar dictionaries", "calendar events").Replace(err.Error()))
	}

	arguments := []string{options.BinaryPath, "--config", options.ConfigPath, "--env-file", options.EnvFilePath, "run", "--output", "json"}
	quoted := make([]string, 0, len(arguments))
	for _, argument := range arguments {
		quoted = append(quoted, systemdQuoteArgument(argument))
	}
	var service bytes.Buffer
	service.WriteString("[Unit]\n")
	service.WriteString("Description=SB Heartbeat least-privilege Supabase heartbeat\n")
	service.WriteString("Documentation=https://github.com/croutoncreations/sb-heartbeat\n")
	service.WriteString("After=network-online.target\nWants=network-online.target\n\n")
	service.WriteString("[Service]\nType=oneshot\n")
	service.WriteString("# The ':' prefix disables systemd environment-variable expansion for every argument.\n")
	service.WriteString("ExecStart=:" + strings.Join(quoted, " ") + "\n")
	service.WriteString("NoNewPrivileges=true\nPrivateUsers=true\nPrivateDevices=true\nPrivateTmp=true\nProtectSystem=strict\nProtectHome=read-only\n")
	service.WriteString("ProtectKernelTunables=true\nProtectKernelModules=true\nProtectKernelLogs=true\n")
	service.WriteString("RestrictSUIDSGID=true\nLockPersonality=true\nMemoryDenyWriteExecute=true\nSystemCallArchitectures=native\n")
	service.WriteString("RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6\nCapabilityBoundingSet=\nUMask=0077\n")

	var timer bytes.Buffer
	timer.WriteString("[Unit]\nDescription=Schedule SB Heartbeat\n\n[Timer]\n")
	for _, entry := range entries {
		timer.WriteString("OnCalendar=" + systemdCalendar(entry) + "\n")
	}
	timer.WriteString("Persistent=true\nAccuracySec=1m\nUnit=" + options.UnitName + ".service\n\n")
	timer.WriteString("[Install]\nWantedBy=timers.target\n")
	return service.Bytes(), timer.Bytes(), nil
}

func systemdQuoteArgument(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, "\"", "\\\"")
	value = strings.ReplaceAll(value, "%", "%%")
	return "\"" + value + "\""
}

func systemdCalendar(entry calendarEntry) string {
	weekday := ""
	if entry.weekday >= 0 {
		weekday = []string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}[entry.weekday] + " "
	}
	month, day := "*", "*"
	if entry.month >= 0 {
		month = fmt.Sprintf("%02d", entry.month)
	}
	if entry.day >= 0 {
		day = fmt.Sprintf("%02d", entry.day)
	}
	hour, minute := "*", "*"
	if entry.hour >= 0 {
		hour = fmt.Sprintf("%02d", entry.hour)
	}
	if entry.minute >= 0 {
		minute = fmt.Sprintf("%02d", entry.minute)
	}
	return fmt.Sprintf("%s*-%s-%s %s:%s:00", weekday, month, day, hour, minute)
}
