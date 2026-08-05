# Configuration

SB Heartbeat reads `./sb-heartbeat.yaml`, or the exact path supplied through
`--config`. It does not search parent directories.

```yaml
version: 1
defaults:
  timeout: 10s
  retries: 1
  retry_backoff: 2s
  concurrency: 4
  output: text
scheduler:
  cron: 37 3,11,19 * * *
projects:
  - name: demo
```

Configuration is strict: unknown and duplicate YAML keys are rejected. The MVP
has no custom query fields; every request targets the guarded heartbeat table.
When `url.env` or `api_key.env` is omitted, SB Heartbeat derives
`SB_HEARTBEAT_<NORMALIZED_NAME>_URL` and
`SB_HEARTBEAT_<NORMALIZED_NAME>_API_KEY`. Normalization uppercases the project
name and changes hyphens to underscores. Explicit bindings take precedence,
and configurations whose derived names collide are rejected.

Runtime ranges:

| Setting | Default | Allowed |
| --- | ---: | ---: |
| Timeout per attempt | 10s | 1–60s |
| Retries after first attempt | 1 | 0–3 |
| Initial retry backoff | 2s | 100ms–30s |
| Concurrent projects | 4 | 1–16 |
| Response body | 64 KiB | fixed |

Cron uses a five-field POSIX expression. GitHub Actions interprets it as UTC;
local cron uses the scheduler host's timezone. The default runs at minutes 37
of hours 3, 11, and 19 in the applicable timezone.
