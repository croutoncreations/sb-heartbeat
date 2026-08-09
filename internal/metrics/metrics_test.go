package metrics_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/croutoncreations/sb-heartbeat/internal/heartbeat"
	"github.com/croutoncreations/sb-heartbeat/internal/metrics"
)

func TestWritePrometheusFileAtomicallyWithSanitizedCurrentResults(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("atomic metrics replacement is intentionally unavailable on Windows")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "textfile", "sb-heartbeat.prom")
	latency := int64(250)
	httpStatus := 200
	results := []heartbeat.Result{
		{Name: "zeta", Status: heartbeat.Timeout, Attempts: 4, Error: &heartbeat.Error{Code: heartbeat.Timeout, Message: "must not persist"}},
		{Name: "alpha", Status: heartbeat.Healthy, HTTPStatus: &httpStatus, LatencyMS: &latency, Attempts: 1},
	}
	finished := time.Unix(1_800_000_000, 987_000_000).UTC()
	if err := metrics.WritePrometheus(path, finished, results); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(contents)
	for _, required := range []string{
		"# HELP sb_heartbeat_run_success Whether every project was healthy in the latest run.",
		"# TYPE sb_heartbeat_run_success gauge",
		"sb_heartbeat_run_success 0",
		"sb_heartbeat_run_timestamp_seconds 1800000000",
		`sb_heartbeat_project_healthy{project="alpha"} 1`,
		`sb_heartbeat_project_status{project="alpha",status="healthy"} 1`,
		`sb_heartbeat_project_latency_seconds{project="alpha"} 0.25`,
		`sb_heartbeat_project_http_status{project="alpha"} 200`,
		`sb_heartbeat_project_attempts{project="zeta"} 4`,
	} {
		if !strings.Contains(text, required) {
			t.Errorf("metrics missing %q\n%s", required, text)
		}
	}
	if strings.Index(text, `project="alpha"`) > strings.Index(text, `project="zeta"`) {
		t.Fatalf("project metrics are not deterministic by name\n%s", text)
	}
	if strings.Contains(text, "must not persist") || strings.Contains(text, "error") {
		t.Fatalf("metrics contain diagnostic material\n%s", text)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o644 {
		t.Fatalf("mode=%v err=%v", info.Mode().Perm(), err)
	}

	if err := metrics.WritePrometheus(path, finished.Add(time.Second), []heartbeat.Result{{Name: "alpha", Status: heartbeat.Healthy, Attempts: 1}}); err != nil {
		t.Fatal(err)
	}
	replaced, err := os.ReadFile(path)
	if err != nil || strings.Contains(string(replaced), "zeta") {
		t.Fatalf("metrics file was not replaced: %q err=%v", replaced, err)
	}
}

func TestWritePrometheusRejectsUnsafeOrInvalidOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		err := metrics.WritePrometheus(filepath.Join(t.TempDir(), "metrics.prom"), time.Now(), []heartbeat.Result{{Name: "demo", Status: heartbeat.Healthy, Attempts: 1}})
		if err == nil || !strings.Contains(err.Error(), "Windows") {
			t.Fatalf("Windows metrics error=%v", err)
		}
		return
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("unchanged"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "metrics.prom")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	valid := []heartbeat.Result{{Name: "demo", Status: heartbeat.Healthy, Attempts: 1}}
	if err := metrics.WritePrometheus(link, time.Now(), valid); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("symlink error=%v", err)
	}
	contents, _ := os.ReadFile(target)
	if string(contents) != "unchanged" {
		t.Fatalf("symlink target changed: %q", contents)
	}
	for name, test := range map[string]struct {
		finished time.Time
		results  []heartbeat.Result
	}{
		"zero time":   {results: valid},
		"empty":       {finished: time.Now(), results: []heartbeat.Result{}},
		"bad project": {finished: time.Now(), results: []heartbeat.Result{{Name: `bad\"name`, Status: heartbeat.Healthy, Attempts: 1}}},
	} {
		t.Run(name, func(t *testing.T) {
			if err := metrics.WritePrometheus(filepath.Join(dir, name+".prom"), test.finished, test.results); err == nil {
				t.Fatal("invalid metrics input accepted")
			}
		})
	}
}
