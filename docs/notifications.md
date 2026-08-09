# Repeated-failure notifications

`run` and `doctor` can send one webhook event after a project fails a
configurable number of consecutive checks:

```bash
export SB_HEARTBEAT_NOTIFICATION_WEBHOOK='https://hooks.example.com/sb-heartbeat'

sb-heartbeat run \
  --notification-state /absolute/private/path/notifications.json \
  --notification-webhook-env SB_HEARTBEAT_NOTIFICATION_WEBHOOK \
  --notify-after 3
```

The state path and webhook binding are explicit. `--notify-after` accepts 1
through 100 and defaults to 3. `--notification-state` and
`--notification-webhook-env` are required together. The webhook environment variable must contain an absolute
HTTPS URL; the URL itself is never accepted as a command-line argument, stored
in the state file, or included in an error message. Keep it in your scheduler's
secret store rather than committing it.

Selecting that environment variable explicitly grants SB Heartbeat authority
to send this fixed POST to its destination, including a private-network or IP
address if you configure one. Use a dedicated receiver you control and scope
its URL or token only to accepting heartbeat notifications.

Every non-healthy stable status increments the project's consecutive failure
count, even when the status changes. The first healthy result resets the
episode. One event becomes pending at the threshold, and successful delivery
suppresses repeats until that reset. A failed delivery remains pending and is
retried after the next observed failure. This release sends no recovery notification.

The receiver must accept this fixed JSON shape:

```json
{
  "schema_version": 1,
  "event": "repeated_failure",
  "project": "toneclone-dev",
  "status": "timeout",
  "episode": 4,
  "consecutive_failures": 3,
  "observed_at": "2026-08-09T12:00:00Z"
}
```

The payload does not contain the Supabase URL, environment-variable names or
values, API keys, request headers, response bodies, diagnostic messages, or the
webhook URL. Redirects are rejected, delivery is capped at 10 seconds, and an
HTTP response body is never copied into output or errors. Any `2xx` status is
success; other statuses and transport failures make the command exit 3.

State is written as pending before delivery and marked delivered afterward.
Delivery is therefore at least once: interruption after the receiver accepts an
event can cause a duplicate, while a failed attempt cannot silently suppress
the event. Receivers should use project plus episode as a deduplication key.

The local state file is POSIX-only, private mode `0600`, bounded to 256 KiB,
strictly validated, and atomically replaced. It requires a persistent local
filesystem and a single writer. Do not point concurrent schedulers at the same
file. Windows rejects this mode before any Supabase request because Go cannot
guarantee the required replacement semantics there.

State for project names not included in a run is retained so `--project` runs
cannot erase another project's failure episode. If projects are permanently
renamed or removed, stop every writer before deleting the state file; deletion
resets notification suppression and streak counts for all projects.
