package notification

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"runtime"
	"sort"
	"time"

	"github.com/croutoncreations/sb-heartbeat/internal/fileutil"
	"github.com/croutoncreations/sb-heartbeat/internal/heartbeat"
)

const (
	SchemaVersion = 1
	MaxFileBytes  = 256 * 1024
	MaxProjects   = 1000
	MaxThreshold  = 100
)

var projectNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,62}$`)

type Event struct {
	Project             string
	Status              heartbeat.Status
	Episode             uint64
	ConsecutiveFailures int
	ObservedAt          time.Time
}

type document struct {
	SchemaVersion int            `json:"schema_version"`
	Projects      []projectState `json:"projects"`
}

type projectState struct {
	Name                string           `json:"name"`
	Episode             uint64           `json:"episode"`
	ConsecutiveFailures int              `json:"consecutive_failures"`
	LastStatus          heartbeat.Status `json:"last_status"`
	ObservedAt          time.Time        `json:"observed_at"`
	Pending             bool             `json:"pending"`
	Notified            bool             `json:"notified"`
}

func Advance(path string, observedAt time.Time, results []heartbeat.Result, threshold int) ([]Event, error) {
	if err := validateRequest(path, observedAt, results, threshold); err != nil {
		return nil, err
	}
	doc, err := read(path)
	if err != nil {
		return nil, err
	}
	states := make(map[string]projectState, len(doc.Projects)+len(results))
	for _, state := range doc.Projects {
		states[state.Name] = state
	}
	events := make([]Event, 0, len(results))
	for _, result := range results {
		state := states[result.Name]
		state.Name = result.Name
		state.ObservedAt = observedAt.UTC()
		if result.Status == heartbeat.Healthy {
			state.ConsecutiveFailures = 0
			state.Pending = false
			state.Notified = false
		} else {
			if state.LastStatus == "" || state.LastStatus == heartbeat.Healthy {
				if state.Episode == ^uint64(0) {
					return nil, errors.New("notification episode counter is exhausted")
				}
				state.Episode++
			}
			if state.ConsecutiveFailures < int(^uint(0)>>1) {
				state.ConsecutiveFailures++
			}
			if state.ConsecutiveFailures >= threshold && !state.Notified {
				state.Pending = true
			}
		}
		state.LastStatus = result.Status
		states[result.Name] = state
		if state.Pending {
			events = append(events, eventFromState(state))
		}
	}
	doc.Projects = sortedStates(states)
	if err := write(path, doc); err != nil {
		return nil, err
	}
	return events, nil
}

func MarkDelivered(path string, event Event) error {
	if runtime.GOOS == "windows" {
		return errors.New("notification state is not supported on Windows because atomic replacement cannot be guaranteed")
	}
	if path == "" {
		return errors.New("notification state path is empty")
	}
	doc, err := read(path)
	if err != nil {
		return err
	}
	found := false
	for index := range doc.Projects {
		state := &doc.Projects[index]
		if state.Name == event.Project && state.Pending && state.LastStatus == event.Status && state.Episode == event.Episode &&
			state.ConsecutiveFailures == event.ConsecutiveFailures && state.ObservedAt.Equal(event.ObservedAt) {
			state.Pending = false
			state.Notified = true
			found = true
			break
		}
	}
	if !found {
		return errors.New("notification event is stale or is not pending")
	}
	return write(path, doc)
}

func validateRequest(path string, observedAt time.Time, results []heartbeat.Result, threshold int) error {
	if runtime.GOOS == "windows" {
		return errors.New("notification state is not supported on Windows because atomic replacement cannot be guaranteed")
	}
	if path == "" {
		return errors.New("notification state path is empty")
	}
	if observedAt.IsZero() {
		return errors.New("notification observation time is required")
	}
	if threshold < 1 || threshold > MaxThreshold {
		return fmt.Errorf("notification threshold must be between 1 and %d", MaxThreshold)
	}
	if len(results) == 0 || len(results) > MaxProjects {
		return fmt.Errorf("notification results must contain between 1 and %d projects", MaxProjects)
	}
	seen := make(map[string]struct{}, len(results))
	for _, result := range results {
		if !projectNamePattern.MatchString(result.Name) || !validStatus(result.Status) {
			return errors.New("notification result contains invalid values")
		}
		if _, exists := seen[result.Name]; exists {
			return errors.New("notification result contains duplicate projects")
		}
		seen[result.Name] = struct{}{}
	}
	return nil
}

func eventFromState(state projectState) Event {
	return Event{
		Project: state.Name, Status: state.LastStatus, Episode: state.Episode,
		ConsecutiveFailures: state.ConsecutiveFailures, ObservedAt: state.ObservedAt,
	}
}

func sortedStates(states map[string]projectState) []projectState {
	names := make([]string, 0, len(states))
	for name := range states {
		names = append(names, name)
	}
	sort.Strings(names)
	result := make([]projectState, 0, len(names))
	for _, name := range names {
		result = append(result, states[name])
	}
	return result
}

func read(path string) (document, error) {
	doc := document{SchemaVersion: SchemaVersion, Projects: []projectState{}}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return doc, nil
	}
	if err != nil {
		return document{}, fmt.Errorf("inspect notification state: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return document{}, errors.New("refusing to read notification state through a symlink")
	}
	if !info.Mode().IsRegular() {
		return document{}, errors.New("notification state path is not a regular file")
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return document{}, errors.New("notification state permissions must not allow group or other access")
	}
	if info.Size() > MaxFileBytes {
		return document{}, fmt.Errorf("notification state is too large: maximum is %d bytes", MaxFileBytes)
	}
	file, err := os.Open(path)
	if err != nil {
		return document{}, fmt.Errorf("open notification state: %w", err)
	}
	defer file.Close()
	openedInfo, err := file.Stat()
	if err != nil {
		return document{}, fmt.Errorf("inspect opened notification state: %w", err)
	}
	if !os.SameFile(info, openedInfo) {
		return document{}, errors.New("notification state changed while opening")
	}
	contents, err := io.ReadAll(io.LimitReader(file, MaxFileBytes+1))
	if err != nil {
		return document{}, fmt.Errorf("read notification state: %w", err)
	}
	if len(contents) > MaxFileBytes {
		return document{}, fmt.Errorf("notification state is too large: maximum is %d bytes", MaxFileBytes)
	}
	if err := decodeStrict(contents, &doc); err != nil || validateDocument(doc) != nil {
		return document{}, errors.New("invalid notification state content")
	}
	return doc, nil
}

func write(path string, doc document) error {
	if err := validateDocument(doc); err != nil {
		return errors.New("notification state contains invalid values")
	}
	encoded, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("encode notification state: %w", err)
	}
	encoded = append(encoded, '\n')
	if len(encoded) > MaxFileBytes {
		return fmt.Errorf("notification state exceeds %d bytes", MaxFileBytes)
	}
	if err := fileutil.WriteAtomic(path, encoded, 0o600, true); err != nil {
		return fmt.Errorf("write notification state atomically: %w", err)
	}
	return nil
}

func validateDocument(doc document) error {
	if doc.SchemaVersion != SchemaVersion || doc.Projects == nil || len(doc.Projects) > MaxProjects {
		return errors.New("invalid notification state document")
	}
	seen := make(map[string]struct{}, len(doc.Projects))
	previousName := ""
	for _, state := range doc.Projects {
		if !projectNamePattern.MatchString(state.Name) || !validStatus(state.LastStatus) || state.ObservedAt.IsZero() ||
			state.ConsecutiveFailures < 0 || (state.Pending && state.Notified) {
			return errors.New("invalid notification project state")
		}
		if state.LastStatus == heartbeat.Healthy {
			if state.ConsecutiveFailures != 0 || state.Pending || state.Notified {
				return errors.New("invalid healthy notification state")
			}
		} else if state.ConsecutiveFailures < 1 || state.Episode == 0 {
			return errors.New("invalid failure notification state")
		}
		if _, exists := seen[state.Name]; exists || (previousName != "" && state.Name < previousName) {
			return errors.New("duplicate or unsorted notification project state")
		}
		seen[state.Name] = struct{}{}
		previousName = state.Name
	}
	return nil
}

func decodeStrict(contents []byte, target any) error {
	if err := rejectDuplicateKeys(contents); err != nil {
		return err
	}
	if err := validateShape(contents); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return errors.New("trailing JSON")
	}
	return nil
}

func validateShape(contents []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return err
	}
	root, ok := value.(map[string]any)
	if !ok || !hasExactKeys(root, "schema_version", "projects") {
		return errors.New("invalid notification state root shape")
	}
	projects, ok := root["projects"].([]any)
	if !ok {
		return errors.New("invalid notification projects shape")
	}
	for _, value := range projects {
		project, ok := value.(map[string]any)
		if !ok || !hasExactKeys(project, "name", "episode", "consecutive_failures", "last_status", "observed_at", "pending", "notified") {
			return errors.New("invalid notification project shape")
		}
	}
	return nil
}

func hasExactKeys(object map[string]any, keys ...string) bool {
	if len(object) != len(keys) {
		return false
	}
	for _, key := range keys {
		if _, exists := object[key]; !exists {
			return false
		}
	}
	return true
}

func rejectDuplicateKeys(contents []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(contents))
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
		return errors.New("invalid JSON delimiter")
	}
	_, err = decoder.Token()
	return err
}

func validStatus(status heartbeat.Status) bool {
	switch status {
	case heartbeat.Healthy, heartbeat.Timeout, heartbeat.DNSFailure, heartbeat.TLSFailure,
		heartbeat.CredentialRejected, heartbeat.DatabasePermissionDenied,
		heartbeat.APIAuthenticationFailed, heartbeat.TemporaryUpstreamFailure,
		heartbeat.ProjectPaused, heartbeat.UnexpectedResponse, heartbeat.MissingInput,
		heartbeat.NoMatchingRow, heartbeat.ResponseTooLarge, heartbeat.InternalError:
		return true
	default:
		return false
	}
}
