# Local scheduler generators

SB Heartbeat can generate local scheduler definitions without installing,
loading, or enabling them. Generation keeps the review and activation boundary
with the machine owner.

## Private environment files

Scheduled processes do not reliably inherit an interactive shell environment.
Use an explicit `--env-file` for the URL and low-privilege API-key bindings
referenced by `sb-heartbeat.yaml`:

```text
PROJECT_URL=https://project-ref.example.invalid
PROJECT_API_KEY=replace-with-the-low-privilege-client-key
```

The format is literal `NAME=value`, not shell syntax. SB Heartbeat never runs
the file through a shell, expands variables, or changes its own process
environment. Blank lines and lines beginning with `#` are allowed; quoting,
`export`, duplicate names, invalid environment names, NUL bytes, and files over
64 KiB are rejected. Values continue through the first `=` so padded legacy
client keys remain representable.

Create a dedicated file outside the repository and restrict it before use:

```bash
mkdir -p "$HOME/.config/sb-heartbeat"
touch "$HOME/.config/sb-heartbeat/heartbeat.env"
chmod 600 "$HOME/.config/sb-heartbeat/heartbeat.env"
```

SB Heartbeat refuses symlinks, non-regular files, and files accessible to the
group or other users. Values from an explicit environment file take precedence
over same-named inherited variables. They are held in memory only and are
never included in output or generated scheduler files.

Strict environment-file loading is available on macOS and Linux. Windows is
rejected because SB Heartbeat does not currently verify equivalent private ACL
semantics there.

Validate the exact files before enabling a scheduler:

```bash
sb-heartbeat \
  --config /absolute/path/sb-heartbeat.yaml \
  --env-file "$HOME/.config/sb-heartbeat/heartbeat.env" \
  doctor
```

## macOS `launchd`

Generate a per-user LaunchAgent:

```bash
sb-heartbeat \
  --config /absolute/path/sb-heartbeat.yaml \
  --env-file "$HOME/.config/sb-heartbeat/heartbeat.env" \
  install launchd \
  --output-path "$HOME/Library/LaunchAgents/io.github.croutoncreations.sb-heartbeat.plist" \
  --stdout-path "$HOME/Library/Logs/sb-heartbeat.log" \
  --stderr-path "$HOME/Library/Logs/sb-heartbeat.error.log"
```

The generated plist invokes the binary directly with a `ProgramArguments`
array. It contains file paths and environment-variable names, never binding
values or shell commands. SB Heartbeat never loads the plist and never enables
the agent.

Review the plist and environment-file permissions, then load it for the current
user:

```bash
launchctl bootstrap "gui/$(id -u)" \
  "$HOME/Library/LaunchAgents/io.github.croutoncreations.sb-heartbeat.plist"
launchctl kickstart -k \
  "gui/$(id -u)/io.github.croutoncreations.sb-heartbeat"
```

Inspect status and logs after the one-off kickstart. To stop and unload it:

```bash
launchctl bootout "gui/$(id -u)" \
  "$HOME/Library/LaunchAgents/io.github.croutoncreations.sb-heartbeat.plist"
```

The calendar runs in the Mac's local timezone. POSIX cron treats simultaneous
day-of-month and weekday restrictions as an OR expression, while `launchd`
calendar dictionaries do not provide an equivalent contract, so the generator
rejects that combination. It also rejects schedules that would require more
than 512 calendar dictionaries. The default three-times-daily schedule creates
three dictionaries.

Scheduled runs can be delayed or coalesced while the Mac sleeps. Keep the logs,
stable exit codes, and a manual `doctor` run available for diagnosis.

## Linux `systemd` user timer

Generate a hardened per-user oneshot service and its timer:

```bash
mkdir -p "$HOME/.config/systemd/user"
sb-heartbeat \
  --config /absolute/path/sb-heartbeat.yaml \
  --env-file "$HOME/.config/sb-heartbeat/heartbeat.env" \
  install systemd \
  --service-output "$HOME/.config/systemd/user/sb-heartbeat.service" \
  --timer-output "$HOME/.config/systemd/user/sb-heartbeat.timer"
```

The generated service invokes the binary directly, disables systemd
environment-variable expansion for its arguments, and applies filesystem,
privilege, private-user, private-device, address-family, and executable-memory
restrictions. These per-user namespace protections require systemd 244 or newer
and a host that permits unprivileged user namespaces; validate the generated
service on the target host before enabling it. It reads the configuration and
private environment file but writes no application state; stdout and stderr go
to the user journal. The timer uses `Persistent=true`, so systemd can run one
missed heartbeat after the user manager becomes available.

SB Heartbeat preflights both output files before writing either one. It never
loads the units and never enables the timer. Review both files and validate the
environment first, then activate them explicitly:

```bash
systemctl --user daemon-reload
systemctl --user enable --now sb-heartbeat.timer
systemctl --user start sb-heartbeat.service
systemctl --user status sb-heartbeat.service sb-heartbeat.timer
journalctl --user-unit sb-heartbeat.service
```

The direct service start is the manual one-off validation. To stop future runs:

```bash
systemctl --user disable --now sb-heartbeat.timer
systemctl --user daemon-reload
```

Calendar events use the host's local timezone and the same bounded translation
rules as the `launchd` generator. Simultaneous non-wildcard day-of-month and
weekday restrictions are rejected instead of silently changing POSIX cron's OR
semantics. User timers normally run only while the user manager is active; a
Linux administrator may enable lingering when that behavior is appropriate.
