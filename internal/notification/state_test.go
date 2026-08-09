package notification_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/croutoncreations/sb-heartbeat/internal/heartbeat"
	"github.com/croutoncreations/sb-heartbeat/internal/notification"
)

var observedAt = time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)

func TestStateNotifiesOnceAfterThresholdAndResetsOnRecovery(t *testing.T) {
	requireSupported(t)
	path := filepath.Join(t.TempDir(), "private", "notifications.json")
	failure := result("stage", heartbeat.Timeout)

	for run := 1; run <= 2; run++ {
		events, err := notification.Advance(path, observedAt.Add(time.Duration(run)*time.Minute), []heartbeat.Result{failure}, 3)
		if err != nil || len(events) != 0 {
			t.Fatalf("failure run %d: events=%+v err=%v", run, events, err)
		}
	}
	events, err := notification.Advance(path, observedAt.Add(3*time.Minute), []heartbeat.Result{failure}, 3)
	if err != nil || len(events) != 1 || events[0].Project != "stage" || events[0].Status != heartbeat.Timeout || events[0].ConsecutiveFailures != 3 {
		t.Fatalf("threshold events=%+v err=%v", events, err)
	}
	if err := notification.MarkDelivered(path, events[0]); err != nil {
		t.Fatal(err)
	}
	events, err = notification.Advance(path, observedAt.Add(4*time.Minute), []heartbeat.Result{failure}, 3)
	if err != nil || len(events) != 0 {
		t.Fatalf("delivered episode repeated: events=%+v err=%v", events, err)
	}
	if events, err = notification.Advance(path, observedAt.Add(5*time.Minute), []heartbeat.Result{result("stage", heartbeat.Healthy)}, 3); err != nil || len(events) != 0 {
		t.Fatalf("healthy reset: events=%+v err=%v", events, err)
	}
	for run := 1; run <= 3; run++ {
		events, err = notification.Advance(path, observedAt.Add(time.Duration(5+run)*time.Minute), []heartbeat.Result{failure}, 3)
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(events) != 1 || events[0].ConsecutiveFailures != 3 {
		t.Fatalf("new failure episode events=%+v", events)
	}
}

func TestDeliveredEventCannotBeReplayedAcrossFailureEpisodes(t *testing.T) {
	requireSupported(t)
	path := filepath.Join(t.TempDir(), "notifications.json")
	events, err := notification.Advance(path, observedAt, []heartbeat.Result{result("stage", heartbeat.Timeout)}, 1)
	if err != nil || len(events) != 1 {
		t.Fatalf("first episode: events=%+v err=%v", events, err)
	}
	oldEvent := events[0]
	if _, err := notification.Advance(path, observedAt, []heartbeat.Result{result("stage", heartbeat.Healthy)}, 1); err != nil {
		t.Fatal(err)
	}
	newEvents, err := notification.Advance(path, observedAt, []heartbeat.Result{result("stage", heartbeat.Timeout)}, 1)
	if err != nil || len(newEvents) != 1 || newEvents[0].Episode == oldEvent.Episode {
		t.Fatalf("new episode: old=%+v new=%+v err=%v", oldEvent, newEvents, err)
	}
	if err := notification.MarkDelivered(path, oldEvent); err == nil {
		t.Fatal("an event from an earlier episode suppressed the current notification")
	}
	if err := notification.MarkDelivered(path, newEvents[0]); err != nil {
		t.Fatal(err)
	}
}

func TestUndeliveredEventRemainsPendingAndProjectsAdvanceIndependently(t *testing.T) {
	requireSupported(t)
	path := filepath.Join(t.TempDir(), "notifications.json")
	results := []heartbeat.Result{result("stage", heartbeat.DNSFailure), result("prod", heartbeat.Healthy)}
	if events, err := notification.Advance(path, observedAt, results, 2); err != nil || len(events) != 0 {
		t.Fatalf("first advance: events=%+v err=%v", events, err)
	}
	events, err := notification.Advance(path, observedAt.Add(time.Minute), []heartbeat.Result{result("stage", heartbeat.DNSFailure)}, 2)
	if err != nil || len(events) != 1 {
		t.Fatalf("threshold advance: events=%+v err=%v", events, err)
	}
	retry, err := notification.Advance(path, observedAt.Add(2*time.Minute), []heartbeat.Result{result("stage", heartbeat.TLSFailure)}, 2)
	if err != nil || len(retry) != 1 || retry[0].Status != heartbeat.TLSFailure || retry[0].ConsecutiveFailures != 3 {
		t.Fatalf("pending retry=%+v err=%v", retry, err)
	}
	if err := notification.MarkDelivered(path, events[0]); err == nil {
		t.Fatal("stale event marked a newer pending notification delivered")
	}
	if err := notification.MarkDelivered(path, retry[0]); err != nil {
		t.Fatal(err)
	}
}

func TestStateIsSanitizedPrivateAndStrictlyValidated(t *testing.T) {
	requireSupported(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "notifications.json")
	unsafe := heartbeat.Result{
		Name: "stage", Status: heartbeat.UnexpectedResponse, Attempts: 1,
		Error: &heartbeat.Error{Code: heartbeat.UnexpectedResponse, Message: "sb_secret_response_body"},
	}
	if _, err := notification.Advance(path, observedAt, []heartbeat.Result{unsafe}, 1); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(contents), "sb_secret") || strings.Contains(string(contents), "response_body") || strings.Contains(string(contents), "error") {
		t.Fatalf("state retained diagnostic material: %s", contents)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("state mode: info=%v err=%v", info, err)
	}

	for name, invalid := range map[string]string{
		"unknown":   `{"schema_version":1,"projects":[],"credential":"value"}`,
		"duplicate": `{"schema_version":1,"schema_version":1,"projects":[]}`,
		"missing":   `{"schema_version":1,"projects":[{"name":"stage","episode":1,"consecutive_failures":1,"last_status":"timeout","observed_at":"2026-08-09T12:00:00Z","pending":true}]}`,
		"semantic":  `{"schema_version":1,"projects":[{"name":"stage","episode":1,"consecutive_failures":-1,"last_status":"healthy","observed_at":"bad","pending":false,"notified":false}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			invalidPath := filepath.Join(dir, name+".json")
			if err := os.WriteFile(invalidPath, []byte(invalid), 0o600); err != nil {
				t.Fatal(err)
			}
			err := advanceError(invalidPath)
			if err == nil || strings.Contains(err.Error(), "credential") || strings.Contains(err.Error(), "value") {
				t.Fatalf("invalid state error=%v", err)
			}
		})
	}
}

func TestStateRejectsUnsafeFilesInvalidInputsAndOversizeContent(t *testing.T) {
	requireSupported(t)
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte(`{"schema_version":1,"projects":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.json")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := advanceError(link); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("symlink error=%v", err)
	}
	permissive := filepath.Join(dir, "permissive.json")
	if err := os.WriteFile(permissive, []byte(`{"schema_version":1,"projects":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := advanceError(permissive); err == nil || !strings.Contains(err.Error(), "permissions") {
		t.Fatalf("permission error=%v", err)
	}
	oversize := filepath.Join(dir, "oversize.json")
	if err := os.WriteFile(oversize, make([]byte, notification.MaxFileBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := advanceError(oversize); err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("oversize error=%v", err)
	}
	if _, err := notification.Advance(filepath.Join(dir, "bad-threshold.json"), observedAt, []heartbeat.Result{result("stage", heartbeat.Timeout)}, 0); err == nil {
		t.Fatal("zero threshold accepted")
	}
	if _, err := notification.Advance(filepath.Join(dir, "bad-result.json"), observedAt, []heartbeat.Result{result("Stage URL", heartbeat.Timeout)}, 2); err == nil {
		t.Fatal("invalid project name accepted")
	}
}

func result(name string, status heartbeat.Status) heartbeat.Result {
	return heartbeat.Result{Name: name, Status: status, Attempts: 1}
}

func advanceError(path string) error {
	_, err := notification.Advance(path, observedAt, []heartbeat.Result{result("stage", heartbeat.Timeout)}, 2)
	return err
}

func requireSupported(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("notification state requires atomic POSIX replacement")
	}
}
