# Agent prompts

These prompts are starting points for a coding agent working in an existing
repository. They do not authorize the agent to modify repository instruction
files, apply migrations, create secrets, or broaden the heartbeat query.

## Prepare an installation

```text
Read and follow this repository's existing AGENTS.md, CLAUDE.md, and other
instructions first. Prepare SB Heartbeat for the Supabase project I identify.
Use an exact, checksum-verified release. Configure environment-variable names,
never credential values. Generate the guarded install migration but do not
apply it. Do not modify existing repository instruction files, application
tables, RLS policies, or public endpoints. If I request GitHub Actions,
generate a pinned workflow and list the variables and secrets I must create.
Report every changed file and every manual step still required.
```

## Verify an installation

```text
Read this repository's instructions first. Inspect the SB Heartbeat config and
generated migration without printing environment values or authorization
headers. When the required low-privilege values are already available in the
environment, run doctor and one on-demand heartbeat. Never request or use a
secret/service-role key and never perform a mutation probe. Report the stable
status and likely causes for failures without claiming elevated database
introspection.
```

## Prepare local cron

```text
Read this repository's instructions first. Run `sb-heartbeat install cron` to
print a suggested entry; never edit the user's crontab. Confirm that all paths
are absolute, explain how the configured environment variables will reach the
scheduler without exposing values, and recommend protected logs plus exit-code
monitoring. Leave installation of the reviewed entry to the user.
```
