# SB Heartbeat

![Animated overview: an idle Supabase project's flat line gets a pulse from SB Heartbeat and shows as active, followed by its features: keeping low-traffic projects active, one tiny read-only query, running on GitHub Actions, Cloudflare Workers, Docker, cron, launchd, or systemd, and easy installation and management by coding agents.](docs/assets/sb-heartbeat-promo.gif)

Keep intentionally retained, low-traffic Supabase projects active with a
least-privilege database heartbeat.

SB Heartbeat is a small Go CLI that runs wherever you already schedule jobs. It
queries one fixed row through the Supabase Data API using either a publishable
key or a legacy anon key. It does not require a secret key, service-role key,
database password, hosted account, or ongoing database writes.

> [!IMPORTANT]
> SB Heartbeat is best-effort. Supabase controls its project-activity classifier and
> can change it. Production projects should use an appropriate paid plan, and
> projects you no longer need should be archived rather than kept running.

## Features

- **Keeps low-traffic projects active.** Scheduled heartbeats run three times a
  day by default, across one or many projects.
- **One tiny, read-only query.** A fixed, read-only `GET` reads one row from a
  dedicated table; transient failures may be retried. Only `SELECT (id)` is granted, and elevated keys are
  rejected before any network request.
- **Runs wherever you already run things.** Generate a
  [GitHub Actions workflow](docs/github-actions.md), a
  [Cloudflare Worker](docs/cloudflare.md), [cron](docs/local-cron.md),
  [launchd, or systemd](docs/local-schedulers.md) configuration, or use the
  [Docker image](docs/docker.md). Generators produce reviewable output; they never
  deploy, load, or edit schedulers for you.
- **Built for coding agents to install and manage.** [`llms.txt`](llms.txt), an
  [agent installation guide](docs/agent-install.md), and
  [copyable prompts](docs/agent-prompts.md) let your coding agent set it up,
  verify it with `doctor`, and later remove it, with explicit safety boundaries.
  The installation guidance is
  [tested with independent agents](docs/agent-evaluation.md).
- **Clear diagnostics and monitoring.** `doctor` explains failures with stable
  codes. Opt into JSON output, [status history](docs/status-history.md),
  [Prometheus metrics](docs/metrics.md),
  [repeated-failure webhooks](docs/notifications.md), and
  [GitHub annotations and artifacts](docs/github-observability.md).
- **Verifiable releases.** Prebuilt binaries for macOS, Linux, and Windows
  (amd64 and arm64) ship with checksums and signed build provenance.

## Installation

Install an exact release with Go:

```bash
go install github.com/croutoncreations/sb-heartbeat/cmd/sb-heartbeat@v0.3.3
```

Or download a prebuilt archive from the
[v0.3.3 release](https://github.com/croutoncreations/sb-heartbeat/releases/tag/v0.3.3)
and verify it as described in [Release verification](docs/releasing.md). A
multi-architecture container image is published to
`ghcr.io/croutoncreations/sb-heartbeat:v0.3.3`; see [Docker](docs/docker.md).

## Quick start

```bash
sb-heartbeat init \
  --non-interactive \
  --project-name my-staging-project \
  --migration-output supabase/migrations/20260815000000_sb-heartbeat.sql
```

Review and apply the generated SQL through your normal migration process, then:

```bash
export SB_HEARTBEAT_MY_STAGING_PROJECT_URL=https://your-project-ref.supabase.co
export SB_HEARTBEAT_MY_STAGING_PROJECT_API_KEY=sb_publishable_your_key

sb-heartbeat doctor
sb-heartbeat run
```

Interactive `init` generates `sb-heartbeat.sql` beside the configuration by
default and prints the same apply/configure/doctor sequence. It never connects
to PostgreSQL or applies the migration itself.

For a legacy anon JWT, put that value in the same API-key environment variable.
SB Heartbeat identifies the form and sends the appropriate headers. Elevated keys
are rejected before any network request. `--url-env` and `--api-key-env` can
override the environment names derived from the project name.

## GitHub Actions

Generate a pinned, checksum-verifying workflow:

```bash
sb-heartbeat install github --sb-heartbeat-version v0.3.3
```

For each project, create the configured URL as a GitHub Actions variable and
the API key as a GitHub Actions secret. The generated workflow runs at
`37 3,11,19 * * *` by default and supports manual dispatch.
Interactive initialization suggests the current repository name, displays the
exact derived binding names, and can collect multiple projects. Existing
GitHub binding names can be entered instead of the derived defaults.

## Other schedulers

For a local scheduler, `sb-heartbeat install cron` prints a shell-safe suggested
entry and the required environment-variable names. It never edits your crontab.
`sb-heartbeat install launchd` generates a reviewable macOS user LaunchAgent
without loading it. Both scheduled and manual runs can use a strict private
`--env-file` without evaluating shell syntax.
`sb-heartbeat install systemd` similarly generates a hardened Linux user
service and timer without loading or enabling either unit.
`sb-heartbeat install cloudflare` generates a tested, cron-only TypeScript
Worker project without deploying it or storing credential values.

## Using a coding agent

Point your agent at [`llms.txt`](llms.txt) or give it one of the
[copyable prompts](docs/agent-prompts.md). The guidance tells agents to use an
exact, checksum-verified release, configure environment-variable names rather
than key values, generate SQL without applying it, never modify your
repository's `AGENTS.md` or `CLAUDE.md`, and report every remaining manual step.

## Security model

- The generated table has one possible row.
- Broad default grants are explicitly revoked from `public`, `anon`,
  `authenticated`, and `service_role`.
- Only `SELECT (id)` is granted to `anon`.
- Redirects are never followed.
- Responses are limited to 64 KiB and must match exactly `[{"id":true}]`.
- Configuration references environment variables and contains no key values.
- Install and uninstall SQL fail closed when the heartbeat name belongs to an
  unrecognized object.

See [Security](docs/security.md), [Configuration](docs/configuration.md),
[GitHub Actions](docs/github-actions.md), and the
[Cloudflare Worker generator](docs/cloudflare.md) for details.

## Commands

```text
sb-heartbeat init
sb-heartbeat [--env-file PATH] run [--project NAME] [--output text|json] [--history PATH] [--metrics PATH] [--notification-state PATH --notification-webhook-env ENV --notify-after N]
sb-heartbeat [--env-file PATH] doctor [--project NAME] [--output text|json] [--history PATH] [--metrics PATH] [--notification-state PATH --notification-webhook-env ENV --notify-after N]
sb-heartbeat migration install|uninstall [--output PATH]
sb-heartbeat install github --sb-heartbeat-version VERSION
sb-heartbeat install cron [--binary-path PATH] [--log-path PATH]
sb-heartbeat --env-file PATH install launchd [--binary-path PATH] [--output-path PATH]
sb-heartbeat --env-file PATH install systemd [--binary-path PATH] [--service-output PATH] [--timer-output PATH]
sb-heartbeat install cloudflare [--output-dir PATH] [--worker-name NAME]
sb-heartbeat completion bash|zsh|fish|powershell
sb-heartbeat version
```

The stable exit codes are `0` for success, `1` for a failed heartbeat, `2` for
invalid input/configuration, and `3` for an internal CLI failure.

## Documentation

- [Quickstart](docs/quickstart.md)
- [Configuration](docs/configuration.md)
- [JSON Schema](schema/sb-heartbeat.schema.json)
- [Shell completions](docs/shell-completions.md)
- [Security model](docs/security.md)
- [GitHub Actions](docs/github-actions.md)
- [GitHub observability](docs/github-observability.md)
- [Local cron](docs/local-cron.md)
- [Local scheduler generators](docs/local-schedulers.md)
- [Cloudflare Worker generator](docs/cloudflare.md)
- [Local status history](docs/status-history.md)
- [Repeated-failure notifications](docs/notifications.md)
- [Prometheus metrics](docs/metrics.md)
- [Docker](docs/docker.md)
- [Agent installation](docs/agent-install.md)
- [Agent prompts](docs/agent-prompts.md)
- [Coding-agent installation evaluation](docs/agent-evaluation.md)
- [LLM and coding-agent index](llms.txt)
- [Troubleshooting](docs/troubleshooting.md)
- [Stop heartbeats and uninstall](docs/uninstall.md)
- [Release verification](docs/releasing.md)
- [Product specification](docs/product-spec.md)

## License

MIT
