# Local cron

SB Heartbeat prints a suggested crontab entry but never installs or edits a
crontab:

```bash
sb-heartbeat --config /absolute/path/sb-heartbeat.yaml install cron \
  --binary-path /absolute/path/sb-heartbeat \
  --log-path /absolute/path/sb-heartbeat.log
```

The configuration, binary, and optional log paths must be absolute. When
`--binary-path` is omitted, SB Heartbeat uses the running executable's path.
`--log-path` is optional; when present, stdout and stderr are appended to that
file. Create its parent directory with permissions for the scheduler account.
Paths containing line breaks or `%` are rejected because crontab parses those
characters before invoking the shell.

Review the printed line before adding it with `crontab -e`. Cron commonly has a
smaller environment than an interactive shell, so arrange for every printed
URL and API-key environment variable to be present without placing values in
the crontab itself. A protected environment file loaded by a wrapper script or
the host scheduler's secret mechanism is preferable. Restrict configuration,
wrapper, environment, and log permissions to the account running the job.

The configured five-field expression runs in the cron host's timezone. Jobs
can be delayed or skipped while the host is asleep or unavailable. Monitor the
log and the command's stable exit codes: `0` means all checks passed, `1` means
a heartbeat failed, `2` means input or configuration is invalid, and `3` means
an internal execution failure. Run `sb-heartbeat doctor` manually after any
environment or permission change.
