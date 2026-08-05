package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
	"gopkg.in/yaml.v3"
)

const (
	DefaultCron        = "37 3,11,19 * * *"
	DefaultOutput      = "text"
	DefaultRetries     = 1
	DefaultConcurrency = 4
)

var (
	projectNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,62}$`)
	envNamePattern     = regexp.MustCompile(`^[A-Z_][A-Z0-9_]{0,126}$`)
)

type Config struct {
	Version   int
	Defaults  Defaults
	Scheduler Scheduler
	Projects  []Project
}

type Defaults struct {
	Timeout      time.Duration
	Retries      int
	RetryBackoff time.Duration
	Concurrency  int
	Output       string
}

type Scheduler struct {
	Cron string
}

type Project struct {
	Name   string
	URL    EnvReference
	APIKey EnvReference
}

type EnvReference struct {
	Env string
}

type wireConfig struct {
	Version   int           `yaml:"version"`
	Defaults  wireDefaults  `yaml:"defaults"`
	Scheduler wireScheduler `yaml:"scheduler"`
	Projects  []wireProject `yaml:"projects"`
}

type wireDefaults struct {
	Timeout      string `yaml:"timeout"`
	Retries      *int   `yaml:"retries"`
	RetryBackoff string `yaml:"retry_backoff"`
	Concurrency  *int   `yaml:"concurrency"`
	Output       string `yaml:"output"`
}

type wireScheduler struct {
	Cron string `yaml:"cron"`
}

type wireProject struct {
	Name   string       `yaml:"name"`
	URL    EnvReference `yaml:"url"`
	APIKey EnvReference `yaml:"api_key"`
}

func Load(r io.Reader) (Config, error) {
	decoder := yaml.NewDecoder(r)
	decoder.KnownFields(true)

	var wire wireConfig
	if err := decoder.Decode(&wire); err != nil {
		return Config{}, fmt.Errorf("decode configuration: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return Config{}, errors.New("configuration contains multiple YAML documents")
		}
		return Config{}, fmt.Errorf("decode configuration: %w", err)
	}

	cfg, err := fromWire(wire)
	if err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func New(project Project, cron string) (Config, error) {
	if cron == "" {
		cron = DefaultCron
	}
	cfg := Config{
		Version: 1,
		Defaults: Defaults{
			Timeout:      10 * time.Second,
			Retries:      DefaultRetries,
			RetryBackoff: 2 * time.Second,
			Concurrency:  DefaultConcurrency,
			Output:       DefaultOutput,
		},
		Scheduler: Scheduler{Cron: cron},
		Projects:  []Project{project},
	}
	return cfg, cfg.Validate()
}

func Marshal(cfg Config) ([]byte, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	retries, concurrency := cfg.Defaults.Retries, cfg.Defaults.Concurrency
	wire := wireConfig{
		Version: cfg.Version,
		Defaults: wireDefaults{
			Timeout:      cfg.Defaults.Timeout.String(),
			Retries:      &retries,
			RetryBackoff: cfg.Defaults.RetryBackoff.String(),
			Concurrency:  &concurrency,
			Output:       cfg.Defaults.Output,
		},
		Scheduler: wireScheduler{Cron: cfg.Scheduler.Cron},
	}
	for _, project := range cfg.Projects {
		wire.Projects = append(wire.Projects, wireProject(project))
	}
	var buffer bytes.Buffer
	encoder := yaml.NewEncoder(&buffer)
	encoder.SetIndent(2)
	if err := encoder.Encode(wire); err != nil {
		return nil, fmt.Errorf("encode configuration: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return nil, fmt.Errorf("encode configuration: %w", err)
	}
	return buffer.Bytes(), nil
}

func fromWire(w wireConfig) (Config, error) {
	cfg := Config{
		Version: w.Version,
		Defaults: Defaults{
			Timeout:      10 * time.Second,
			Retries:      DefaultRetries,
			RetryBackoff: 2 * time.Second,
			Concurrency:  DefaultConcurrency,
			Output:       DefaultOutput,
		},
		Scheduler: Scheduler{Cron: DefaultCron},
	}
	if w.Defaults.Timeout != "" {
		d, err := time.ParseDuration(w.Defaults.Timeout)
		if err != nil {
			return Config{}, fmt.Errorf("defaults.timeout: %w", err)
		}
		cfg.Defaults.Timeout = d
	}
	if w.Defaults.Retries != nil {
		cfg.Defaults.Retries = *w.Defaults.Retries
	}
	if w.Defaults.RetryBackoff != "" {
		d, err := time.ParseDuration(w.Defaults.RetryBackoff)
		if err != nil {
			return Config{}, fmt.Errorf("defaults.retry_backoff: %w", err)
		}
		cfg.Defaults.RetryBackoff = d
	}
	if w.Defaults.Concurrency != nil {
		cfg.Defaults.Concurrency = *w.Defaults.Concurrency
	}
	if w.Defaults.Output != "" {
		cfg.Defaults.Output = w.Defaults.Output
	}
	if w.Scheduler.Cron != "" {
		cfg.Scheduler.Cron = w.Scheduler.Cron
	}
	for _, p := range w.Projects {
		cfg.Projects = append(cfg.Projects, Project(p))
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) Validate() error {
	var problems []string
	if c.Version != 1 {
		problems = append(problems, "version must be 1")
	}
	if c.Defaults.Timeout < time.Second || c.Defaults.Timeout > time.Minute {
		problems = append(problems, "defaults.timeout must be between 1s and 60s")
	}
	if c.Defaults.Retries < 0 || c.Defaults.Retries > 3 {
		problems = append(problems, "defaults.retries must be between 0 and 3")
	}
	if c.Defaults.RetryBackoff < 100*time.Millisecond || c.Defaults.RetryBackoff > 30*time.Second {
		problems = append(problems, "defaults.retry_backoff must be between 100ms and 30s")
	}
	if c.Defaults.Concurrency < 1 || c.Defaults.Concurrency > 16 {
		problems = append(problems, "defaults.concurrency must be between 1 and 16")
	}
	if c.Defaults.Output != "text" && c.Defaults.Output != "json" {
		problems = append(problems, "defaults.output must be text or json")
	}
	if strings.TrimSpace(c.Scheduler.Cron) == "" {
		problems = append(problems, "scheduler.cron must not be empty")
	} else if len(strings.Fields(c.Scheduler.Cron)) != 5 {
		problems = append(problems, "scheduler.cron must be a valid five-field POSIX cron expression")
	} else if _, err := cron.ParseStandard(c.Scheduler.Cron); err != nil {
		problems = append(problems, "scheduler.cron must be a valid five-field POSIX cron expression")
	}
	if len(c.Projects) == 0 {
		problems = append(problems, "at least one project is required")
	}

	names := make(map[string]struct{}, len(c.Projects))
	bindings := make(map[string]string, len(c.Projects)*2)
	for i, p := range c.Projects {
		prefix := fmt.Sprintf("projects[%d]", i)
		if !projectNamePattern.MatchString(p.Name) {
			problems = append(problems, prefix+".name is invalid")
		} else if _, exists := names[p.Name]; exists {
			problems = append(problems, prefix+".name is duplicated")
		}
		names[p.Name] = struct{}{}
		bindingList := []struct {
			label string
			env   string
		}{
			{label: "url.env", env: p.URL.Env},
			{label: "api_key.env", env: p.APIKey.Env},
		}
		for _, binding := range bindingList {
			label, env := binding.label, binding.env
			if !envNamePattern.MatchString(env) {
				problems = append(problems, prefix+"."+label+" is invalid")
				continue
			}
			if previous, exists := bindings[env]; exists {
				problems = append(problems, prefix+"."+label+" duplicates "+previous)
			} else {
				bindings[env] = prefix + "." + label
			}
		}
	}
	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "; "))
	}
	return nil
}
