package scheduler

import (
	"errors"
	"path/filepath"
	"strings"

	"github.com/jfox85/sb-heartbeat/internal/config"
)

func LocalCron(cfg config.Config, binaryPath, configPath, logPath string) (string, error) {
	if err := cfg.Validate(); err != nil {
		return "", err
	}
	if !validCronPath(binaryPath) {
		return "", errors.New("cron binary path must be a safe absolute path without line breaks or percent signs")
	}
	if !validCronPath(configPath) {
		return "", errors.New("cron configuration path must be a safe absolute path without line breaks or percent signs")
	}
	if logPath != "" && !validCronPath(logPath) {
		return "", errors.New("cron log path must be a safe absolute path without line breaks or percent signs")
	}

	environmentNames := make([]string, 0, len(cfg.Projects)*2)
	for _, project := range cfg.Projects {
		environmentNames = append(environmentNames, project.URL.Env, project.APIKey.Env)
	}
	entry := cfg.Scheduler.Cron + " " + shellQuote(binaryPath) + " --config " + shellQuote(configPath) + " run --output json"
	if logPath != "" {
		entry += " >> " + shellQuote(logPath) + " 2>&1"
	}
	return "# Suggested by SB Heartbeat; review before adding it. This command never edits your crontab.\n" +
		"# Make these variables available in cron's environment: " + strings.Join(environmentNames, ", ") + "\n" +
		entry + "\n", nil
}

func validCronPath(value string) bool {
	return filepath.IsAbs(value) && !strings.ContainsAny(value, "%\x00\r\n")
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}
