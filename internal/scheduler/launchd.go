package scheduler

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/croutoncreations/sb-heartbeat/internal/config"
	cron "github.com/robfig/cron/v3"
)

const maxLaunchdCalendarEntries = 512

var launchdLabelPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.-]{0,254}$`)

type LaunchdOptions struct {
	Label       string
	BinaryPath  string
	ConfigPath  string
	EnvFilePath string
	StdoutPath  string
	StderrPath  string
}

type calendarEntry struct {
	minute, hour, day, month, weekday int
}

func Launchd(cfg config.Config, options LaunchdOptions) ([]byte, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if !launchdLabelPattern.MatchString(options.Label) {
		return nil, errors.New("launchd label must contain only letters, digits, dots, and hyphens")
	}
	for label, path := range map[string]string{
		"binary": options.BinaryPath, "configuration": options.ConfigPath, "environment file": options.EnvFilePath,
	} {
		if !validLaunchdPath(path) {
			return nil, fmt.Errorf("launchd %s path must be a safe absolute path", label)
		}
	}
	for label, path := range map[string]string{"standard output": options.StdoutPath, "standard error": options.StderrPath} {
		if path != "" && !validLaunchdPath(path) {
			return nil, fmt.Errorf("launchd %s path must be a safe absolute path", label)
		}
	}
	protectedPaths := []string{options.BinaryPath, options.ConfigPath, options.EnvFilePath}
	if cleanPathsEqual(options.ConfigPath, options.EnvFilePath) {
		return nil, errors.New("launchd configuration and environment file paths must differ")
	}
	for label, path := range map[string]string{"standard output": options.StdoutPath, "standard error": options.StderrPath} {
		if path == "" {
			continue
		}
		for _, protected := range protectedPaths {
			if cleanPathsEqual(path, protected) {
				return nil, fmt.Errorf("launchd %s path must not replace the binary, configuration, or environment file", label)
			}
		}
	}
	entries, err := launchdCalendarEntries(cfg.Scheduler.Cron)
	if err != nil {
		return nil, err
	}

	var result bytes.Buffer
	result.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n")
	result.WriteString("<!DOCTYPE plist PUBLIC \"-//Apple//DTD PLIST 1.0//EN\" \"http://www.apple.com/DTDs/PropertyList-1.0.dtd\">\n")
	result.WriteString("<plist version=\"1.0\">\n<dict>\n")
	writePlistString(&result, "Label", options.Label, 1)
	result.WriteString("  <key>ProgramArguments</key>\n  <array>\n")
	for _, argument := range []string{options.BinaryPath, "--config", options.ConfigPath, "--env-file", options.EnvFilePath, "run", "--output", "json"} {
		writeXMLString(&result, argument, 2)
	}
	result.WriteString("  </array>\n")
	result.WriteString("  <key>StartCalendarInterval</key>\n  <array>\n")
	for _, entry := range entries {
		result.WriteString("    <dict>\n")
		writeCalendarInteger(&result, "Minute", entry.minute)
		writeCalendarInteger(&result, "Hour", entry.hour)
		writeCalendarInteger(&result, "Day", entry.day)
		writeCalendarInteger(&result, "Month", entry.month)
		writeCalendarInteger(&result, "Weekday", entry.weekday)
		result.WriteString("    </dict>\n")
	}
	result.WriteString("  </array>\n")
	writePlistString(&result, "ProcessType", "Background", 1)
	if options.StdoutPath != "" {
		writePlistString(&result, "StandardOutPath", options.StdoutPath, 1)
	}
	if options.StderrPath != "" {
		writePlistString(&result, "StandardErrorPath", options.StderrPath, 1)
	}
	result.WriteString("</dict>\n</plist>\n")
	return result.Bytes(), nil
}

func validLaunchdPath(path string) bool {
	if !filepath.IsAbs(path) || !utf8.ValidString(path) || strings.ContainsAny(path, "\x00\r\n") {
		return false
	}
	for _, character := range path {
		if unicode.IsControl(character) || !validXMLCharacter(character) {
			return false
		}
	}
	return true
}

func validXMLCharacter(character rune) bool {
	return character == 0x9 || character == 0xA || character == 0xD ||
		character >= 0x20 && character <= 0xD7FF ||
		character >= 0xE000 && character <= 0xFFFD ||
		character >= 0x10000 && character <= 0x10FFFF
}

func cleanPathsEqual(left, right string) bool {
	return filepath.Clean(left) == filepath.Clean(right)
}

func launchdCalendarEntries(spec string) ([]calendarEntry, error) {
	parsed, err := cron.ParseStandard(spec)
	if err != nil {
		return nil, errors.New("launchd schedule must be a valid five-field POSIX cron expression")
	}
	schedule, ok := parsed.(*cron.SpecSchedule)
	if !ok {
		return nil, errors.New("launchd schedule could not be represented")
	}
	minutes := selectedBits(schedule.Minute, 0, 59)
	hours := selectedBits(schedule.Hour, 0, 23)
	days := selectedBits(schedule.Dom, 1, 31)
	months := selectedBits(schedule.Month, 1, 12)
	weekdays := selectedBits(schedule.Dow, 0, 6)
	dayWildcard := schedule.Dom&(uint64(1)<<63) != 0
	weekdayWildcard := schedule.Dow&(uint64(1)<<63) != 0
	if !dayWildcard && !weekdayWildcard {
		return nil, errors.New("launchd schedules cannot restrict both day-of-month and weekday because their semantics differ from POSIX cron")
	}
	if schedule.Minute&(uint64(1)<<63) != 0 {
		minutes = []int{-1}
	}
	if schedule.Hour&(uint64(1)<<63) != 0 {
		hours = []int{-1}
	}
	if dayWildcard {
		days = []int{-1}
	}
	if schedule.Month&(uint64(1)<<63) != 0 {
		months = []int{-1}
	}
	if weekdayWildcard {
		weekdays = []int{-1}
	}
	entryCount := len(minutes) * len(hours) * len(days) * len(months) * len(weekdays)
	if entryCount == 0 || entryCount > maxLaunchdCalendarEntries {
		return nil, fmt.Errorf("launchd schedule expands to %d calendar entries; maximum is %d", entryCount, maxLaunchdCalendarEntries)
	}
	entries := make([]calendarEntry, 0, entryCount)
	for _, month := range months {
		for _, day := range days {
			for _, weekday := range weekdays {
				for _, hour := range hours {
					for _, minute := range minutes {
						entries = append(entries, calendarEntry{minute: minute, hour: hour, day: day, month: month, weekday: weekday})
					}
				}
			}
		}
	}
	return entries, nil
}

func selectedBits(mask uint64, minimum, maximum int) []int {
	values := make([]int, 0, maximum-minimum+1)
	for value := minimum; value <= maximum; value++ {
		if mask&(uint64(1)<<value) != 0 {
			values = append(values, value)
		}
	}
	return values
}

func writePlistString(result *bytes.Buffer, key, value string, indent int) {
	prefix := strings.Repeat("  ", indent)
	fmt.Fprintf(result, "%s<key>%s</key>\n", prefix, key)
	result.WriteString(prefix)
	writeXMLString(result, value, 0)
}

func writeXMLString(result *bytes.Buffer, value string, indent int) {
	result.WriteString(strings.Repeat("  ", indent))
	result.WriteString("<string>")
	for _, character := range value {
		switch character {
		case '&':
			result.WriteString("&amp;")
		case '<':
			result.WriteString("&lt;")
		case '>':
			result.WriteString("&gt;")
		case '"':
			result.WriteString("&quot;")
		case '\'':
			result.WriteString("&apos;")
		default:
			result.WriteRune(character)
		}
	}
	result.WriteString("</string>\n")
}

func writeCalendarInteger(result *bytes.Buffer, key string, value int) {
	if value < 0 {
		return
	}
	fmt.Fprintf(result, "      <key>%s</key>\n      <integer>%d</integer>\n", key, value)
}
