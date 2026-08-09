## Changed files

- `sb-heartbeat.yaml` — project `evaluation-stage`, required environment bindings, and default three-times-daily UTC schedule.
- `.github/workflows/sb-heartbeat.yml` — exact SB Heartbeat `v0.1.1`, checksum-verified release download, SHA-pinned checkout action, repository variable/secret bindings.
- `supabase/migrations/20260808000000_install_sb_heartbeat.sql` — guarded installation migration following the required naming convention.

Local artifact assertions passed. `AGENTS.md` and `TASK.md` remain unchanged. No credentials were used, SQL was not applied, network project checks were not run, repository settings were not changed, and no commit was created.

## Remaining manual steps

1. Review and apply the migration through the repository’s normal Supabase migration process.
2. Create GitHub repository variable `EVALUATION_SUPABASE_URL`.
3. Create GitHub repository secret `EVALUATION_SUPABASE_API_KEY` using only a low-privilege publishable/anon client key—never a secret or service-role key.
4. Once those values are available in the environment, run `sb-heartbeat doctor`, then one on-demand `sb-heartbeat run`.
5. Review, commit, and push the generated artifacts to enable the scheduled workflow.
