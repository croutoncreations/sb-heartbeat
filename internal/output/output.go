package output

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/croutoncreations/sb-heartbeat/internal/heartbeat"
)

const SchemaVersion = 1

type Run struct {
	StartedAt  time.Time
	FinishedAt time.Time
	Results    []heartbeat.Result
}

type runEnvelope struct {
	SchemaVersion int                `json:"schema_version"`
	StartedAt     time.Time          `json:"started_at"`
	FinishedAt    time.Time          `json:"finished_at"`
	Success       bool               `json:"success"`
	Projects      []heartbeat.Result `json:"projects"`
}

type failureEnvelope struct {
	SchemaVersion int          `json:"schema_version"`
	Success       bool         `json:"success"`
	Error         failureError `json:"error"`
}

type failureError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func WriteText(w io.Writer, results []heartbeat.Result) error {
	for _, result := range results {
		if result.Status == heartbeat.Healthy {
			if result.LatencyMS != nil {
				if _, err := fmt.Fprintf(w, "✓ %s healthy %dms\n", result.Name, *result.LatencyMS); err != nil {
					return err
				}
			} else if _, err := fmt.Fprintf(w, "✓ %s healthy\n", result.Name); err != nil {
				return err
			}
			continue
		}
		if _, err := fmt.Fprintf(w, "✗ %s %s\n", result.Name, result.Status); err != nil {
			return err
		}
	}
	return nil
}

func WriteJSON(w io.Writer, run Run) error {
	envelope := runEnvelope{
		SchemaVersion: SchemaVersion,
		StartedAt:     run.StartedAt,
		FinishedAt:    run.FinishedAt,
		Success:       heartbeat.ExitCode(run.Results) == 0,
		Projects:      run.Results,
	}
	return writeIndentedJSON(w, envelope)
}

func WriteFailureJSON(w io.Writer, code, message string) error {
	return writeIndentedJSON(w, failureEnvelope{
		SchemaVersion: SchemaVersion,
		Success:       false,
		Error:         failureError{Code: code, Message: message},
	})
}

func writeIndentedJSON(w io.Writer, value any) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	return encoder.Encode(value)
}
