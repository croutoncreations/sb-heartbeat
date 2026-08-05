package credentials_test

import (
	"encoding/base64"
	"fmt"
	"testing"

	"github.com/jfox85/supawake/internal/credentials"
)

func jwt(role string) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(`{"role":%q}`, role)))
	return header + "." + payload + ".signature"
}

func TestClassifySupportedKeys(t *testing.T) {
	tests := []struct {
		name string
		key  string
		want credentials.Kind
	}{
		{"publishable", "sb_publishable_abcdefghijklmnopqrstuv_12345678", credentials.Publishable},
		{"legacy anon", jwt("anon"), credentials.LegacyAnon},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := credentials.Classify(tt.key)
			if err != nil || got != tt.want {
				t.Fatalf("Classify() = %v, %v; want %v", got, err, tt.want)
			}
		})
	}
}

func TestClassifyRejectsElevatedAndMalformedKeys(t *testing.T) {
	tests := []string{
		"sb_secret_review_fixture_not_a_real_key",
		jwt("service_role"),
		"not-a-key",
		"",
	}
	for _, key := range tests {
		if _, err := credentials.Classify(key); err == nil {
			t.Fatalf("Classify(%q) error = nil, want rejection", key)
		}
	}
}

func TestHeadersDependOnKeyKind(t *testing.T) {
	pub := "sb_publishable_abcdefghijklmnopqrstuv_12345678"
	headers, err := credentials.Headers(pub)
	if err != nil {
		t.Fatal(err)
	}
	if headers.Get("apikey") != pub || headers.Get("Authorization") != "" {
		t.Fatalf("publishable headers = %v", headers)
	}

	anon := jwt("anon")
	headers, err = credentials.Headers(anon)
	if err != nil {
		t.Fatal(err)
	}
	if headers.Get("apikey") != anon || headers.Get("Authorization") != "Bearer "+anon {
		t.Fatalf("anon headers = %v", headers)
	}
}

func TestRedactNeverReturnsCompleteKey(t *testing.T) {
	key := "sb_publishable_abcdefghijklmnopqrstuv_12345678"
	got := credentials.Redact(key)
	if got == key || got == "" {
		t.Fatalf("Redact() = %q", got)
	}
}
