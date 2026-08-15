# Local status history

`run` and `doctor` can retain a small local JSON history for debugging:

```bash
sb-heartbeat run \
  --history /absolute/private/path/history.json \
  --history-limit 100
```

History is opt-in. `--history-limit` defaults to 100 and accepts 1 through
1,000. Supplying the limit without `--history` is rejected. Use an absolute
private path outside the repository so operational records are not committed by
accident.

Local history is unavailable on Windows because Go does not guarantee atomic
replacement there. Windows runs fail before network access when `--history` is
requested; normal `run` and `doctor` behavior remains supported.

Each entry contains only start and finish timestamps, overall success, and each
project's configured name, stable status, HTTP status, latency, and attempt
count. It never includes URLs, environment-variable names or values, API keys,
authorization headers, response bodies, or error messages.

The complete snapshot is limited to 1 MiB. If the configured run count would
exceed that byte limit, the oldest complete entries are removed until the file
fits. New files and replacements use mode `0600` on POSIX systems. Existing
symlinks, non-regular files, malformed JSON, unknown fields, unsupported schema
versions, and oversized snapshots are rejected rather than replaced.

On supported POSIX systems, writes use an atomic same-directory replacement, so
readers see either the old or new complete snapshot. This is not a multi-writer
database: concurrent processes are safe from partial files, but last writer wins
and one process can replace another process's newly appended entry. Use one
scheduler writer per history path, or give independent schedulers separate
files.

A requested history write is part of the command's success contract. If it
cannot be validated or written, SB Heartbeat returns internal-error exit code 3
instead of silently dropping the record. No history entry is attempted for
configuration, credential, or other preflight failures.
