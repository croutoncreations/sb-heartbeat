# GitHub Actions

Generate the workflow from a versioned SB Heartbeat release:

```bash
sb-heartbeat install github --sb-heartbeat-version v0.1.0
```

The default target is `.github/workflows/sb-heartbeat.yml`. Existing files are not
replaced unless `--force` is explicit.

The generated workflow uses the path supplied to `--config`; combined `init`
uses its `--output-path`. Absolute paths are made repository-relative using the
generated `.github/workflows/` location. Use `--workflow-config` when the
workflow and configuration output paths do not reveal the intended repository
layout.

For each configured project:

- Create the URL environment name as a GitHub Actions variable.
- Create the API-key environment name as a GitHub Actions secret.

The generated workflow checks out the default-branch configuration using a
commit-pinned action, downloads one exact SB Heartbeat release, verifies the archive
against `checksums.txt`, runs JSON checks, and writes the result to the job
summary. It grants the workflow token only `contents: read`, and exposes project
variables and low-privilege keys only to the heartbeat step.

GitHub cron is UTC. Scheduled jobs can be delayed or dropped under load, and
public-repository schedules can be disabled after prolonged repository
inactivity. Keep `workflow_dispatch` for manual verification.
