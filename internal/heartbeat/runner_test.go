package heartbeat

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const publishableKey = "sb_publishable_abcdefghijklmnopqrstuv_12345678"

func projectFor(t *testing.T, name, rawURL string) Project {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	return Project{Name: name, BaseURL: u, APIKey: publishableKey}
}

func healthyServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/v1/sb_heartbeat" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if got := r.URL.Query().Get("select"); got != "id" {
			t.Errorf("select = %q", got)
		}
		if got := r.URL.Query().Get("id"); got != "eq.true" {
			t.Errorf("id filter = %q", got)
		}
		if got := r.URL.Query().Get("limit"); got != "1" {
			t.Errorf("limit = %q", got)
		}
		if r.Header.Get("apikey") != publishableKey || r.Header.Get("Authorization") != "" {
			t.Errorf("credential headers = %v", r.Header)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[{"id":true}]`)
	}))
}

func TestCheckHealthyFixedHeartbeat(t *testing.T) {
	server := healthyServer(t)
	defer server.Close()

	result := NewRunner(Options{}).Check(context.Background(), projectFor(t, "demo", server.URL))
	if result.Status != Healthy || result.Attempts != 1 || result.HTTPStatus == nil || *result.HTTPStatus != 200 {
		t.Fatalf("result = %+v", result)
	}
	if result.LatencyMS == nil || result.Error != nil {
		t.Fatalf("result = %+v", result)
	}
}

func TestCheckResponseClassifications(t *testing.T) {
	tests := []struct {
		name        string
		status      int
		contentType string
		body        string
		want        Status
	}{
		{"paused", 540, "application/json", `{}`, ProjectPaused},
		{"permission", 401, "application/json", `{"code":"42501","message":"denied"}`, DatabasePermissionDenied},
		{"authentication", 401, "application/json", `{"message":"bad key"}`, APIAuthenticationFailed},
		{"no row", 200, "application/json", `[]`, NoMatchingRow},
		{"many rows", 200, "application/json", `[{"id":true},{"id":true}]`, UnexpectedResponse},
		{"wrong row", 200, "application/json", `[{"id":false}]`, UnexpectedResponse},
		{"extra field", 200, "application/json", `[{"id":true,"secret":"x"}]`, UnexpectedResponse},
		{"invalid json", 200, "application/json", `{`, UnexpectedResponse},
		{"wrong content type", 200, "text/plain", `[{"id":true}]`, UnexpectedResponse},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", tt.contentType)
				w.WriteHeader(tt.status)
				fmt.Fprint(w, tt.body)
			}))
			defer server.Close()

			result := NewRunner(Options{Retries: intPointer(0)}).Check(context.Background(), projectFor(t, "demo", server.URL))
			if result.Status != tt.want {
				t.Fatalf("status = %q, want %q; result=%+v", result.Status, tt.want, result)
			}
			if result.Error == nil && tt.want != Healthy {
				t.Fatal("error detail = nil")
			}
		})
	}
}

func TestCheckRejectsOversizedBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, strings.Repeat("x", 65*1024))
	}))
	defer server.Close()

	result := NewRunner(Options{}).Check(context.Background(), projectFor(t, "demo", server.URL))
	if result.Status != ResponseTooLarge {
		t.Fatalf("status = %q", result.Status)
	}
}

func TestCheckNeverFollowsRedirect(t *testing.T) {
	var targetCalls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		targetCalls.Add(1)
	}))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer source.Close()

	result := NewRunner(Options{}).Check(context.Background(), projectFor(t, "demo", source.URL))
	if result.Status != UnexpectedResponse || targetCalls.Load() != 0 {
		t.Fatalf("result = %+v, target calls = %d", result, targetCalls.Load())
	}
}

func TestCheckRetriesTemporaryStatus(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[{"id":true}]`)
	}))
	defer server.Close()

	runner := NewRunner(Options{RetryBackoff: durationPointer(time.Millisecond)})
	result := runner.Check(context.Background(), projectFor(t, "demo", server.URL))
	if result.Status != Healthy || result.Attempts != 2 || calls.Load() != 2 {
		t.Fatalf("result = %+v, calls = %d", result, calls.Load())
	}
}

func TestCheckTimeoutRetriesThenClassifies(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(50 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[{"id":true}]`)
	}))
	defer server.Close()

	runner := NewRunner(Options{Timeout: durationPointer(5 * time.Millisecond), RetryBackoff: durationPointer(time.Millisecond)})
	result := runner.Check(context.Background(), projectFor(t, "demo", server.URL))
	if result.Status != Timeout || result.Attempts != 2 {
		t.Fatalf("result = %+v", result)
	}
}

func TestCheckResponseBodyTimeoutRetriesAndPreservesStatus(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()

	runner := NewRunner(Options{Timeout: durationPointer(10 * time.Millisecond), RetryBackoff: durationPointer(time.Millisecond)})
	result := runner.Check(context.Background(), projectFor(t, "demo", server.URL))
	if result.Status != Timeout || result.Attempts != 2 || calls.Load() != 2 {
		t.Fatalf("result = %+v, calls = %d", result, calls.Load())
	}
	if result.HTTPStatus == nil || *result.HTTPStatus != http.StatusOK {
		t.Fatalf("HTTP status = %v", result.HTTPStatus)
	}
}

func TestCheckTruncatedBodyRetries(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if calls.Add(1) == 1 {
			w.Header().Set("Content-Length", "100")
			fmt.Fprint(w, `[{"id":`)
			return
		}
		fmt.Fprint(w, `[{"id":true}]`)
	}))
	defer server.Close()

	runner := NewRunner(Options{RetryBackoff: durationPointer(time.Millisecond)})
	result := runner.Check(context.Background(), projectFor(t, "demo", server.URL))
	if result.Status != Healthy || result.Attempts != 2 || calls.Load() != 2 {
		t.Fatalf("result = %+v, calls = %d", result, calls.Load())
	}
}

func TestCheckRejectsCredentialBeforeNetwork(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		calls.Add(1)
	}))
	defer server.Close()
	p := projectFor(t, "demo", server.URL)
	p.APIKey = "sb_secret_do_not_use"

	result := NewRunner(Options{}).Check(context.Background(), p)
	if result.Status != CredentialRejected || result.Attempts != 0 || result.LatencyMS != nil || calls.Load() != 0 {
		t.Fatalf("result = %+v, calls = %d", result, calls.Load())
	}
}

func TestRunAllPreservesOrderAndBoundsConcurrency(t *testing.T) {
	var active atomic.Int32
	var maximum atomic.Int32
	servers := make([]*httptest.Server, 5)
	projects := make([]Project, 5)
	for i := range servers {
		servers[i] = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			current := active.Add(1)
			defer active.Add(-1)
			for {
				old := maximum.Load()
				if current <= old || maximum.CompareAndSwap(old, current) {
					break
				}
			}
			time.Sleep(10 * time.Millisecond)
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `[{"id":true}]`)
		}))
		defer servers[i].Close()
		projects[i] = projectFor(t, fmt.Sprintf("p%d", i), servers[i].URL)
	}

	results := NewRunner(Options{Concurrency: intPointer(2)}).RunAll(context.Background(), projects)
	if maximum.Load() > 2 {
		t.Fatalf("maximum concurrency = %d", maximum.Load())
	}
	for i, result := range results {
		if result.Name != projects[i].Name || result.Status != Healthy {
			t.Fatalf("results[%d] = %+v", i, result)
		}
	}
}

func TestCheckHonorsCancellationDuringBackoff(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	runner := NewRunner(Options{RetryBackoff: durationPointer(time.Hour)})
	project := projectFor(t, "demo", server.URL)
	done := make(chan Result, 1)
	go func() { done <- runner.Check(ctx, project) }()
	time.Sleep(10 * time.Millisecond)
	cancel()

	select {
	case result := <-done:
		if result.Status != Timeout || result.Attempts != 1 {
			t.Fatalf("result = %+v", result)
		}
	case <-time.After(time.Second):
		t.Fatal("Check did not stop after cancellation")
	}
}

func TestCheckDoesNotRetryPausedResponse(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(540)
	}))
	defer server.Close()

	result := NewRunner(Options{}).Check(context.Background(), projectFor(t, "demo", server.URL))
	if result.Status != ProjectPaused || result.Attempts != 1 || calls.Load() != 1 {
		t.Fatalf("result = %+v, calls = %d", result, calls.Load())
	}
}

func TestCheckClassifiesCertificateFailureWithoutRetry(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[{"id":true}]`)
	}))
	defer server.Close()

	result := NewRunner(Options{}).Check(context.Background(), projectFor(t, "demo", server.URL))
	if result.Status != TLSFailure || result.Attempts != 1 {
		t.Fatalf("result = %+v", result)
	}
}

func TestCheckClassifiesInvalidTLSRecordWithoutRetry(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()
	project := projectFor(t, "demo", server.URL)
	project.BaseURL.Scheme = "https"

	result := NewRunner(Options{}).Check(context.Background(), project)
	if result.Status != TLSFailure || result.Attempts != 1 {
		t.Fatalf("result = %+v", result)
	}
}

func TestCheckRetriesTransientTLSHandshakeFailure(t *testing.T) {
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	server.TLS = &tls.Config{ClientAuth: tls.RequireAnyClientCert}
	server.StartTLS()
	defer server.Close()

	runner := NewRunner(Options{Client: server.Client(), RetryBackoff: durationPointer(time.Millisecond)})
	result := runner.Check(context.Background(), projectFor(t, "demo", server.URL))
	if result.Status != TLSFailure || result.Attempts != 2 {
		t.Fatalf("result = %+v", result)
	}
}

func TestCheckClassifiesHandshakeEOFAsTLSFailure(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 2 {
			connection, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			connection.Close()
		}
	}()

	runner := NewRunner(Options{RetryBackoff: durationPointer(time.Millisecond)})
	result := runner.Check(context.Background(), projectFor(t, "demo", "https://"+listener.Addr().String()))
	if result.Status != TLSFailure || result.Attempts != 2 {
		t.Fatalf("result = %+v", result)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("TLS fixture did not receive both attempts")
	}
}

func TestRetryAfterParsing(t *testing.T) {
	if delay, ok := retryAfter("0", time.Now()); !ok || delay != 0 {
		t.Fatalf("retryAfter(0) = %s, %v", delay, ok)
	}
	if delay, ok := retryAfter("999", time.Now()); !ok || delay != 30*time.Second {
		t.Fatalf("retryAfter(999) = %s, %v", delay, ok)
	}
	if _, ok := retryAfter("nonsense", time.Now()); ok {
		t.Fatal("retryAfter(nonsense) accepted")
	}
}

func intPointer(value int) *int                          { return &value }
func durationPointer(value time.Duration) *time.Duration { return &value }
