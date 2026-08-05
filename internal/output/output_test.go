package output_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/jfox85/sb-heartbeat/internal/heartbeat"
	"github.com/jfox85/sb-heartbeat/internal/output"
)

func intPointer(value int) *int       { return &value }
func int64Pointer(value int64) *int64 { return &value }

func TestWriteText(t *testing.T) {
	results := []heartbeat.Result{
		{Name: "demo", Status: heartbeat.Healthy, LatencyMS: int64Pointer(42), Attempts: 1},
		{Name: "paused", Status: heartbeat.ProjectPaused, HTTPStatus: intPointer(540), Attempts: 1},
	}
	var buf bytes.Buffer
	if err := output.WriteText(&buf, results); err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	if !strings.Contains(got, "✓ demo healthy 42ms") || !strings.Contains(got, "✗ paused project_paused") {
		t.Fatalf("text output = %q", got)
	}
}

func TestWriteJSONUsesVersionedEnvelope(t *testing.T) {
	started := time.Date(2026, 8, 4, 10, 0, 0, 0, time.UTC)
	finished := started.Add(time.Second)
	run := output.Run{
		StartedAt:  started,
		FinishedAt: finished,
		Results:    []heartbeat.Result{{Name: "demo", Status: heartbeat.Healthy, Attempts: 1}},
	}
	var buf bytes.Buffer
	if err := output.WriteJSON(&buf, run); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["schema_version"] != float64(1) || got["success"] != true {
		t.Fatalf("JSON envelope = %#v", got)
	}
}

func TestWriteFailureJSON(t *testing.T) {
	var buf bytes.Buffer
	if err := output.WriteFailureJSON(&buf, "invalid_configuration", "configuration could not be loaded"); err != nil {
		t.Fatal(err)
	}
	if got := buf.String(); !strings.Contains(got, `"schema_version": 1`) || !strings.Contains(got, `"success": false`) {
		t.Fatalf("failure JSON = %q", got)
	}
}
