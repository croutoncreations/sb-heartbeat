package notification

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/croutoncreations/sb-heartbeat/internal/heartbeat"
)

const (
	MaxWebhookURLBytes = 2048
	DeliveryTimeout    = 10 * time.Second
)

var errRedirectRejected = errors.New("notification webhook redirects are not allowed")

type webhookPayload struct {
	SchemaVersion       int              `json:"schema_version"`
	Event               string           `json:"event"`
	Project             string           `json:"project"`
	Status              heartbeat.Status `json:"status"`
	Episode             uint64           `json:"episode"`
	ConsecutiveFailures int              `json:"consecutive_failures"`
	ObservedAt          time.Time        `json:"observed_at"`
}

func Deliver(ctx context.Context, client *http.Client, rawURL string, event Event) error {
	if err := ValidateWebhookURL(rawURL); err != nil {
		return err
	}
	if err := validateEvent(event); err != nil {
		return err
	}
	payload, err := json.Marshal(webhookPayload{
		SchemaVersion: 1, Event: "repeated_failure", Project: event.Project,
		Status: event.Status, Episode: event.Episode,
		ConsecutiveFailures: event.ConsecutiveFailures, ObservedAt: event.ObservedAt.UTC(),
	})
	if err != nil {
		return errors.New("encode notification webhook event")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, bytes.NewReader(payload))
	if err != nil {
		return errors.New("create notification webhook request")
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "sb-heartbeat-notification/1")

	deliveryClient := http.Client{}
	if client != nil {
		deliveryClient = *client
	}
	if deliveryClient.Timeout <= 0 || deliveryClient.Timeout > DeliveryTimeout {
		deliveryClient.Timeout = DeliveryTimeout
	}
	deliveryClient.CheckRedirect = func(*http.Request, []*http.Request) error {
		return errRedirectRejected
	}
	response, err := deliveryClient.Do(request)
	if err != nil {
		if errors.Is(err, errRedirectRejected) {
			return errRedirectRejected
		}
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return errors.New("notification webhook delivery timed out")
		}
		return errors.New("notification webhook delivery failed")
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("notification webhook returned HTTP status %d", response.StatusCode)
	}
	return nil
}

func ValidateWebhookURL(rawURL string) error {
	if rawURL == "" || len(rawURL) > MaxWebhookURLBytes || strings.TrimSpace(rawURL) != rawURL || strings.ContainsAny(rawURL, "\r\n") {
		return errors.New("notification webhook URL is invalid")
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.Hostname() == "" || parsed.User != nil || strings.Contains(rawURL, "#") || parsed.Opaque != "" || strings.HasSuffix(parsed.Host, ":") {
		return errors.New("notification webhook URL must be an absolute HTTPS URL without user information or a fragment")
	}
	if port := parsed.Port(); port != "" {
		number, err := strconv.Atoi(port)
		if err != nil || number < 1 || number > 65535 {
			return errors.New("notification webhook URL contains an invalid port")
		}
	}
	return nil
}

func validateEvent(event Event) error {
	if !projectNamePattern.MatchString(event.Project) || !validStatus(event.Status) || event.Status == heartbeat.Healthy ||
		event.Episode == 0 || event.ConsecutiveFailures < 1 || event.ObservedAt.IsZero() {
		return errors.New("notification event contains invalid values")
	}
	return nil
}
