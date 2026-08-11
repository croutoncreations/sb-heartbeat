package scheduler

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/croutoncreations/sb-heartbeat/internal/config"
	"github.com/robfig/cron/v3"
)

const (
	cloudflareCompatibilityDate = "2026-08-10"
	cloudflareWranglerVersion   = "4.120.1"
	cloudflareTypeScriptVersion = "7.0.2"
	cloudflareVitestVersion     = "4.1.10"
	cloudflarePoolVersion       = "0.21.0"
	cloudflareFreeVariables     = 64
	cloudflareFreeSubrequests   = 50
	cloudflareRuntimeBudget     = 14 * time.Minute
)

var cloudflareWorkerNamePattern = regexp.MustCompile("^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$")

type CloudflareOptions struct {
	WorkerName string
}

type GeneratedFile struct {
	Path string
	Data []byte
}

type cloudflareProject struct {
	Name       string `json:"name"`
	URLBinding string `json:"urlBinding"`
	KeyBinding string `json:"keyBinding"`
}

func Cloudflare(cfg config.Config, options CloudflareOptions) ([]GeneratedFile, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if !cloudflareWorkerNamePattern.MatchString(options.WorkerName) {
		return nil, errors.New("Cloudflare Worker name must be 1-63 lowercase letters, digits, or hyphens and must start and end with a letter or digit")
	}
	if len(cfg.Projects)*2 > cloudflareFreeVariables {
		return nil, fmt.Errorf("Cloudflare Worker requires %d secret bindings; the free-plan limit is %d", len(cfg.Projects)*2, cloudflareFreeVariables)
	}
	worstCaseRequests := len(cfg.Projects) * (cfg.Defaults.Retries + 1)
	if worstCaseRequests > cloudflareFreeSubrequests {
		return nil, fmt.Errorf("Cloudflare Worker can make %d requests with retries; the free-plan subrequest limit is %d", worstCaseRequests, cloudflareFreeSubrequests)
	}
	backoff := time.Duration(0)
	for retry := 0; retry < cfg.Defaults.Retries; retry++ {
		backoff += min(cfg.Defaults.RetryBackoff*time.Duration(1<<retry), 30*time.Second)
	}
	perBatch := cfg.Defaults.Timeout*time.Duration(cfg.Defaults.Retries+1) + backoff
	batches := (len(cfg.Projects) + cfg.Defaults.Concurrency - 1) / cfg.Defaults.Concurrency
	if perBatch*time.Duration(batches) > cloudflareRuntimeBudget {
		return nil, errors.New("Cloudflare Worker's worst-case run exceeds the 14-minute generated runtime budget")
	}
	cloudflareSchedule, err := cloudflareCron(cfg.Scheduler.Cron)
	if err != nil {
		return nil, err
	}

	projects := make([]cloudflareProject, 0, len(cfg.Projects))
	secrets := make([]string, 0, len(cfg.Projects)*2)
	for _, project := range cfg.Projects {
		projects = append(projects, cloudflareProject{Name: project.Name, URLBinding: project.URL.Env, KeyBinding: project.APIKey.Env})
		secrets = append(secrets, project.URL.Env, project.APIKey.Env)
	}
	projectJSON, err := json.MarshalIndent(projects, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode Cloudflare projects: %w", err)
	}

	wrangler, err := json.MarshalIndent(map[string]any{
		"$schema":            "./node_modules/wrangler/config-schema.json",
		"name":               options.WorkerName,
		"main":               "src/index.ts",
		"compatibility_date": cloudflareCompatibilityDate,
		"workers_dev":        false,
		"preview_urls":       false,
		"observability":      map[string]any{"enabled": true},
		"triggers":           map[string]any{"crons": []string{cloudflareSchedule}},
		"secrets":            map[string]any{"required": secrets},
	}, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode Wrangler configuration: %w", err)
	}
	wrangler = append(wrangler, '\n')

	packageJSON := fmt.Sprintf("{\n"+
		"  \"name\": %s,\n"+
		"  \"version\": \"1.0.0\",\n"+
		"  \"private\": true,\n"+
		"  \"type\": \"module\",\n"+
		"  \"engines\": { \"node\": \">=22\" },\n"+
		"  \"packageManager\": \"npm@11.9.0\",\n"+
		"  \"scripts\": {\n"+
		"    \"check\": \"tsc --noEmit\",\n"+
		"    \"test\": \"vitest run\",\n"+
		"    \"dev\": \"wrangler dev\",\n"+
		"    \"deploy\": \"wrangler deploy\",\n"+
		"    \"types\": \"wrangler types\"\n"+
		"  },\n"+
		"  \"devDependencies\": {\n"+
		"    \"@cloudflare/vitest-pool-workers\": \"%s\",\n"+
		"    \"@cloudflare/workers-types\": \"5.20260811.1\",\n"+
		"    \"typescript\": \"%s\",\n"+
		"    \"vitest\": \"%s\",\n"+
		"    \"wrangler\": \"%s\"\n"+
		"  }\n"+
		"}\n", strconv.Quote(options.WorkerName), cloudflarePoolVersion, cloudflareTypeScriptVersion, cloudflareVitestVersion, cloudflareWranglerVersion)

	worker := fmt.Sprintf(cloudflareWorkerTemplate,
		string(projectJSON),
		cfg.Defaults.Timeout.Milliseconds(),
		cfg.Defaults.Retries,
		cfg.Defaults.RetryBackoff.Milliseconds(),
		cfg.Defaults.Concurrency,
	)
	readme := cloudflareReadme(options.WorkerName, cloudflareSchedule, secrets)

	return []GeneratedFile{
		{Path: ".gitignore", Data: []byte("node_modules/\n.dev.vars*\n.env*\n.wrangler/\ndist/\n")},
		{Path: "README.md", Data: []byte(readme)},
		{Path: "package.json", Data: []byte(packageJSON)},
		{Path: "tsconfig.json", Data: []byte(cloudflareTSConfig)},
		{Path: "vitest.config.ts", Data: []byte(cloudflareVitestConfig)},
		{Path: "wrangler.jsonc", Data: wrangler},
		{Path: "src/index.ts", Data: []byte(worker)},
		{Path: "test/index.spec.ts", Data: []byte(cloudflareWorkerTests)},
		{Path: "test/tsconfig.json", Data: []byte(cloudflareTestTSConfig)},
	}, nil
}

func cloudflareCron(spec string) (string, error) {
	parsed, err := cron.ParseStandard(spec)
	if err != nil {
		return "", errors.New("Cloudflare schedule must be a valid five-field POSIX cron expression")
	}
	schedule, ok := parsed.(*cron.SpecSchedule)
	if !ok {
		return "", errors.New("Cloudflare schedule could not be represented")
	}
	dayWildcard := schedule.Dom&(uint64(1)<<63) != 0
	weekdayWildcard := schedule.Dow&(uint64(1)<<63) != 0
	if !dayWildcard && !weekdayWildcard {
		return "", errors.New("Cloudflare schedules cannot restrict both day-of-month and weekday because their semantics may differ from POSIX cron")
	}
	fields := []string{
		cloudflareNumericField(schedule.Minute, 0, 59),
		cloudflareNumericField(schedule.Hour, 0, 23),
		cloudflareNumericField(schedule.Dom, 1, 31),
		cloudflareNumericField(schedule.Month, 1, 12),
		cloudflareWeekdayField(schedule.Dow),
	}
	return strings.Join(fields, " "), nil
}

func cloudflareNumericField(mask uint64, minimum, maximum int) string {
	if mask&(uint64(1)<<63) != 0 {
		return "*"
	}
	values := selectedBits(mask, minimum, maximum)
	parts := make([]string, len(values))
	for index, value := range values {
		parts[index] = strconv.Itoa(value)
	}
	return strings.Join(parts, ",")
}

func cloudflareWeekdayField(mask uint64) string {
	if mask&(uint64(1)<<63) != 0 {
		return "*"
	}
	names := []string{"SUN", "MON", "TUE", "WED", "THU", "FRI", "SAT"}
	values := selectedBits(mask, 0, 6)
	parts := make([]string, len(values))
	for index, value := range values {
		parts[index] = names[value]
	}
	return strings.Join(parts, ",")
}

func cloudflareReadme(workerName, schedule string, secrets []string) string {
	var bindings strings.Builder
	for _, secret := range secrets {
		fmt.Fprintf(&bindings, "- %s\n", secret)
	}
	return fmt.Sprintf("# SB Heartbeat Cloudflare Worker\n\n"+
		"This generated project runs the fixed, read-only SB Heartbeat query from a\n"+
		"Cloudflare Cron Trigger. It has no fetch handler, and both workers.dev and\n"+
		"Preview URLs are disabled for deployed versions. The %s schedule runs in UTC.\n\n"+
		"Worker name: %s\n\nRequired Worker secrets:\n\n%s\n"+
		"Store both project URLs and low-privilege publishable or legacy anon keys as\n"+
		"Cloudflare secrets. Never use a secret/service-role key. This project never commits\n"+
		"credential values: .dev.vars* and .env* are ignored.\n\n"+
		"## Validate locally\n\n"+
		"1. Install the exact direct tool versions with npm install. Commit the\n"+
		"   resulting package lock in the repository that owns this generated project.\n"+
		"2. Create a private .dev.vars file containing every required binding as\n"+
		"   NAME=value, then run chmod 600 .dev.vars.\n"+
		"3. Run npm test and npm run check.\n"+
		"4. Start npm run dev and, in another terminal, invoke only Wrangler's local\n"+
		"   scheduled-event route:\n\n"+
		"   curl \"http://localhost:8787/cdn-cgi/handler/scheduled?format=json\"\n\n"+
		"The local route exists only in the development server; deployment does not\n"+
		"create a public heartbeat endpoint.\n\n"+
		"## Deploy deliberately\n\n"+
		"Authenticate with Cloudflare, review wrangler.jsonc, then upload the code\n"+
		"and initial secrets together:\n\n"+
		"npx wrangler deploy --secrets-file .dev.vars\n\n"+
		"Delete the local secret file if it is no longer needed. Later secret rotations\n"+
		"can use npx wrangler secret put NAME. Cron configuration is owned by\n"+
		"wrangler.jsonc; deployment replaces the configured triggers, and changes\n"+
		"can take up to 15 minutes to propagate. Inspect sanitized results with Workers\n"+
		"Logs or npx wrangler tail. A failed project makes the scheduled invocation\n"+
		"fail without logging URLs, keys, response bodies, or transport details.\n",
		schedule, workerName, bindings.String())
}

const cloudflareTSConfig = `{
  "compilerOptions": {
    "target": "ES2024",
    "module": "ESNext",
    "moduleResolution": "Bundler",
    "strict": true,
    "noEmit": true,
    "skipLibCheck": true,
    "types": ["@cloudflare/workers-types"]
  },
  "include": ["src/**/*.ts", "vitest.config.ts"]
}
`

const cloudflareTestTSConfig = `{
  "extends": "../tsconfig.json",
  "compilerOptions": {
    "types": ["@cloudflare/vitest-pool-workers/types"]
  },
  "include": ["./**/*.ts", "../src/**/*.ts"]
}
`

const cloudflareVitestConfig = `import { cloudflareTest } from "@cloudflare/vitest-pool-workers";
import { defineConfig } from "vitest/config";

export default defineConfig({
  plugins: [cloudflareTest({ wrangler: { configPath: "./wrangler.jsonc" } })],
  test: { include: ["test/**/*.spec.ts"] },
});
`

const cloudflareWorkerTemplate = `// Generated by SB Heartbeat. Regenerate instead of hand-editing the runtime contract.
const MAX_RESPONSE_BYTES = 64 * 1024;
const HEARTBEAT_PATH = "/rest/v1/sb_heartbeat?select=id&id=eq.true&limit=1";
const HOSTED_ORIGIN = /^https:\/\/([a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)\.supabase\.co\/?$/;
const PUBLISHABLE_KEY = /^sb_publishable_[A-Za-z0-9_-]+$/;

type Env = Record<string, string>;
type Status = "healthy" | "missing_input" | "credential_rejected" | "timeout" |
  "temporary_upstream_failure" | "project_paused" | "database_permission_denied" |
  "api_authentication_failed" | "unexpected_response" | "no_matching_row" |
  "response_too_large";
type Project = { name: string; urlBinding: string; keyBinding: string };
export type RuntimeSettings = { timeoutMs: number; retries: number; retryBackoffMs: number; concurrency: number };
export type Result = { name: string; status: Status; http_status: number | null; attempts: number };

const PROJECTS: readonly Project[] = %s;
const SETTINGS: RuntimeSettings = {
  timeoutMs: %d,
  retries: %d,
  retryBackoffMs: %d,
  concurrency: %d,
};

export function validateHostedOrigin(raw: string): string | null {
  const match = HOSTED_ORIGIN.exec(raw);
  return match && match[0] === raw ? "https://" + match[1] + ".supabase.co" : null;
}

function legacyAnon(key: string): boolean {
  const parts = key.split(".");
  if (parts.length !== 3) return false;
  if (!/^[A-Za-z0-9_-]+$/.test(parts[1])) return false;
  try {
    const encoded = parts[1].replace(/-/g, "+").replace(/_/g, "/");
    const padded = encoded + "=".repeat((4 - encoded.length %% 4) %% 4);
    const bytes = Uint8Array.from(atob(padded), (value) => value.charCodeAt(0));
    const claims = JSON.parse(new TextDecoder("utf-8", { fatal: true }).decode(bytes)) as { role?: unknown };
	if (claims.role !== "anon") return false;
	return true;
  } catch {
    return false;
  }
}

function keyKind(key: string): "publishable" | "anon" | null {
  if (PUBLISHABLE_KEY.test(key)) return "publishable";
  return legacyAnon(key) ? "anon" : null;
}

class ResponseLimitError extends Error {}
class ResponseFormatError extends Error {}

async function boundedBytes(response: Response): Promise<Uint8Array> {
  if (!response.body) return new Uint8Array();
  const reader = response.body.getReader();
  const chunks: Uint8Array[] = [];
  let size = 0;
  while (true) {
    const next = await reader.read();
    if (next.done) break;
    size += next.value.byteLength;
    if (size > MAX_RESPONSE_BYTES) {
      await reader.cancel();
      throw new ResponseLimitError();
    }
    chunks.push(next.value);
  }
  const joined = new Uint8Array(size);
  let offset = 0;
  for (const chunk of chunks) {
    joined.set(chunk, offset);
    offset += chunk.byteLength;
  }
  return joined;
}

function decodeBody(bytes: Uint8Array): string {
  try {
    return new TextDecoder("utf-8", { fatal: true }).decode(bytes);
  } catch {
    throw new ResponseFormatError();
  }
}

function retryableHTTPStatus(status: number): boolean {
  return [408, 425, 429, 502, 503, 504, 544].includes(status);
}

const HTTP_MONTHS = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"] as const;
const HTTP_WEEKDAYS = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"] as const;

function strictHTTPDate(raw: string): number | null {
  let match = /^(Sun|Mon|Tue|Wed|Thu|Fri|Sat), ([0-9]{2}) (Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec) ([0-9]{4}) ([0-9]{2}):([0-9]{2}):([0-9]{2}) GMT$/.exec(raw);
  let weekday: string;
  let day: number;
  let month: number;
  let year: number;
  let hour: number;
  let minute: number;
  let second: number;
  if (match) {
    weekday = match[1];
    day = Number(match[2]);
    month = HTTP_MONTHS.indexOf(match[3] as typeof HTTP_MONTHS[number]);
    year = Number(match[4]);
    hour = Number(match[5]);
    minute = Number(match[6]);
    second = Number(match[7]);
  } else {
    match = /^(Sunday|Monday|Tuesday|Wednesday|Thursday|Friday|Saturday), ([0-9]{2})-(Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec)-([0-9]{2}) ([0-9]{2}):([0-9]{2}):([0-9]{2}) GMT$/.exec(raw);
    if (match) {
      weekday = match[1].slice(0, 3);
      day = Number(match[2]);
      month = HTTP_MONTHS.indexOf(match[3] as typeof HTTP_MONTHS[number]);
      const shortYear = Number(match[4]);
      year = shortYear >= 69 ? 1900 + shortYear : 2000 + shortYear;
      hour = Number(match[5]);
      minute = Number(match[6]);
      second = Number(match[7]);
    } else {
      match = /^(Sun|Mon|Tue|Wed|Thu|Fri|Sat) (Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec) ( [1-9]|[12][0-9]|3[01]) ([0-9]{2}):([0-9]{2}):([0-9]{2}) ([0-9]{4})$/.exec(raw);
      if (!match) return null;
      weekday = match[1];
      month = HTTP_MONTHS.indexOf(match[2] as typeof HTTP_MONTHS[number]);
      day = Number(match[3].trim());
      hour = Number(match[4]);
      minute = Number(match[5]);
      second = Number(match[6]);
      year = Number(match[7]);
    }
  }
  const date = new Date(0);
  date.setUTCFullYear(year, month, day);
  date.setUTCHours(hour, minute, second, 0);
  const timestamp = date.getTime();
  if (!Number.isFinite(timestamp) || date.getUTCFullYear() !== year || date.getUTCMonth() !== month ||
    date.getUTCDate() !== day || date.getUTCHours() !== hour || date.getUTCMinutes() !== minute ||
    date.getUTCSeconds() !== second || HTTP_WEEKDAYS[date.getUTCDay()] !== weekday) return null;
  return timestamp;
}

export function retryAfterMilliseconds(raw: string, nowMilliseconds = Date.now()): number | null {
  const trimmed = raw.trim();
  if (/^[0-9]+$/.test(trimmed)) {
    try {
      const seconds = BigInt(trimmed);
      if (seconds > 18_446_744_073_709_551_615n) return null;
      return seconds >= 30n ? 30_000 : Number(seconds) * 1_000;
    } catch {
      return null;
    }
  }
	const date = strictHTTPDate(raw);
	if (date === null || date <= nowMilliseconds) return null;
	return Math.min(date - nowMilliseconds, 30_000);
}

function classifyResponse(response: Response, body: string): { status: Status; retry: boolean } {
  if (response.status === 540) return { status: "project_paused", retry: false };
  if (retryableHTTPStatus(response.status)) {
    return { status: "temporary_upstream_failure", retry: true };
  }
  if (response.status === 401 || response.status === 403) {
    try {
      const parsed = JSON.parse(body) as { code?: unknown };
      if (parsed.code === "42501") return { status: "database_permission_denied", retry: false };
    } catch { /* response details are intentionally discarded */ }
    return { status: "api_authentication_failed", retry: false };
  }
  if (response.status < 200 || response.status >= 300) return { status: "unexpected_response", retry: false };
  if ((response.headers.get("content-type") ?? "").split(";", 1)[0].trim().toLowerCase() !== "application/json") {
    return { status: "unexpected_response", retry: false };
  }
  try {
    const rows = JSON.parse(body) as unknown;
    if (!Array.isArray(rows)) return { status: "unexpected_response", retry: false };
    if (rows.length === 0) return { status: "no_matching_row", retry: false };
    const row = rows[0];
    if (rows.length !== 1 || typeof row !== "object" || row === null || Array.isArray(row) ||
      Object.keys(row).length !== 1 || (row as { id?: unknown }).id !== true) {
      return { status: "unexpected_response", retry: false };
    }
    return { status: "healthy", retry: false };
  } catch {
    return { status: "unexpected_response", retry: false };
  }
}

const sleep = (milliseconds: number) => new Promise<void>((resolve) => setTimeout(resolve, milliseconds));

export async function runProject(project: Project, rawURL: string | undefined, key: string | undefined,
  settings: RuntimeSettings = SETTINGS): Promise<Result> {
  if (!rawURL || !key) return { name: project.name, status: "missing_input", http_status: null, attempts: 0 };
  const origin = validateHostedOrigin(rawURL);
  const kind = keyKind(key);
  if (!origin || !kind) return { name: project.name, status: "credential_rejected", http_status: null, attempts: 0 };

  for (let attempt = 1; attempt <= settings.retries + 1; attempt++) {
	let delay = Math.min(settings.retryBackoffMs * 2 ** (attempt - 1), 30_000);
    try {
      const headers: Record<string, string> = { Accept: "application/json", "apikey": key };
      if (kind === "anon") headers.Authorization = "Bearer " + key;
      const response = await fetch(origin + HEARTBEAT_PATH, {
        method: "GET",
        headers,
        redirect: "error",
        signal: AbortSignal.timeout(settings.timeoutMs),
      });
		let bodyBytes: Uint8Array;
      try {
		bodyBytes = await boundedBytes(response);
      } catch (error) {
        if (error instanceof ResponseLimitError) {
          return { name: project.name, status: "response_too_large", http_status: response.status, attempts: attempt };
        }
        throw error;
      }
		if (retryableHTTPStatus(response.status)) {
			if (attempt > settings.retries) {
				return { name: project.name, status: "temporary_upstream_failure", http_status: response.status, attempts: attempt };
			}
			delay = retryAfterMilliseconds(response.headers.get("retry-after") ?? "") ?? delay;
			await sleep(delay);
			continue;
		}
		let body: string;
		try {
			body = decodeBody(bodyBytes);
		} catch (error) {
			if (error instanceof ResponseFormatError) {
				if (response.status === 540) return { name: project.name, status: "project_paused", http_status: response.status, attempts: attempt };
				if (response.status === 401 || response.status === 403) return { name: project.name, status: "api_authentication_failed", http_status: response.status, attempts: attempt };
				if (response.status < 200 || response.status >= 300) return { name: project.name, status: "unexpected_response", http_status: response.status, attempts: attempt };
				return { name: project.name, status: "unexpected_response", http_status: response.status, attempts: attempt };
			}
			throw error;
		}
      const classified = classifyResponse(response, body);
      if (!classified.retry || attempt > settings.retries) {
        return { name: project.name, status: classified.status, http_status: response.status, attempts: attempt };
      }
    } catch (error) {
      if (attempt > settings.retries) {
        const timeout = error instanceof DOMException && (error.name === "TimeoutError" || error.name === "AbortError");
        return { name: project.name, status: timeout ? "timeout" : "temporary_upstream_failure", http_status: null, attempts: attempt };
      }
    }
	await sleep(delay);
  }
  return { name: project.name, status: "temporary_upstream_failure", http_status: null, attempts: settings.retries + 1 };
}

async function runAll(env: Env): Promise<Result[]> {
  const results = new Array<Result>(PROJECTS.length);
  let next = 0;
  const worker = async () => {
    while (next < PROJECTS.length) {
      const index = next++;
      const project = PROJECTS[index];
      results[index] = await runProject(project, env[project.urlBinding], env[project.keyBinding]);
    }
  };
  await Promise.all(Array.from({ length: Math.min(SETTINGS.concurrency, PROJECTS.length) }, worker));
  return results;
}

export default {
	async scheduled(controller: ScheduledController, env: Env): Promise<void> {
	controller.noRetry();
    const started_at = new Date().toISOString();
    const results = await runAll(env);
    const success = results.every((result) => result.status === "healthy");
    console.log(JSON.stringify({ schema_version: 1, started_at, finished_at: new Date().toISOString(), success, projects: results }));
    if (!success) throw new Error("SB Heartbeat failed for " + results.filter((result) => result.status !== "healthy").length + " project(s)");
  },
} satisfies ExportedHandler<Env>;
`

const cloudflareWorkerTests = `import { afterEach, describe, expect, it, vi } from "vitest";
import worker, { retryAfterMilliseconds, runProject, validateHostedOrigin, type RuntimeSettings } from "../src/index";

const project = { name: "tone-clone", urlBinding: "PROJECT_URL", keyBinding: "PROJECT_KEY" };
const settings: RuntimeSettings = { timeoutMs: 1_000, retries: 0, retryBackoffMs: 100, concurrency: 1 };
const origin = "https://abcdefghijklmnopqrst.supabase.co";
const publishable = "sb_publishable_example";
const jwt = (role: string) => "header." + btoa(JSON.stringify({ role })).replace(/=/g, "").replace(/\+/g, "-").replace(/\//g, "_") + ".signature";

afterEach(() => vi.unstubAllGlobals());

describe("generated heartbeat contract", () => {
	it("honors valid Retry-After values up to 30 seconds", () => {
		const now = Date.UTC(2026, 7, 10, 20, 0, 0);
		expect(retryAfterMilliseconds("29", now)).toBe(29_000);
		expect(retryAfterMilliseconds("31", now)).toBe(30_000);
		expect(retryAfterMilliseconds("18446744073709551615", now)).toBe(30_000);
		expect(retryAfterMilliseconds("18446744073709551616", now)).toBeNull();
		expect(retryAfterMilliseconds(new Date(now + 5_000).toUTCString(), now)).toBe(5_000);
		expect(retryAfterMilliseconds("Monday, 10-Aug-26 20:00:05 GMT", now)).toBe(5_000);
		expect(retryAfterMilliseconds("Mon Aug 10 20:00:05 2026", now)).toBe(5_000);
		expect(retryAfterMilliseconds(new Date(now - 5_000).toUTCString(), now)).toBeNull();
		expect(retryAfterMilliseconds("2026-08-10T20:00:05Z", now)).toBeNull();
		expect(retryAfterMilliseconds("August 10, 2026 20:00:05 GMT", now)).toBeNull();
		expect(retryAfterMilliseconds("Sunday, 10-Aug-26 20:00:05 GMT", now)).toBeNull();
		expect(retryAfterMilliseconds("Mon, 31 Feb 2026 20:00:05 GMT", now)).toBeNull();
		expect(retryAfterMilliseconds("not-a-delay", now)).toBeNull();
	});

  it("validates hosted Supabase origins", () => {
    expect(validateHostedOrigin(origin + "/")).toBe(origin);
    for (const invalid of ["http://abcdefghijklmnopqrst.supabase.co", origin + "/path", origin + ":443", origin + "?x=1", origin + "\n", "https://example.com"]) {
      expect(validateHostedOrigin(invalid)).toBeNull();
    }
  });

  it("supports legacy anon keys with the required authorization header", async () => {
    const anon = jwt("anon");
    const fetchMock = vi.fn().mockResolvedValue(new Response('[{"id":true}]', { status: 200, headers: { "content-type": "application/json" } }));
    vi.stubGlobal("fetch", fetchMock);
    await expect(runProject(project, origin, anon, settings)).resolves.toMatchObject({ status: "healthy" });
    expect(fetchMock.mock.calls[0][1].headers).toMatchObject({ apikey: anon, Authorization: "Bearer " + anon });
  });

  it("makes the fixed read-only request", async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response('[{"id":true}]', { status: 200, headers: { "content-type": "application/json" } }));
    vi.stubGlobal("fetch", fetchMock);
    await expect(runProject(project, origin, publishable, settings)).resolves.toMatchObject({ status: "healthy", attempts: 1 });
    expect(fetchMock).toHaveBeenCalledOnce();
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe(origin + "/rest/v1/sb_heartbeat?select=id&id=eq.true&limit=1");
    expect(init).toMatchObject({ method: "GET", redirect: "error", headers: { Accept: "application/json", apikey: publishable } });
    expect(init.headers).not.toHaveProperty("Authorization");
  });

  it("rejects elevated keys before fetch", async () => {
    const fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);
    await expect(runProject(project, origin, "sb_secret_forbidden", settings)).resolves.toMatchObject({ status: "credential_rejected", attempts: 0 });
    await expect(runProject(project, origin, jwt("service_role"), settings)).resolves.toMatchObject({ status: "credential_rejected", attempts: 0 });
    const padded = jwt("anon").replace(".signature", "=.signature");
    await expect(runProject(project, origin, padded, settings)).resolves.toMatchObject({ status: "credential_rejected", attempts: 0 });
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("rejects redirects", async () => {
    const fetchMock = vi.fn().mockRejectedValue(new TypeError("redirect mode is error"));
    vi.stubGlobal("fetch", fetchMock);
    await expect(runProject(project, origin, publishable, settings)).resolves.toMatchObject({ status: "temporary_upstream_failure" });
    expect(fetchMock.mock.calls[0][1].redirect).toBe("error");
  });

  it("rejects oversized responses", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response("x".repeat(64 * 1024 + 1), { status: 200, headers: { "content-type": "application/json" } })));
    await expect(runProject(project, origin, publishable, settings)).resolves.toMatchObject({ status: "response_too_large" });
  });

  it("classifies invalid UTF-8 as an unexpected response", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(new Uint8Array([0xff]), { status: 200, headers: { "content-type": "application/json" } })));
    await expect(runProject(project, origin, publishable, settings)).resolves.toMatchObject({ status: "unexpected_response" });
  });

	it("retries a retryable HTTP status even when its body is invalid UTF-8", async () => {
		const fetchMock = vi.fn()
			.mockResolvedValueOnce(new Response(new Uint8Array([0xff]), { status: 503 }))
			.mockResolvedValueOnce(new Response('[{"id":true}]', { status: 200, headers: { "content-type": "application/json" } }));
		vi.stubGlobal("fetch", fetchMock);
		await expect(runProject(project, origin, publishable, { ...settings, retries: 1, retryBackoffMs: 0 })).resolves.toMatchObject({ status: "healthy", attempts: 2 });
		expect(fetchMock).toHaveBeenCalledTimes(2);
	});

	it("prevents platform retries before scheduled work", async () => {
		const noRetry = vi.fn();
		const controller = { noRetry } as unknown as ScheduledController;
		await expect(worker.scheduled(controller, {})).rejects.toThrow("SB Heartbeat failed");
		expect(noRetry).toHaveBeenCalledOnce();
	});
});
`
