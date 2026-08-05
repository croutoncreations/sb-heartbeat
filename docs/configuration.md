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
    url:
      env: DEMO_SUPABASE_URL
    api_key:
      env: DEMO_SUPABASE_API_KEY
```

Configuration is strict: unknown and duplicate YAML keys are rejected. The MVP
has no custom query fields; every request targets the guarded heartbeat table.

Runtime ranges:

| Setting | Default | Allowed |
| --- | ---: | ---: |
| Timeout per attempt | 10s | 1–60s |
| Retries after first attempt | 1 | 0–3 |
| Initial retry backoff | 2s | 100ms–30s |
| Concurrent projects | 4 | 1–16 |
| Response body | 64 KiB | fixed |

Cron uses a five-field POSIX expression interpreted as UTC by GitHub Actions.
The default runs at 03:37, 11:37, and 19:37 UTC.
