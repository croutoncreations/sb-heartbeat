# GitHub Actions

Generate the workflow from a versioned SB Heartbeat release:

```bash
sb-heartbeat install github --sb-heartbeat-version v0.3.3
```

Apply the generated install migration and run `sb-heartbeat doctor` successfully
before enabling or manually dispatching the workflow. The scheduled workflow
holds only a low-privilege client key; it cannot and must not create database
objects.

The default target is `.github/workflows/sb-heartbeat.yml`. Existing files are not
replaced unless `--force` is explicit.

The generated workflow uses the path supplied to `--config`; combined `init`
uses its `--output-path`. Absolute paths are made repository-relative using the
generated `.github/workflows/` location. Use `--workflow-config` when the
workflow and configuration output paths do not reveal the intended repository
layout.

By default, for each configured project:

- Create the URL environment name as a GitHub Actions variable.
- Create the API-key environment name as a GitHub Actions secret.

The interactive initializer prints these exact names. A project named `my-app`
derives:

```text
GitHub variable: SB_HEARTBEAT_MY_APP_URL
GitHub secret:   SB_HEARTBEAT_MY_APP_API_KEY
```

Existing repositories may already use names such as `SUPABASE_URL`,
`NEXT_PUBLIC_SUPABASE_URL`, `VITE_SUPABASE_URL`, `SUPABASE_ANON_KEY`, or a
project-specific publishable-key name. Those bindings might instead live in a
hosting provider and not in GitHub. Inspect names without reading values:

```bash
gh variable list --json name --jq '.[].name'
gh secret list --json name --jq '.[].name'
```

GitHub variables are readable in repository settings and are not masked in
workflow logs. Prefer a secret for any API key, even a low-privilege publishable
or legacy anon key. Select `github: variable` for an API key only when that
visibility is a deliberate repository-owner choice.

To reuse existing GitHub names, enter them when the wizard offers the derived
defaults, or configure them explicitly:

```yaml
projects:
  - name: my-app
    url:
      env: NEXT_PUBLIC_SUPABASE_URL
      github: secret
    api_key:
      env: SUPABASE_ANON_KEY
      github: secret
```

The generated workflow will then reference `secrets.NEXT_PUBLIC_SUPABASE_URL`
and `secrets.SUPABASE_ANON_KEY`. Each binding's optional `github` field accepts
only `variable` or `secret`; omitted fields preserve the safe URL-variable and
API-key-secret defaults. The wizard prompts for both sources, and noninteractive
setup exposes `--url-github-source` and `--api-key-github-source`.
Any configuration that explicitly writes a `github` source requires a workflow
pinned to SB Heartbeat `v0.2.0` or newer, including an explicit spelling of a
default. The generator rejects older release pins instead of creating an
incompatible workflow. Generated default URL-variable and API-key-secret
configurations remain compatible with `v0.1.1` because they omit the source
fields.

## Add bindings with GitHub CLI

Repository URLs are not credentials and can be added as variables by default:

```bash
gh variable set SB_HEARTBEAT_MY_APP_URL \
  --body 'https://your-project-ref.supabase.co'
```

Add the publishable or legacy anon key through the CLI's protected prompt so it
does not appear in command history:

```bash
gh secret set SB_HEARTBEAT_MY_APP_API_KEY
```

When configuration deliberately reverses either source, use `gh secret set`
for a `secret` binding and `gh variable set` for a `variable` binding. Avoid
passing values in command arguments; use the protected prompt or standard input.

Do not add a database connection URL, database password, secret key, or
service-role key. Scheduled heartbeats need only the hosted project URL and one
low-privilege client key.

## Add bindings in the GitHub web interface

1. Open the downstream repository on GitHub and select **Settings**.
2. Select **Secrets and variables**, then **Actions**.
3. On **Variables** or **Secrets**, create each binding in the store printed by
   SB Heartbeat.
4. Repeat for each configured Supabase project.

The values belong in the downstream application repository, not in the SB
Heartbeat development repository.

The generated workflow checks out the default-branch configuration using a
commit-pinned action, downloads one exact SB Heartbeat release, verifies the archive
against `checksums.txt`, runs JSON checks, and writes the result to the job
summary. It grants the workflow token only `contents: read`, and exposes project
variables and low-privilege keys only to the heartbeat step.

When generated repeated-failure notifications are enabled, the workflow also
grants `actions: read` so it can validate restored state against the exact
preceding default-branch run. The webhook secret remains scoped only to the
heartbeat step. See [Repeated-failure notifications](notifications.md).

GitHub cron is UTC. Scheduled jobs can be delayed or dropped under load, and
public-repository schedules can be disabled after prolonged repository
inactivity. Keep `workflow_dispatch` for manual verification.

Optional sanitized annotations and retained result artifacts are documented in
[GitHub observability](github-observability.md).
