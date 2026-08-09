package history_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/croutoncreations/sb-heartbeat/internal/heartbeat"
	"github.com/croutoncreations/sb-heartbeat/internal/history"
)

func TestAppendCreatesPrivateBoundedSanitizedHistory(t *testing.T) {
	requireHistorySupported(t)
	path := filepath.Join(t.TempDir(), "status", "history.json")
	for index := range 4 {
		run := history.Run{
			StartedAt:  time.Date(2026, 8, 9, index, 0, 0, 0, time.UTC),
			FinishedAt: time.Date(2026, 8, 9, index, 0, 1, 0, time.UTC),
			Results: []heartbeat.Result{{
				Name: "stage", Status: heartbeat.UnexpectedResponse, Attempts: 2,
				Error: &heartbeat.Error{Code: heartbeat.UnexpectedResponse, Message: "secret response body"},
			}},
		}
		if err := history.Append(path, run, 3); err != nil {
			t.Fatal(err)
		}
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(contents), "secret response body") || strings.Contains(string(contents), "error") {
		t.Fatalf("history retained sensitive diagnostic material: %s", contents)
	}
	var document struct {
		SchemaVersion int `json:"schema_version"`
		Runs          []struct {
			StartedAt time.Time `json:"started_at"`
			Projects  []struct {
				Name     string           `json:"name"`
				Status   heartbeat.Status `json:"status"`
				Attempts int              `json:"attempts"`
			} `json:"projects"`
		} `json:"runs"`
	}
	if err := json.Unmarshal(contents, &document); err != nil {
		t.Fatal(err)
	}
	if document.SchemaVersion != 1 || len(document.Runs) != 3 || document.Runs[0].StartedAt.Hour() != 1 {
		t.Fatalf("history = %+v", document)
	}
	if document.Runs[2].Projects[0].Name != "stage" || document.Runs[2].Projects[0].Attempts != 2 {
		t.Fatalf("project history = %+v", document.Runs[2].Projects)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("history mode = %o, want 600", info.Mode().Perm())
	}
}

func TestAppendRejectsUnsafeOrInvalidExistingHistory(t *testing.T) {
	requireHistorySupported(t)
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "history.json")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := history.Append(link, history.Run{}, 10); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("symlink error = %v", err)
	}

	invalid := filepath.Join(dir, "invalid.json")
	if err := os.WriteFile(invalid, []byte(`{"schema_version":1,"runs":[],"credential":"value"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := history.Append(invalid, history.Run{}, 10); err == nil {
		t.Fatal("invalid history was accepted")
	}
}

func TestAppendRejectsSemanticAndDuplicateHistoryWithoutEchoingContent(t *testing.T) {
	requireHistorySupported(t)
	dir := t.TempDir()
	for name, contents := range map[string]string{
		"semantic":  `{"schema_version":1,"runs":[{"started_at":null,"finished_at":null,"success":false,"projects":[{"name":"sb_secret_must_not_echo","status":"captured response body","http_status":null,"latency_ms":-1,"attempts":-1}]}]}`,
		"duplicate": `{"schema_version":1,"schema_version":1,"runs":[]}`,
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(dir, name+".json")
			if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
				t.Fatal(err)
			}
			err := history.Append(path, history.Run{}, 10)
			if err == nil || strings.Contains(err.Error(), "sb_secret") || strings.Contains(err.Error(), "captured response") {
				t.Fatalf("error = %v", err)
			}
			if !strings.Contains(err.Error(), "invalid history content") {
				t.Fatalf("error is not content-free: %v", err)
			}
		})
	}
}

func TestAppendValidatesLimitAndExistingSize(t *testing.T) {
	requireHistorySupported(t)
	path := filepath.Join(t.TempDir(), "history.json")
	if err := history.Append(path, history.Run{}, 0); err == nil {
		t.Fatal("zero limit was accepted")
	}
	if err := os.WriteFile(path, make([]byte, history.MaxFileBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := history.Append(path, history.Run{}, 10); err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("oversize error = %v", err)
	}
}

func TestAppendRejectsOverlyPermissiveExistingHistory(t *testing.T) {
	requireHistorySupported(t)
	path := filepath.Join(t.TempDir(), "history.json")
	if err := os.WriteFile(path, []byte(`{"schema_version":1,"runs":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := history.Append(path, history.Run{}, 10); err == nil || !strings.Contains(err.Error(), "permissions") {
		t.Fatalf("permissions error = %v", err)
	}
}

func TestAppendSupportsMoreProjectsThanConcurrencyLimit(t *testing.T) {
	requireHistorySupported(t)
	results := make([]heartbeat.Result, 20)
	for index := range results {
		results[index] = heartbeat.Result{Name: "project_" + strconv.Itoa(index), Status: heartbeat.Healthy, Attempts: 1}
	}
	run := history.Run{
		StartedAt:  time.Date(2026, 8, 9, 1, 0, 0, 0, time.UTC),
		FinishedAt: time.Date(2026, 8, 9, 1, 0, 1, 0, time.UTC),
		Results:    results,
	}
	if err := history.Append(filepath.Join(t.TempDir(), "history.json"), run, 10); err != nil {
		t.Fatal(err)
	}
}

func TestAppendIsExplicitlyUnavailableOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-specific contract")
	}
	err := history.Append(filepath.Join(t.TempDir(), "history.json"), history.Run{}, 10)
	if err == nil || !strings.Contains(err.Error(), "not supported on Windows") {
		t.Fatalf("error = %v", err)
	}
}

func requireHistorySupported(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("local history requires atomic POSIX replacement")
	}
}
