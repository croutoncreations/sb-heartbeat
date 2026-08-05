package config_test

import (
	"strings"
	"testing"
	"time"

	"github.com/jfox85/sb-heartbeat/internal/config"
)

const validConfig = `
version: 1
defaults:
  timeout: 10s
  retries: 1
  retry_backoff: 2s
  concurrency: 4
  output: text
scheduler:
  cron: "37 3,11,19 * * *"
projects:
  - name: demo
    url:
      env: DEMO_SUPABASE_URL
    api_key:
      env: DEMO_SUPABASE_API_KEY
`

func TestLoadAppliesAndPreservesRuntimeSettings(t *testing.T) {
	cfg, err := config.Load(strings.NewReader(validConfig))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Defaults.Timeout != 10*time.Second {
		t.Fatalf("timeout = %s, want 10s", cfg.Defaults.Timeout)
	}
	if cfg.Defaults.Retries != 1 || cfg.Defaults.Concurrency != 4 {
		t.Fatalf("defaults = %+v", cfg.Defaults)
	}
	if got := cfg.Projects[0].APIKey.Env; got != "DEMO_SUPABASE_API_KEY" {
		t.Fatalf("api key env = %q", got)
	}
}

func TestLoadSuppliesDocumentedDefaults(t *testing.T) {
	cfg, err := config.Load(strings.NewReader(`
version: 1
projects:
  - name: demo
    url: {env: DEMO_URL}
    api_key: {env: DEMO_KEY}
`))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Defaults.Timeout != 10*time.Second || cfg.Defaults.Retries != 1 ||
		cfg.Defaults.RetryBackoff != 2*time.Second || cfg.Defaults.Concurrency != 4 {
		t.Fatalf("defaults = %+v", cfg.Defaults)
	}
	if cfg.Scheduler.Cron != "37 3,11,19 * * *" {
		t.Fatalf("cron = %q", cfg.Scheduler.Cron)
	}
}

func TestLoadRejectsUnknownAndDuplicateKeys(t *testing.T) {
	tests := map[string]string{
		"unknown":   strings.Replace(validConfig, "version: 1", "version: 1\nmystery: true", 1),
		"duplicate": strings.Replace(validConfig, "version: 1", "version: 1\nversion: 1", 1),
	}
	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := config.Load(strings.NewReader(input)); err == nil {
				t.Fatal("Load() error = nil, want rejection")
			}
		})
	}
}

func TestLoadRejectsInvalidNamesBindingsAndBounds(t *testing.T) {
	tests := map[string]string{
		"project name":      strings.Replace(validConfig, "name: demo", "name: Demo!", 1),
		"environment":       strings.Replace(validConfig, "DEMO_SUPABASE_URL", "demo-url", 1),
		"duplicate binding": strings.Replace(validConfig, "DEMO_SUPABASE_API_KEY", "DEMO_SUPABASE_URL", 1),
		"timeout":           strings.Replace(validConfig, "timeout: 10s", "timeout: 61s", 1),
		"retries":           strings.Replace(validConfig, "retries: 1", "retries: 4", 1),
		"concurrency":       strings.Replace(validConfig, "concurrency: 4", "concurrency: 17", 1),
		"cron":              strings.Replace(validConfig, `cron: "37 3,11,19 * * *"`, `cron: "not a cron"`, 1),
		"cron descriptor":   strings.Replace(validConfig, `cron: "37 3,11,19 * * *"`, `cron: "@daily"`, 1),
	}
	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := config.Load(strings.NewReader(input)); err == nil {
				t.Fatal("Load() error = nil, want rejection")
			}
		})
	}
}

func TestLoadRejectsGenericQuerySurface(t *testing.T) {
	input := strings.Replace(validConfig, "    api_key:\n      env: DEMO_SUPABASE_API_KEY", "    api_key:\n      env: DEMO_SUPABASE_API_KEY\n    query:\n      table: users", 1)
	if _, err := config.Load(strings.NewReader(input)); err == nil {
		t.Fatal("Load() error = nil, want fixed-query enforcement")
	}
}

func TestValidationErrorsAreDeterministic(t *testing.T) {
	input := strings.Replace(validConfig, "DEMO_SUPABASE_API_KEY", "DEMO_SUPABASE_URL", 1)
	var first string
	for i := 0; i < 20; i++ {
		_, err := config.Load(strings.NewReader(input))
		if err == nil {
			t.Fatal("Load() error = nil")
		}
		if first == "" {
			first = err.Error()
		} else if err.Error() != first {
			t.Fatalf("validation error changed: %q != %q", err, first)
		}
	}
}
