package history

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"runtime"
	"strconv"
	"time"

	"github.com/croutoncreations/sb-heartbeat/internal/fileutil"
	"github.com/croutoncreations/sb-heartbeat/internal/heartbeat"
)

const (
	SchemaVersion = 1
	MaxFileBytes  = 1024 * 1024
	MaxRuns       = 1000
)

var historyProjectNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,62}$`)

type Run struct {
	StartedAt  time.Time
	FinishedAt time.Time
	Results    []heartbeat.Result
}

type document struct {
	SchemaVersion int         `json:"schema_version"`
	Runs          []storedRun `json:"runs"`
}

type storedRun struct {
	StartedAt  time.Time       `json:"started_at"`
	FinishedAt time.Time       `json:"finished_at"`
	Success    bool            `json:"success"`
	Projects   []storedProject `json:"projects"`
}

type storedProject struct {
	Name       string           `json:"name"`
	Status     heartbeat.Status `json:"status"`
	HTTPStatus *int             `json:"http_status"`
	LatencyMS  *int64           `json:"latency_ms"`
	Attempts   int              `json:"attempts"`
}

func Append(path string, run Run, limit int) error {
	if runtime.GOOS == "windows" {
		return errors.New("local history is not supported on Windows because atomic replacement cannot be guaranteed")
	}
	if limit < 1 || limit > MaxRuns {
		return fmt.Errorf("history limit must be between 1 and %d", MaxRuns)
	}
	doc, err := read(path)
	if err != nil {
		return err
	}
	doc.Runs = append(doc.Runs, sanitize(run))
	if len(doc.Runs) > limit {
		doc.Runs = doc.Runs[len(doc.Runs)-limit:]
	}
	encoded, err := encodeBounded(&doc)
	if err != nil {
		return err
	}
	if err := fileutil.WriteAtomic(path, encoded, 0o600, true); err != nil {
		return fmt.Errorf("write history atomically: %w", err)
	}
	return nil
}

func read(path string) (document, error) {
	doc := document{SchemaVersion: SchemaVersion, Runs: []storedRun{}}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return doc, nil
	}
	if err != nil {
		return document{}, fmt.Errorf("inspect history: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return document{}, errors.New("refusing to read history through a symlink")
	}
	if !info.Mode().IsRegular() {
		return document{}, errors.New("history path is not a regular file")
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return document{}, errors.New("history file permissions must not allow group or other access")
	}
	if info.Size() > MaxFileBytes {
		return document{}, fmt.Errorf("history file is too large: maximum is %d bytes", MaxFileBytes)
	}
	file, err := os.Open(path)
	if err != nil {
		return document{}, fmt.Errorf("open history: %w", err)
	}
	defer file.Close()
	openedInfo, err := file.Stat()
	if err != nil {
		return document{}, fmt.Errorf("inspect opened history: %w", err)
	}
	if !os.SameFile(info, openedInfo) {
		return document{}, errors.New("history file changed while opening")
	}
	contents, err := io.ReadAll(io.LimitReader(file, MaxFileBytes+1))
	if err != nil {
		return document{}, fmt.Errorf("read history: %w", err)
	}
	if len(contents) > MaxFileBytes {
		return document{}, fmt.Errorf("history file is too large: maximum is %d bytes", MaxFileBytes)
	}
	if err := validateHistoryContent(contents); err != nil {
		return document{}, errors.New("invalid history content")
	}
	if err := json.Unmarshal(contents, &doc); err != nil {
		return document{}, errors.New("invalid history content")
	}
	return doc, nil
}

func sanitize(run Run) storedRun {
	stored := storedRun{
		StartedAt: run.StartedAt.UTC(), FinishedAt: run.FinishedAt.UTC(),
		Success:  heartbeat.ExitCode(run.Results) == 0,
		Projects: make([]storedProject, 0, len(run.Results)),
	}
	for _, result := range run.Results {
		stored.Projects = append(stored.Projects, storedProject{
			Name: result.Name, Status: result.Status, HTTPStatus: result.HTTPStatus,
			LatencyMS: result.LatencyMS, Attempts: result.Attempts,
		})
	}
	return stored
}

func encodeBounded(doc *document) ([]byte, error) {
	for {
		encoded, err := json.MarshalIndent(doc, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("encode history: %w", err)
		}
		encoded = append(encoded, '\n')
		if len(encoded) <= MaxFileBytes {
			if err := validateHistoryContent(encoded); err != nil {
				return nil, errors.New("history run contains invalid values")
			}
			return encoded, nil
		}
		if len(doc.Runs) <= 1 {
			return nil, fmt.Errorf("history entry exceeds %d bytes", MaxFileBytes)
		}
		doc.Runs = doc.Runs[1:]
	}
}

func validateHistoryContent(contents []byte) error {
	if err := rejectDuplicateKeys(contents); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return errors.New("trailing JSON")
	}
	root, ok := exactObject(value, "schema_version", "runs")
	if !ok || integer(root["schema_version"]) != SchemaVersion {
		return errors.New("invalid root")
	}
	runs, ok := root["runs"].([]any)
	if !ok || len(runs) > MaxRuns {
		return errors.New("invalid runs")
	}
	for _, rawRun := range runs {
		run, ok := exactObject(rawRun, "started_at", "finished_at", "success", "projects")
		if !ok {
			return errors.New("invalid run")
		}
		started, startOK := timestamp(run["started_at"])
		finished, finishOK := timestamp(run["finished_at"])
		success, successOK := run["success"].(bool)
		projects, projectsOK := run["projects"].([]any)
		if !startOK || !finishOK || finished.Before(started) || !successOK || !projectsOK || len(projects) < 1 {
			return errors.New("invalid run values")
		}
		allHealthy := true
		seenNames := make(map[string]struct{}, len(projects))
		for _, rawProject := range projects {
			project, ok := exactObject(rawProject, "name", "status", "http_status", "latency_ms", "attempts")
			if !ok {
				return errors.New("invalid project")
			}
			name, nameOK := project["name"].(string)
			status, statusOK := project["status"].(string)
			attempts := integer(project["attempts"])
			if !nameOK || !historyProjectNamePattern.MatchString(name) || !statusOK || !validStatus(heartbeat.Status(status)) || attempts < 0 || attempts > 4 ||
				!validNullableInteger(project["http_status"], 100, 599) || !validNullableInteger(project["latency_ms"], 0, int(^uint(0)>>1)) {
				return errors.New("invalid project values")
			}
			if _, exists := seenNames[name]; exists {
				return errors.New("duplicate project")
			}
			seenNames[name] = struct{}{}
			allHealthy = allHealthy && heartbeat.Status(status) == heartbeat.Healthy
		}
		if success != allHealthy {
			return errors.New("invalid success")
		}
	}
	return nil
}

func rejectDuplicateKeys(contents []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.UseNumber()
	if err := scanJSONValue(decoder); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return errors.New("trailing JSON")
	}
	return nil
}

func scanJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		seen := map[string]struct{}{}
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return errors.New("invalid object key")
			}
			if _, exists := seen[key]; exists {
				return errors.New("duplicate object key")
			}
			seen[key] = struct{}{}
			if err := scanJSONValue(decoder); err != nil {
				return err
			}
		}
	case '[':
		for decoder.More() {
			if err := scanJSONValue(decoder); err != nil {
				return err
			}
		}
	default:
		return errors.New("invalid delimiter")
	}
	_, err = decoder.Token()
	return err
}

func exactObject(value any, keys ...string) (map[string]any, bool) {
	object, ok := value.(map[string]any)
	if !ok || len(object) != len(keys) {
		return nil, false
	}
	for _, key := range keys {
		if _, exists := object[key]; !exists {
			return nil, false
		}
	}
	return object, true
}

func integer(value any) int {
	number, ok := value.(json.Number)
	if !ok {
		return -1
	}
	parsed, err := strconv.Atoi(number.String())
	if err != nil {
		return -1
	}
	return parsed
}

func validNullableInteger(value any, minimum, maximum int) bool {
	if value == nil {
		return true
	}
	parsed := integer(value)
	return parsed >= minimum && parsed <= maximum
}

func timestamp(value any) (time.Time, bool) {
	text, ok := value.(string)
	if !ok {
		return time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339Nano, text)
	return parsed, err == nil && !parsed.IsZero()
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
