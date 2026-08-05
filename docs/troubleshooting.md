# Troubleshooting

## `credential_rejected`

Use a current `sb_publishable_...` key or a legacy JWT whose role is `anon`.
Secret and service-role keys are intentionally unsupported.

## `api_authentication_failed`

Confirm the key belongs to the project URL and has not been disabled or
rotated.

## `database_permission_denied`

Possible causes include Data API schema exposure, schema `USAGE`, table or
column grants, or RLS. The low-privilege request cannot distinguish them. Review
the generated migration and your Supabase Data API settings.

## `no_matching_row`

Confirm the guarded migration was applied and that the fixed `id = true` row
still exists.

## `project_paused`

Resume the project from Supabase, wait for it to become available, and rerun
`sb-heartbeat doctor` manually.

## GitHub schedule did not run

Confirm the workflow exists on the default branch, Actions is enabled, required
variables/secrets exist, and the scheduled workflow has not been automatically
disabled. Use manual dispatch to test it.
