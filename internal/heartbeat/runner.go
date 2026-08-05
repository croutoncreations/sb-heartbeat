package heartbeat

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jfox85/sb-heartbeat/internal/credentials"
	clientfactory "github.com/jfox85/sb-heartbeat/internal/httpclient"
)

const maxResponseBytes = 64 * 1024

type Project struct {
	Name    string
	BaseURL *url.URL
	APIKey  string
}

type Options struct {
	Timeout      *time.Duration
	Retries      *int
	RetryBackoff *time.Duration
	Concurrency  *int
	Client       *http.Client
}

type Runner struct {
	timeout      time.Duration
	retries      int
	retryBackoff time.Duration
	concurrency  int
	client       *http.Client
}

func NewRunner(options Options) *Runner {
	runner := &Runner{
		timeout:      10 * time.Second,
		retries:      1,
		retryBackoff: 2 * time.Second,
		concurrency:  4,
		client:       options.Client,
	}
	if options.Timeout != nil {
		runner.timeout = *options.Timeout
	}
	if options.Retries != nil {
		runner.retries = clampInt(*options.Retries, 0, 3)
	}
	if options.RetryBackoff != nil {
		runner.retryBackoff = *options.RetryBackoff
	}
	if options.Concurrency != nil {
		runner.concurrency = clampInt(*options.Concurrency, 1, 16)
	}
	if runner.client == nil {
		runner.client = clientfactory.New()
	}
	return runner
}

func (r *Runner) RunAll(ctx context.Context, projects []Project) []Result {
	results := make([]Result, len(projects))
	if len(projects) == 0 {
		return results
	}
	workers := min(r.concurrency, len(projects))
	jobs := make(chan int)
	var wg sync.WaitGroup
	wg.Add(workers)
	for range workers {
		go func() {
			defer wg.Done()
			for index := range jobs {
				results[index] = r.Check(ctx, projects[index])
			}
		}()
	}
	for index := range projects {
		jobs <- index
	}
	close(jobs)
	wg.Wait()
	return results
}

func (r *Runner) Check(ctx context.Context, project Project) Result {
	headers, err := credentials.Headers(project.APIKey)
	if err != nil {
		return failed(project.Name, CredentialRejected, "API key is not a supported low-privilege key", nil, nil, 0)
	}
	requestURL, err := heartbeatURL(project.BaseURL)
	if err != nil {
		return failed(project.Name, InternalError, "could not construct heartbeat URL", nil, nil, 0)
	}

	started := time.Now()
	var attempts int
	for {
		attempts++
		attemptCtx, cancel := context.WithTimeout(ctx, r.timeout)
		req, requestErr := http.NewRequestWithContext(attemptCtx, http.MethodGet, requestURL.String(), nil)
		if requestErr != nil {
			cancel()
			return failed(project.Name, InternalError, "could not create heartbeat request", nil, elapsedMS(started), attempts)
		}
		req.Header = headers.Clone()
		response, requestErr := r.client.Do(req)
		if requestErr != nil {
			cancel()
			status, retry := classifyTransport(requestErr, ctx, project.BaseURL.Scheme == "https")
			if retry && attempts <= r.retries {
				if sleepContext(ctx, r.backoff(attempts)) == nil {
					continue
				}
				return failed(project.Name, Timeout, "request canceled during retry backoff", nil, elapsedMS(started), attempts)
			}
			return failed(project.Name, status, transportMessage(status), nil, elapsedMS(started), attempts)
		}

		body, tooLarge, readErr := readBounded(response.Body, maxResponseBytes)
		response.Body.Close()
		httpStatus := response.StatusCode
		if readErr != nil {
			status, retry := classifyTransport(readErr, attemptCtx, false)
			cancel()
			if retry && attempts <= r.retries {
				if sleepContext(ctx, r.backoff(attempts)) == nil {
					continue
				}
				return failed(project.Name, Timeout, "request canceled during retry backoff", &httpStatus, elapsedMS(started), attempts)
			}
			return failed(project.Name, status, transportMessage(status), &httpStatus, elapsedMS(started), attempts)
		}
		cancel()
		if tooLarge {
			return failed(project.Name, ResponseTooLarge, "response exceeded 64 KiB", &httpStatus, elapsedMS(started), attempts)
		}

		status, message, retry := classifyResponse(response, body)
		if retry && attempts <= r.retries {
			delay := r.backoff(attempts)
			if retryAfterDelay, ok := retryAfter(response.Header.Get("Retry-After"), time.Now()); ok {
				delay = retryAfterDelay
			}
			if sleepContext(ctx, delay) == nil {
				continue
			}
			return failed(project.Name, Timeout, "request canceled during retry backoff", &httpStatus, elapsedMS(started), attempts)
		}
		if status == Healthy {
			return Result{
				Name:       project.Name,
				Status:     Healthy,
				HTTPStatus: &httpStatus,
				LatencyMS:  elapsedMS(started),
				Attempts:   attempts,
			}
		}
		return failed(project.Name, status, message, &httpStatus, elapsedMS(started), attempts)
	}
}

func heartbeatURL(base *url.URL) (*url.URL, error) {
	if base == nil || base.Scheme == "" || base.Host == "" {
		return nil, errors.New("base URL is missing")
	}
	u := *base
	u.Path = "/rest/v1/sb_heartbeat"
	u.RawPath = ""
	u.RawQuery = url.Values{
		"select": {"id"},
		"id":     {"eq.true"},
		"limit":  {"1"},
	}.Encode()
	u.Fragment = ""
	return &u, nil
}

func classifyResponse(response *http.Response, body []byte) (Status, string, bool) {
	switch response.StatusCode {
	case 540:
		return ProjectPaused, "project appears to be paused", false
	case http.StatusRequestTimeout, 425, http.StatusTooManyRequests,
		http.StatusBadGateway, http.StatusServiceUnavailable,
		http.StatusGatewayTimeout, 544:
		return TemporaryUpstreamFailure, "temporary upstream failure", true
	case http.StatusUnauthorized, http.StatusForbidden:
		var upstream struct {
			Code string `json:"code"`
		}
		if json.Unmarshal(body, &upstream) == nil && upstream.Code == "42501" {
			return DatabasePermissionDenied, "database permission denied", false
		}
		return APIAuthenticationFailed, "API authentication failed", false
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return UnexpectedResponse, "unexpected HTTP response", false
	}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return UnexpectedResponse, "response is not application/json", false
	}
	var rows []struct {
		ID bool `json:"id"`
	}
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&rows); err != nil {
		return UnexpectedResponse, "response JSON has an unexpected shape", false
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return UnexpectedResponse, "response contains trailing JSON", false
	}
	if len(rows) == 0 {
		return NoMatchingRow, "heartbeat row was not returned", false
	}
	if len(rows) != 1 || !rows[0].ID {
		return UnexpectedResponse, "heartbeat response did not match the expected row", false
	}
	return Healthy, "", false
}

func classifyTransport(err error, parent context.Context, tlsExpected bool) (Status, bool) {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(parent.Err(), context.Canceled) || errors.Is(parent.Err(), context.DeadlineExceeded) {
		return Timeout, true
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return DNSFailure, true
	}
	var unknownAuthority x509.UnknownAuthorityError
	var hostnameError x509.HostnameError
	var invalidCertificate x509.CertificateInvalidError
	var verificationError *tls.CertificateVerificationError
	var recordHeaderError tls.RecordHeaderError
	if errors.As(err, &unknownAuthority) || errors.As(err, &hostnameError) || errors.As(err, &invalidCertificate) ||
		errors.As(err, &verificationError) || errors.As(err, &recordHeaderError) {
		return TLSFailure, false
	}
	if strings.Contains(err.Error(), "server gave HTTP response to HTTPS client") {
		return TLSFailure, false
	}
	if containsTLSError(err) {
		return TLSFailure, true
	}
	if tlsExpected && (errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF)) {
		return TLSFailure, true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return Timeout, true
	}
	return TemporaryUpstreamFailure, true
}

func containsTLSError(err error) bool {
	for current := err; current != nil; current = errors.Unwrap(current) {
		typeOfError := reflect.TypeOf(current)
		if typeOfError == nil {
			continue
		}
		if typeOfError.Kind() == reflect.Pointer {
			typeOfError = typeOfError.Elem()
		}
		if typeOfError.PkgPath() == "crypto/tls" {
			return true
		}
	}
	return false
}

func readBounded(body io.Reader, maximum int64) ([]byte, bool, error) {
	data, err := io.ReadAll(io.LimitReader(body, maximum+1))
	if err != nil {
		return nil, false, err
	}
	if int64(len(data)) > maximum {
		return nil, true, nil
	}
	return data, false, nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("additional JSON value")
		}
		return err
	}
	return nil
}

func retryAfter(raw string, now time.Time) (time.Duration, bool) {
	if seconds, err := strconv.Atoi(strings.TrimSpace(raw)); err == nil && seconds >= 0 {
		return min(time.Duration(seconds)*time.Second, 30*time.Second), true
	}
	if date, err := http.ParseTime(raw); err == nil && date.After(now) {
		return min(date.Sub(now), 30*time.Second), true
	}
	return 0, false
}

func (r *Runner) backoff(attempt int) time.Duration {
	delay := r.retryBackoff * time.Duration(1<<max(0, attempt-1))
	return min(delay, 30*time.Second)
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func failed(name string, status Status, message string, httpStatus *int, latency *int64, attempts int) Result {
	return Result{
		Name:       name,
		Status:     status,
		HTTPStatus: httpStatus,
		LatencyMS:  latency,
		Attempts:   attempts,
		Error:      &Error{Code: status, Message: message},
	}
}

func elapsedMS(started time.Time) *int64 {
	value := time.Since(started).Milliseconds()
	return &value
}

func transportMessage(status Status) string {
	switch status {
	case Timeout:
		return "request timed out or was canceled"
	case DNSFailure:
		return "DNS lookup failed"
	case TLSFailure:
		return "TLS verification or negotiation failed"
	default:
		return "temporary network failure"
	}
}

func clampInt(value, low, high int) int {
	return max(low, min(value, high))
}
