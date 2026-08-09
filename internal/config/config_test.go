package config_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/croutoncreations/sb-heartbeat/internal/config"
	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
	"gopkg.in/yaml.v3"
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

func TestLoadDerivesEnvironmentBindingsFromProjectName(t *testing.T) {
	cfg, err := config.Load(strings.NewReader(`
version: 1
projects:
  - name: my-stage
`))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got := cfg.Projects[0].URL.Env; got != "SB_HEARTBEAT_MY_STAGE_URL" {
		t.Fatalf("URL env = %q", got)
	}
	if got := cfg.Projects[0].APIKey.Env; got != "SB_HEARTBEAT_MY_STAGE_API_KEY" {
		t.Fatalf("API key env = %q", got)
	}
}

func TestLoadPreservesExplicitEnvironmentBindings(t *testing.T) {
	cfg, err := config.Load(strings.NewReader(`
version: 1
projects:
  - name: demo
    url: {env: CUSTOM_URL}
    api_key: {env: CUSTOM_KEY}
`))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Projects[0].URL.Env != "CUSTOM_URL" || cfg.Projects[0].APIKey.Env != "CUSTOM_KEY" {
		t.Fatalf("bindings = %+v", cfg.Projects[0])
	}
}

func TestLoadDefaultsAndPreservesGitHubBindingSources(t *testing.T) {
	defaulted, err := config.Load(strings.NewReader(`
version: 1
projects:
  - name: demo
`))
	if err != nil {
		t.Fatalf("Load(defaulted) error = %v", err)
	}
	if defaulted.Projects[0].URL.GitHub != "variable" || defaulted.Projects[0].APIKey.GitHub != "secret" {
		t.Fatalf("default GitHub sources = %+v", defaulted.Projects[0])
	}

	explicit, err := config.Load(strings.NewReader(`
version: 1
projects:
  - name: demo
    url: {env: CUSTOM_URL, github: secret}
    api_key: {env: CUSTOM_KEY, github: variable}
`))
	if err != nil {
		t.Fatalf("Load(explicit) error = %v", err)
	}
	if explicit.Projects[0].URL.GitHub != "secret" || explicit.Projects[0].APIKey.GitHub != "variable" {
		t.Fatalf("explicit GitHub sources = %+v", explicit.Projects[0])
	}
}

func TestNewProjectsAppliesImplicitBindingsToEveryProject(t *testing.T) {
	cfg, err := config.NewProjects([]config.Project{{Name: "first"}, {Name: "second-project"}}, "")
	if err != nil {
		t.Fatalf("NewProjects() error = %v", err)
	}
	if len(cfg.Projects) != 2 || cfg.Projects[1].APIKey.Env != "SB_HEARTBEAT_SECOND_PROJECT_API_KEY" {
		t.Fatalf("projects = %+v", cfg.Projects)
	}
}

func TestMarshalIncludesPublishedSchemaDirective(t *testing.T) {
	cfg, err := config.New(config.Project{Name: "demo"}, "")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	encoded, err := config.Marshal(cfg)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	directive := "# yaml-language-server: $schema=" + config.SchemaURL + "\n"
	if !strings.HasPrefix(string(encoded), directive) {
		t.Fatalf("Marshal() prefix = %q, want %q", strings.SplitN(string(encoded), "\n", 2)[0], strings.TrimSpace(directive))
	}
	if _, err := config.Load(strings.NewReader(string(encoded))); err != nil {
		t.Fatalf("Load(Marshal()) error = %v", err)
	}
}

func TestPublishedSchemaCoversStrictConfigurationContract(t *testing.T) {
	path := filepath.Join("..", "..", "schema", "sb-heartbeat.schema.json")
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}
	var schema map[string]any
	if err := json.Unmarshal(contents, &schema); err != nil {
		t.Fatalf("schema JSON error = %v", err)
	}
	if schema["$schema"] != "https://json-schema.org/draft/2020-12/schema" {
		t.Fatalf("$schema = %v", schema["$schema"])
	}
	if schema["$id"] != config.SchemaURL {
		t.Fatalf("$id = %v, want %s", schema["$id"], config.SchemaURL)
	}
	if schema["additionalProperties"] != false {
		t.Fatalf("root additionalProperties = %v", schema["additionalProperties"])
	}
	if got := schema["required"]; !schemaArrayEquals(got, "version", "projects") {
		t.Fatalf("root required = %v", got)
	}
	properties := schemaObject(t, schema, "properties")
	version := schemaObject(t, properties, "version")
	if version["const"] != float64(1) {
		t.Fatalf("version const = %v", version["const"])
	}
	defaults := schemaObject(t, properties, "defaults")
	if got := defaults["type"]; !schemaArrayEquals(got, "object", "null") {
		t.Fatalf("defaults type = %v", got)
	}
	if defaults["additionalProperties"] != false {
		t.Fatalf("defaults additionalProperties = %v", defaults["additionalProperties"])
	}
	defaultProperties := schemaObject(t, defaults, "properties")
	assertSchemaNumber(t, schemaObject(t, defaultProperties, "retries"), 0, 3)
	assertSchemaNumber(t, schemaObject(t, defaultProperties, "concurrency"), 1, 16)
	output := schemaObject(t, defaultProperties, "output")
	if got := output["enum"]; !schemaArrayContains(got, "text", "json", "", nil) {
		t.Fatalf("output enum = %v", got)
	}
	projects := schemaObject(t, properties, "projects")
	if projects["minItems"] != float64(1) {
		t.Fatalf("projects minItems = %v", projects["minItems"])
	}
	project := schemaObject(t, projects, "items")
	if project["additionalProperties"] != false {
		t.Fatalf("project additionalProperties = %v", project["additionalProperties"])
	}
	if got := project["required"]; !schemaArrayEquals(got, "name") {
		t.Fatalf("project required = %v", got)
	}
	projectProperties := schemaObject(t, project, "properties")
	if schemaObject(t, projectProperties, "name")["pattern"] != "^[a-z][a-z0-9_-]{0,62}$" {
		t.Fatalf("project name pattern = %v", schemaObject(t, projectProperties, "name")["pattern"])
	}
	for _, binding := range []string{"url", "api_key"} {
		ref := schemaObject(t, projectProperties, binding)
		if got := ref["type"]; !schemaArrayEquals(got, "object", "null") {
			t.Fatalf("%s type = %v", binding, got)
		}
		if ref["additionalProperties"] != false {
			t.Fatalf("%s additionalProperties = %v", binding, ref["additionalProperties"])
		}
		env := schemaObject(t, schemaObject(t, ref, "properties"), "env")
		if env["pattern"] != "^$|^[A-Z_][A-Z0-9_]{0,126}$" {
			t.Fatalf("%s env pattern = %v", binding, env["pattern"])
		}
		github := schemaObject(t, schemaObject(t, ref, "properties"), "github")
		if got := github["enum"]; !schemaArrayContains(got, "variable", "secret", "", nil) {
			t.Fatalf("%s github enum = %v", binding, got)
		}
	}
}

func TestPublishedSchemaAcceptsConfiguredGitHubBindingSources(t *testing.T) {
	validatePublishedSchema(t, `
version: 1
projects:
  - name: demo
    url: {env: DEMO_URL, github: secret}
    api_key: {env: DEMO_KEY, github: variable}
`)
}

func TestPublishedSchemaRejectsUnknownGitHubBindingSource(t *testing.T) {
	validatePublishedSchemaError(t, `
version: 1
projects:
  - name: demo
    url: {github: artifact}
`)
}

func TestPublishedSchemaAcceptsCLIAcceptedExplicitOmissions(t *testing.T) {
	const input = `
version: 1
defaults:
  timeout: null
  retries: null
  retry_backoff: ""
  concurrency: null
  output: ""
scheduler:
  cron: null
projects:
  - name: demo
    url:
      env: null
    api_key: null
`
	_, err := config.Load(strings.NewReader(input))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	validatePublishedSchema(t, input)
}

func TestPublishedSchemaRejectsUnknownFields(t *testing.T) {
	validatePublishedSchemaError(t, `
version: 1
projects:
  - name: demo
    query: users
`)
}

func schemaObject(t *testing.T, parent map[string]any, key string) map[string]any {
	t.Helper()
	value, ok := parent[key].(map[string]any)
	if !ok {
		t.Fatalf("%s = %#v, want object", key, parent[key])
	}
	return value
}

func assertSchemaNumber(t *testing.T, schema map[string]any, minimum, maximum float64) {
	t.Helper()
	if schema["minimum"] != minimum || schema["maximum"] != maximum {
		t.Fatalf("number bounds = [%v,%v], want [%v,%v]", schema["minimum"], schema["maximum"], minimum, maximum)
	}
}

func schemaArrayEquals(value any, want ...string) bool {
	items, ok := value.([]any)
	if !ok || len(items) != len(want) {
		return false
	}
	for i := range items {
		if items[i] != want[i] {
			return false
		}
	}
	return true
}

func schemaArrayContains(value any, want ...any) bool {
	items, ok := value.([]any)
	if !ok || len(items) != len(want) {
		return false
	}
	for _, wanted := range want {
		found := false
		for _, item := range items {
			if item == wanted {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func validatePublishedSchema(t *testing.T, input string) {
	t.Helper()
	if err := compiledPublishedSchema(t).Validate(decodeYAML(t, input)); err != nil {
		t.Fatalf("schema validation error = %v", err)
	}
}

func validatePublishedSchemaError(t *testing.T, input string) {
	t.Helper()
	if err := compiledPublishedSchema(t).Validate(decodeYAML(t, input)); err == nil {
		t.Fatal("schema validation error = nil, want rejection")
	}
}

func compiledPublishedSchema(t *testing.T) *jsonschema.Schema {
	t.Helper()
	path := filepath.Join("..", "..", "schema", "sb-heartbeat.schema.json")
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}
	var document any
	if err := json.Unmarshal(contents, &document); err != nil {
		t.Fatalf("schema JSON error = %v", err)
	}
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource(config.SchemaURL, document); err != nil {
		t.Fatalf("AddResource() error = %v", err)
	}
	schema, err := compiler.Compile(config.SchemaURL)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	return schema
}

func decodeYAML(t *testing.T, input string) any {
	t.Helper()
	var document any
	if err := yaml.Unmarshal([]byte(input), &document); err != nil {
		t.Fatalf("YAML decode error = %v", err)
	}
	return document
}

func TestLoadRejectsCollidingImplicitEnvironmentBindings(t *testing.T) {
	_, err := config.Load(strings.NewReader(`
version: 1
projects:
  - name: a-b
  - name: a_b
`))
	if err == nil || !strings.Contains(err.Error(), "duplicates") {
		t.Fatalf("Load() error = %v, want duplicate binding rejection", err)
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
		"github source":     strings.Replace(validConfig, "env: DEMO_SUPABASE_URL", "env: DEMO_SUPABASE_URL\n      github: artifact", 1),
		"duplicate binding": strings.Replace(validConfig, "DEMO_SUPABASE_API_KEY", "DEMO_SUPABASE_URL", 1),
		"timeout":           strings.Replace(validConfig, "timeout: 10s", "timeout: 61s", 1),
		"retries":           strings.Replace(validConfig, "retries: 1", "retries: 4", 1),
		"concurrency":       strings.Replace(validConfig, "concurrency: 4", "concurrency: 17", 1),
		"cron":              strings.Replace(validConfig, `cron: "37 3,11,19 * * *"`, `cron: "not a cron"`, 1),
		"cron descriptor":   strings.Replace(validConfig, `cron: "37 3,11,19 * * *"`, `cron: "@daily"`, 1),
		"multiline cron":    strings.Replace(validConfig, `cron: "37 3,11,19 * * *"`, "cron: |\n    37 3,11,19\n    * * *", 1),
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
