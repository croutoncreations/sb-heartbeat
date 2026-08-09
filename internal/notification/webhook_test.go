package notification_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/croutoncreations/sb-heartbeat/internal/heartbeat"
	"github.com/croutoncreations/sb-heartbeat/internal/notification"
)

func TestWebhookDeliversExactSanitizedEvent(t *testing.T) {
	var received map[string]any
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.Header.Get("Content-Type") != "application/json" {
			t.Errorf("request method/header = %s/%q", request.Method, request.Header.Get("Content-Type"))
		}
		decoder := json.NewDecoder(request.Body)
		if err := decoder.Decode(&received); err != nil {
			t.Error(err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	event := notification.Event{
		Project: "stage", Status: heartbeat.Timeout, Episode: 4,
		ConsecutiveFailures: 3, ObservedAt: observedAt,
	}
	if err := notification.Deliver(context.Background(), server.Client(), server.URL+"?token=secret", event); err != nil {
		t.Fatal(err)
	}
	wantKeys := []string{"schema_version", "event", "project", "status", "episode", "consecutive_failures", "observed_at"}
	if len(received) != len(wantKeys) {
		t.Fatalf("payload = %#v", received)
	}
	for _, key := range wantKeys {
		if _, exists := received[key]; !exists {
			t.Errorf("payload missing %q: %#v", key, received)
		}
	}
	if received["event"] != "repeated_failure" || received["project"] != "stage" || received["status"] != "timeout" {
		t.Fatalf("payload = %#v", received)
	}
	encoded, _ := json.Marshal(received)
	if strings.Contains(string(encoded), "secret") || strings.Contains(string(encoded), "url") || strings.Contains(string(encoded), "message") {
		t.Fatalf("payload leaked non-event data: %s", encoded)
	}
}

func TestWebhookRejectsRedirectsAndDoesNotReachTarget(t *testing.T) {
	targetCalled := false
	target := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { targetCalled = true }))
	defer target.Close()
	redirect := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		http.Redirect(w, request, target.URL, http.StatusFound)
	}))
	defer redirect.Close()
	client := redirect.Client()
	client.CheckRedirect = nil
	err := notification.Deliver(context.Background(), client, redirect.URL, validEvent())
	if err == nil || !strings.Contains(err.Error(), "redirect") || targetCalled {
		t.Fatalf("redirect error=%v targetCalled=%v", err, targetCalled)
	}
}

func TestWebhookErrorsNeverEchoURLOrResponseBody(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("sb_secret_response_body"))
	}))
	defer server.Close()
	rawURL := server.URL + "/private-token"
	err := notification.Deliver(context.Background(), server.Client(), rawURL, validEvent())
	if err == nil || !strings.Contains(err.Error(), "502") || strings.Contains(err.Error(), "private-token") || strings.Contains(err.Error(), "sb_secret") {
		t.Fatalf("delivery error=%v", err)
	}
}

func TestWebhookValidatesDestinationAndEventBeforeNetwork(t *testing.T) {
	for name, rawURL := range map[string]string{
		"http":           "http://hooks.example.com/path",
		"userinfo":       "https://user:pass@hooks.example.com/path",
		"fragment":       "https://hooks.example.com/path#secret",
		"empty fragment": "https://hooks.example.com/path#",
		"missing host":   "https:///path",
		"empty hostname": "https://:443/path",
		"empty port":     "https://hooks.example.com:/path",
		"zero port":      "https://hooks.example.com:0/path",
		"large port":     "https://hooks.example.com:65536/path",
	} {
		t.Run(name, func(t *testing.T) {
			called := false
			client := &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
				called = true
				return nil, errors.New("network must not be reached")
			})}
			if err := notification.Deliver(context.Background(), client, rawURL, validEvent()); err == nil || strings.Contains(err.Error(), rawURL) || called {
				t.Fatalf("validation error=%v networkCalled=%v", err, called)
			}
		})
	}
	called := false
	client := &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		called = true
		return nil, errors.New("network must not be reached")
	})}
	invalid := validEvent()
	invalid.Project = "Secret URL"
	if err := notification.Deliver(context.Background(), client, "https://hooks.example.com/path", invalid); err == nil || called {
		t.Fatalf("invalid event accepted or reached network: error=%v networkCalled=%v", err, called)
	}
}

func TestWebhookHonorsContextAndReturnsSanitizedTransportError(t *testing.T) {
	client := &http.Client{Transport: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		<-request.Context().Done()
		return nil, request.Context().Err()
	})}
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	err := notification.Deliver(ctx, client, "https://hooks.example.com/private-token", validEvent())
	if err == nil || strings.Contains(err.Error(), "private-token") {
		t.Fatalf("transport error=%v", err)
	}
}

func TestWebhookCapsNonpositiveClientTimeout(t *testing.T) {
	var remaining time.Duration
	client := &http.Client{
		Timeout: -1,
		Transport: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
			deadline, ok := request.Context().Deadline()
			if !ok {
				t.Fatal("delivery request has no deadline")
			}
			remaining = time.Until(deadline)
			return &http.Response{
				StatusCode: http.StatusNoContent,
				Body:       io.NopCloser(strings.NewReader("")),
				Header:     make(http.Header),
			}, nil
		}),
	}
	if err := notification.Deliver(context.Background(), client, "https://hooks.example.com/path", validEvent()); err != nil {
		t.Fatal(err)
	}
	if remaining <= 0 || remaining > notification.DeliveryTimeout {
		t.Fatalf("delivery deadline remaining=%s", remaining)
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (function roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func validEvent() notification.Event {
	return notification.Event{
		Project: "stage", Status: heartbeat.Timeout, Episode: 1,
		ConsecutiveFailures: 3, ObservedAt: observedAt,
	}
}
