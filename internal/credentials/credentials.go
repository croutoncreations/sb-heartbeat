package credentials

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
)

type Kind string

const (
	Publishable Kind = "publishable"
	LegacyAnon  Kind = "legacy_anon"
)

var publishablePattern = regexp.MustCompile(`^sb_publishable_[A-Za-z0-9_-]+$`)

func Classify(key string) (Kind, error) {
	if strings.HasPrefix(key, "sb_secret_") {
		return "", errors.New("elevated Supabase secret keys are not supported")
	}
	if publishablePattern.MatchString(key) {
		return Publishable, nil
	}

	parts := strings.Split(key, ".")
	if len(parts) != 3 {
		return "", errors.New("API key is not a supported publishable or legacy anon key")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", errors.New("API key is not a valid legacy JWT")
	}
	var claims struct {
		Role string `json:"role"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return "", errors.New("API key is not a valid legacy JWT")
	}
	if claims.Role == "service_role" {
		return "", errors.New("elevated Supabase service-role keys are not supported")
	}
	if claims.Role != "anon" {
		return "", errors.New("legacy JWT does not have the anon role")
	}
	return LegacyAnon, nil
}

func Headers(key string) (http.Header, error) {
	kind, err := Classify(key)
	if err != nil {
		return nil, err
	}
	headers := make(http.Header)
	headers.Set("apikey", key)
	headers.Set("Accept", "application/json")
	if kind == LegacyAnon {
		headers.Set("Authorization", "Bearer "+key)
	}
	return headers, nil
}

func Redact(key string) string {
	if key == "" {
		return "[empty]"
	}
	const visible = 8
	if len(key) <= visible {
		return "[redacted]"
	}
	return key[:visible] + "…[redacted]"
}
