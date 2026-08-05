# Uninstall

Generate the guarded removal SQL:

```bash
sb-heartbeat migration uninstall --output supabase/migrations/20260804_remove_sb-heartbeat.sql
```

Review and apply it through your normal migration process. The SQL removes the
table only when its ownership marker and v1 shape match. A same-named unrelated
or modified object causes an exception rather than a drop.

Then remove the project from `sb-heartbeat.yaml`, delete or regenerate the scheduler
workflow, and remove unused GitHub variables and secrets.
