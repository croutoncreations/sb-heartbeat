Installation artifacts are prepared and verified offline with the supplied `v0.1.1` binary. No credentials were used, no network project checks or SQL application occurred, and nothing was committed.

## Changed files

- `sb-heartbeat.yaml` — project `evaluation-stage`, required environment bindings, and default three-times-daily schedule.
- `supabase/migrations/20260808235920_install_sb_heartbeat.sql` — guarded install migration with collision validation, RLS, explicit revocation, and select-only anon grant.
- `.github/workflows/sb-heartbeat.yml` — UTC schedule at 03:37, 11:37, and 19:37; exact `v0.1.1` release; pinned checkout SHA; checksum-verified download.

Offline checks confirmed that the migration exactly matches fresh generator output, expected bindings and safeguards are present, no credential-shaped values or `.env` file were added, and `AGENTS.md`/`TASK.md` remain unchanged.

## Remaining manual steps

1. Review and apply the generated migration through the repository’s normal Supabase migration process.
2. Create GitHub repository variable `EVALUATION_SUPABASE_URL`.
3. Create GitHub repository secret `EVALUATION_SUPABASE_API_KEY` using only a low-privilege publishable/anon API key—never a secret or service-role key.
4. Once those values are available in the environment, run `sb-heartbeat doctor`, followed by one on-demand `sb-heartbeat run`.
5. Review, commit, and push the three generated files to enable the workflow.
