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
`--notification-webhook-env` are required together. The webhook environment
variable must contain an absolute HTTPS URL; the URL itself is never accepted
as a command-line argument, stored in the state file, or included in an error
message. Keep it in your scheduler's secret store rather than committing it.

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
  "project": "demo-staging",
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

## Generated GitHub Actions workflow

Starting with the `v0.2.0` runtime, generate a scheduled workflow with durable
notification state by naming the GitHub secret that will hold the webhook:

```bash
sb-heartbeat install github \
  --sb-heartbeat-version v0.3.3 \
  --github-notification-webhook-secret SB_HEARTBEAT_NOTIFICATION_WEBHOOK \
  --notify-after 3
```

The generator writes only the secret name. Add the value through the repository
web UI under **Settings → Secrets and variables → Actions**, or without placing
the value in shell history:

```bash
printf '%s' "$SB_HEARTBEAT_NOTIFICATION_WEBHOOK" |
  gh secret set SB_HEARTBEAT_NOTIFICATION_WEBHOOK --repo OWNER/REPOSITORY
```

The workflow maps that secret only into the heartbeat step. Durable
notifications run only from the repository's default branch; a manual dispatch
on another ref fails with an explicit guard message and performs no heartbeat.
The workflow serializes default-branch runs, uses read-only
Actions metadata to identify the immediately preceding run of the same
workflow and default branch, and accepts restored state only when its recorded
run ID matches that exact predecessor (or the same run during a rerun). An
older fallback or malformed sequence metadata resets the state instead of
rolling it back.
The workflow saves changed state even when the heartbeat or webhook fails and
reports the original CLI exit status only after the save. The state cache never
contains the webhook or Supabase credentials.

Persistence uses the official `actions/cache` restore and save actions pinned
to reviewed commit `55cc8345863c7cc4c66a329aec7e433d2d1c52a9`
(`v6.1.0`). GitHub cache storage is best effort: a cache can be evicted or
manually deleted, which resets streak and suppression state and delays the next
notification until the threshold is reached again. Failure to resolve GitHub's
preceding-run metadata also resets state safely. Do not treat this cache as an
audit log. Re-run generation with the same notification options and
`--force` when upgrading or replacing an installed workflow.
