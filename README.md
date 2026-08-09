# SB Heartbeat

Keep intentionally retained, low-traffic Supabase projects active with a
least-privilege database heartbeat.

SB Heartbeat is a small Go CLI for GitHub Actions and local schedulers. It queries
one fixed row through the Supabase Data API using either a publishable key or a
legacy anon key. It does not require a secret key, service-role key, database
password, hosted account, or ongoing database writes.

> [!IMPORTANT]
> SB Heartbeat is best-effort. Supabase controls its project-activity classifier and
> can change it. Production projects should use an appropriate paid plan, and
> projects you no longer need should be archived rather than kept running.

## Quick start

```bash
go install github.com/croutoncreations/sb-heartbeat/cmd/sb-heartbeat@v0.1.1

sb-heartbeat init \
  --non-interactive \
  --project-name my-staging-project \
  --migration-output supabase/migrations/20260804_sb-heartbeat.sql
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

After a release exists, generate a pinned, checksum-verifying workflow:

```bash
sb-heartbeat install github --sb-heartbeat-version v0.1.1
```

For each project, create the configured URL as a GitHub Actions variable and
the API key as a GitHub Actions secret. The generated workflow runs at
`37 3,11,19 * * *` by default and supports manual dispatch.
Interactive initialization suggests the current repository name, displays the
exact derived binding names, and can collect multiple projects. Existing
GitHub binding names can be entered instead of the derived defaults.

For a local scheduler, `sb-heartbeat install cron` prints a shell-safe suggested
entry and the required environment-variable names. It never edits your crontab.

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

See [Security](docs/security.md), [Configuration](docs/configuration.md), and
[GitHub Actions](docs/github-actions.md) for details.

## Commands

```text
sb-heartbeat init
sb-heartbeat run [--project NAME] [--output text|json]
sb-heartbeat doctor [--project NAME] [--output text|json]
sb-heartbeat migration install|uninstall [--output PATH]
sb-heartbeat install github --sb-heartbeat-version VERSION
sb-heartbeat install cron [--binary-path PATH] [--log-path PATH]
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
- [Local cron](docs/local-cron.md)
- [Docker](docs/docker.md)
- [Agent installation](docs/agent-install.md)
- [Agent prompts](docs/agent-prompts.md)
- [Coding-agent installation evaluation](docs/agent-evaluation.md)
- [LLM and coding-agent index](llms.txt)
- [Troubleshooting](docs/troubleshooting.md)
- [Uninstall](docs/uninstall.md)
- [Release verification](docs/releasing.md)
- [Product specification](docs/product-spec.md)

## License

MIT
