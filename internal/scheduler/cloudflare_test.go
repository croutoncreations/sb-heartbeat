package scheduler_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/croutoncreations/sb-heartbeat/internal/config"
	"github.com/croutoncreations/sb-heartbeat/internal/scheduler"
)

func TestCloudflareGeneratesPinnedCronOnlyWorkerProject(t *testing.T) {
	cfg := workflowConfig(t)
	files, err := scheduler.Cloudflare(cfg, scheduler.CloudflareOptions{WorkerName: "sb-heartbeat-toneclone"})
	if err != nil {
		t.Fatal(err)
	}
	generated := generatedFileMap(t, files)
	for _, name := range []string{
		".gitignore", "README.md", "package.json", "tsconfig.json", "vitest.config.ts",
		"wrangler.jsonc", "src/index.ts", "test/index.spec.ts", "test/tsconfig.json",
	} {
		if _, exists := generated[name]; !exists {
			t.Errorf("missing generated file %s", name)
		}
	}
	if len(generated) != 9 {
		t.Fatalf("generated %d files, want 9", len(generated))
	}

	var wrangler struct {
		Name        string `json:"name"`
		Main        string `json:"main"`
		WorkersDev  bool   `json:"workers_dev"`
		PreviewURLs bool   `json:"preview_urls"`
		Triggers    struct {
			Crons []string `json:"crons"`
		} `json:"triggers"`
		Secrets struct {
			Required []string `json:"required"`
		} `json:"secrets"`
	}
	if err := json.Unmarshal(generated["wrangler.jsonc"], &wrangler); err != nil {
		t.Fatal(err)
	}
	if wrangler.Name != "sb-heartbeat-toneclone" || wrangler.Main != "src/index.ts" || wrangler.WorkersDev || wrangler.PreviewURLs {
		t.Fatalf("unexpected Wrangler routing config: %+v", wrangler)
	}
	if got := strings.Join(wrangler.Triggers.Crons, ","); got != "37 3,11,19 * * *" {
		t.Fatalf("cron = %q", got)
	}
	wantSecrets := "DEMO_URL,DEMO_KEY"
	if got := strings.Join(wrangler.Secrets.Required, ","); got != wantSecrets {
		t.Fatalf("required secrets = %q, want %q", got, wantSecrets)
	}

	packageJSON := string(generated["package.json"])
	for _, required := range []string{
		`"private": true`, `"wrangler": "4.120.1"`, `"typescript": "7.0.2"`,
		`"vitest": "4.1.10"`, `"@cloudflare/vitest-pool-workers": "0.21.0"`,
		`"test": "vitest run"`, `"deploy": "wrangler deploy"`,
	} {
		if !strings.Contains(packageJSON, required) {
			t.Errorf("package.json missing %q", required)
		}
	}
	for _, unpinned := range []string{`": "^`, `": "~`, `": "*"`} {
		if strings.Contains(packageJSON, unpinned) {
			t.Fatalf("package.json contains unpinned dependency %q", unpinned)
		}
	}

	worker := string(generated["src/index.ts"])
	for _, required := range []string{
		`/rest/v1/sb_heartbeat?select=id&id=eq.true&limit=1`,
		`redirect: "error"`, `const MAX_RESPONSE_BYTES = 64 * 1024`,
		`"apikey"`, `Authorization`, `sb_publishable_`, `role !== "anon"`,
		`scheduled(`, `controller.noRetry()`, `retry-after`, `DEMO_URL`, `DEMO_KEY`,
	} {
		if !strings.Contains(worker, required) {
			t.Errorf("worker missing %q", required)
		}
	}
	for _, forbidden := range []string{"service_role key", "console.log(key", "async fetch(request"} {
		if strings.Contains(worker, forbidden) {
			t.Fatalf("worker contains forbidden %q", forbidden)
		}
	}

	tests := string(generated["test/index.spec.ts"])
	for _, required := range []string{
		"fixed read-only request", "rejects elevated keys before fetch", "rejects redirects",
		"rejects oversized responses", "validates hosted Supabase origins",
		"honors valid Retry-After", "prevents platform retries", "retries a retryable HTTP status",
	} {
		if !strings.Contains(tests, required) {
			t.Errorf("generated Worker tests missing %q", required)
		}
	}

	readme := string(generated["README.md"])
	for _, required := range []string{
		"UTC", "npm test", "npm run check", "npm run dev", "/cdn-cgi/handler/scheduled?format=json",
		"wrangler deploy --secrets-file", "never commits", "workers.dev", "Preview URLs",
	} {
		if !strings.Contains(readme, required) {
			t.Errorf("generated README missing %q", required)
		}
	}
	gitignore := string(generated[".gitignore"])
	for _, required := range []string{".dev.vars*", ".env*", "node_modules/"} {
		if !strings.Contains(gitignore, required) {
			t.Errorf("generated .gitignore missing %q", required)
		}
	}
}

func TestCloudflareTranslatesPOSIXWeekdaysWithoutChangingDaySemantics(t *testing.T) {
	cfg := workflowConfig(t)
	cfg.Scheduler.Cron = "15 4 * 1,7 0,1,5,6"
	files, err := scheduler.Cloudflare(cfg, scheduler.CloudflareOptions{WorkerName: "sb-heartbeat-weekdays"})
	if err != nil {
		t.Fatal(err)
	}
	wrangler := string(generatedFileMap(t, files)["wrangler.jsonc"])
	if !strings.Contains(wrangler, `"15 4 * 1,7 SUN,MON,FRI,SAT"`) {
		t.Fatalf("translated cron missing:\n%s", wrangler)
	}

	cfg.Scheduler.Cron = "*/15 */6 * * *"
	files, err = scheduler.Cloudflare(cfg, scheduler.CloudflareOptions{WorkerName: "sb-heartbeat-steps"})
	if err != nil {
		t.Fatal(err)
	}
	wrangler = string(generatedFileMap(t, files)["wrangler.jsonc"])
	if !strings.Contains(wrangler, `"0,15,30,45 0,6,12,18 * * *"`) {
		t.Fatalf("stepped cron changed semantics:\n%s", wrangler)
	}

	cfg.Scheduler.Cron = "15 4 1 * 1"
	if _, err := scheduler.Cloudflare(cfg, scheduler.CloudflareOptions{WorkerName: "sb-heartbeat-days"}); err == nil {
		t.Fatal("Cloudflare() accepted simultaneous day-of-month and weekday restrictions")
	}
}

func TestCloudflareRejectsInvalidNamesAndFreePlanOverflow(t *testing.T) {
	cfg := workflowConfig(t)
	for _, name := range []string{"", "UPPER", "-bad", "bad_name", strings.Repeat("a", 64)} {
		if _, err := scheduler.Cloudflare(cfg, scheduler.CloudflareOptions{WorkerName: name}); err == nil {
			t.Errorf("Cloudflare() accepted worker name %q", name)
		}
	}

	projects := make([]config.Project, 26)
	for index := range projects {
		name := "project-" + string(rune('a'+index))
		urlEnv, keyEnv := config.SuggestedEnvironmentNames(name)
		projects[index] = config.Project{Name: name, URL: config.EnvReference{Env: urlEnv}, APIKey: config.EnvReference{Env: keyEnv}}
	}
	tooMany, err := config.NewProjects(projects, "37 3,11,19 * * *")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := scheduler.Cloudflare(tooMany, scheduler.CloudflareOptions{WorkerName: "sb-heartbeat-many"}); err == nil {
		t.Fatal("Cloudflare() accepted more projects/retries than the free-plan subrequest limit")
	}

	withinRequests, err := config.NewProjects(projects[:10], "37 3,11,19 * * *")
	if err != nil {
		t.Fatal(err)
	}
	withinRequests.Defaults.Timeout = 60 * time.Second
	withinRequests.Defaults.Concurrency = 1
	if _, err := scheduler.Cloudflare(withinRequests, scheduler.CloudflareOptions{WorkerName: "sb-heartbeat-slow"}); err == nil {
		t.Fatal("Cloudflare() accepted a worst-case run longer than the scheduled invocation budget")
	}
}

func generatedFileMap(t *testing.T, files []scheduler.GeneratedFile) map[string][]byte {
	t.Helper()
	result := make(map[string][]byte, len(files))
	for _, file := range files {
		if file.Path == "" || len(file.Data) == 0 {
			t.Fatalf("invalid generated file: %+v", file)
		}
		if _, exists := result[file.Path]; exists {
			t.Fatalf("duplicate generated path %s", file.Path)
		}
		result[file.Path] = file.Data
	}
	return result
}
