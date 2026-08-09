package metrics

import (
	"errors"
	"fmt"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/croutoncreations/sb-heartbeat/internal/fileutil"
	"github.com/croutoncreations/sb-heartbeat/internal/heartbeat"
)

var projectNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,62}$`)

func WritePrometheus(path string, finishedAt time.Time, results []heartbeat.Result) error {
	if runtime.GOOS == "windows" {
		return errors.New("Prometheus metrics are not supported on Windows because atomic replacement cannot be guaranteed")
	}
	if err := validate(finishedAt, results); err != nil {
		return err
	}
	ordered := slices.Clone(results)
	slices.SortFunc(ordered, func(left, right heartbeat.Result) int {
		return strings.Compare(left.Name, right.Name)
	})

	var output strings.Builder
	writeMetadata(&output, "sb_heartbeat_run_success", "Whether every project was healthy in the latest run.")
	fmt.Fprintf(&output, "sb_heartbeat_run_success %d\n", boolValue(heartbeat.ExitCode(ordered) == 0))
	writeMetadata(&output, "sb_heartbeat_run_timestamp_seconds", "Unix timestamp of the latest completed heartbeat run.")
	fmt.Fprintf(&output, "sb_heartbeat_run_timestamp_seconds %d\n", finishedAt.Unix())
	writeMetadata(&output, "sb_heartbeat_project_healthy", "Whether the project was healthy in the latest run.")
	writeMetadata(&output, "sb_heartbeat_project_status", "Current stable status of the project in the latest run.")
	writeMetadata(&output, "sb_heartbeat_project_attempts", "Number of HTTP attempts for the project in the latest run.")
	writeMetadata(&output, "sb_heartbeat_project_latency_seconds", "Total project check latency in seconds in the latest run.")
	writeMetadata(&output, "sb_heartbeat_project_http_status", "Observed HTTP response status for the project in the latest run.")
	for _, result := range ordered {
		project := strconv.Quote(result.Name)
		status := strconv.Quote(string(result.Status))
		fmt.Fprintf(&output, "sb_heartbeat_project_healthy{project=%s} %d\n", project, boolValue(result.Status == heartbeat.Healthy))
		fmt.Fprintf(&output, "sb_heartbeat_project_status{project=%s,status=%s} 1\n", project, status)
		fmt.Fprintf(&output, "sb_heartbeat_project_attempts{project=%s} %d\n", project, result.Attempts)
		if result.LatencyMS != nil {
			latency := strconv.FormatFloat(float64(*result.LatencyMS)/1000, 'f', -1, 64)
			fmt.Fprintf(&output, "sb_heartbeat_project_latency_seconds{project=%s} %s\n", project, latency)
		}
		if result.HTTPStatus != nil {
			fmt.Fprintf(&output, "sb_heartbeat_project_http_status{project=%s} %d\n", project, *result.HTTPStatus)
		}
	}
	if err := fileutil.WriteAtomic(path, []byte(output.String()), 0o644, true); err != nil {
		return fmt.Errorf("write Prometheus metrics atomically: %w", err)
	}
	return nil
}

func writeMetadata(output *strings.Builder, name, help string) {
	fmt.Fprintf(output, "# HELP %s %s\n# TYPE %s gauge\n", name, help, name)
}

func boolValue(value bool) int {
	if value {
		return 1
	}
	return 0
}

func validate(finishedAt time.Time, results []heartbeat.Result) error {
	if finishedAt.IsZero() {
		return errors.New("metrics completion time is required")
	}
	if len(results) == 0 {
		return errors.New("metrics require at least one project result")
	}
	seen := make(map[string]struct{}, len(results))
	for _, result := range results {
		if !projectNamePattern.MatchString(result.Name) || !validStatus(result.Status) || result.Attempts < 0 || result.Attempts > 4 {
			return errors.New("metrics contain an invalid project result")
		}
		if _, exists := seen[result.Name]; exists {
			return errors.New("metrics contain duplicate project results")
		}
		seen[result.Name] = struct{}{}
		if result.LatencyMS != nil && *result.LatencyMS < 0 {
			return errors.New("metrics contain an invalid project latency")
		}
		if result.HTTPStatus != nil && (*result.HTTPStatus < 100 || *result.HTTPStatus > 599) {
			return errors.New("metrics contain an invalid HTTP status")
		}
	}
	return nil
}

func validStatus(status heartbeat.Status) bool {
	switch status {
	case heartbeat.Healthy, heartbeat.Timeout, heartbeat.DNSFailure, heartbeat.TLSFailure,
		heartbeat.CredentialRejected, heartbeat.DatabasePermissionDenied, heartbeat.APIAuthenticationFailed,
		heartbeat.TemporaryUpstreamFailure, heartbeat.ProjectPaused, heartbeat.UnexpectedResponse,
		heartbeat.MissingInput, heartbeat.NoMatchingRow, heartbeat.ResponseTooLarge, heartbeat.InternalError:
		return true
	default:
		return false
	}
}
