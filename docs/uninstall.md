# Stop heartbeats and uninstall

Stopping activity is a lifecycle operation, not a single destructive command.
SB Heartbeat never changes GitHub settings, edits a crontab, applies SQL, or
archives a Supabase project for you.

## Disable scheduling first

Prevent another heartbeat before cleaning up database objects or bindings.

For GitHub Actions:

- To stop one project while others remain, remove only that project from
  `sb-heartbeat.yaml`. Read the currently pinned exact version from the existing
  workflow, then regenerate it with that version:

  ```bash
  sb-heartbeat --config sb-heartbeat.yaml install github \
    --sb-heartbeat-version vX.Y.Z \
    --output-path .github/workflows/sb-heartbeat.yml \
    --force
  ```

  Review the config and workflow diff, confirm retained projects remain, and
  deploy both changes together.
- To stop all projects, disable or remove the SB Heartbeat workflow through the
  repository's normal review process.
- Confirm that no older workflow, fork, or other repository still schedules the
  same project.

For local cron, stopping one project means removing only that project from the
configuration; keep the shared cron entry for retained projects. When
stopping all projects, identify and manually remove the exact reviewed cron
entry. Do not remove unrelated entries. Check other scheduler accounts or
machines if the entry may have been installed elsewhere.

Run no further heartbeat as part of teardown. Observe at least one former
schedule window when practical if you need confidence that scheduling stopped.

## Remove unused bindings

After scheduling is disabled, inspect the config and generated workflow to
derive `variable` or `secret` storage independently for both the URL and API-key
binding. Omitted sources mean URL variable and API-key secret; explicit sources
may reverse either default. Remove a binding from the correct store only when
it is no longer used. A binding can be shared by another workflow, project, or
application; verify its consumers before deleting it. Never print a value while
checking usage.

If other projects remain in `sb-heartbeat.yaml`, retain every binding referenced
by those projects.

## Optionally remove the heartbeat object

First confirm that the chosen migration path deploys only to the identified
Supabase project. Then generate the guarded removal SQL into that project's
normal migration directory:

```bash
sb-heartbeat migration uninstall --output supabase/migrations/20260815000000_remove_sb-heartbeat.sql
```

This command generates SQL only; it does not apply the migration or connect to
Supabase. Review and apply it separately through the established migration
process and only with explicit authorization. The SQL removes the table only
when its ownership marker and v1 shape match. A same-named unrelated or
modified object causes an exception rather than a drop.

If the repository uses one migration stream for multiple Supabase projects, do
not add uninstall SQL to that shared migration stream: it would remove every
valid heartbeat object receiving the migration. Leave the tiny object in place
or establish a separately authorized, project-scoped database-change path.

Database cleanup is optional. If the project may return, retaining the tiny
guarded object can be reasonable. Supabase does not provide a separate archive
operation: depending on the project and plan, the deliberate choices are to
[pause a Free project](https://supabase.com/docs/guides/platform/free-project-pausing),
retain it, or externally verify a
[backup](https://supabase.com/docs/guides/platform/backups) before choosing to
[permanently delete the project](https://supabase.com/docs/guides/platform/delete-project).
Deletion is destructive; stopping a heartbeat neither authorizes nor performs
it.

## Verify the stopped state

- Confirm the project is absent from active SB Heartbeat configuration.
- Confirm the GitHub Actions workflow or local cron entry no longer targets it.
- Confirm unused bindings were removed without deleting shared values.
- If uninstall SQL was authorized and applied, retain the reviewed migration in
  repository history and record its normal deployment result.
- Document whether the Supabase project is retained, paused, backed up, or
  separately approved for deletion so another maintainer does not recreate the
  heartbeat by mistake.
