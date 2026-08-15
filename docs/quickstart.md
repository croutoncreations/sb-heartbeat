# Quickstart

## 1. Install an exact release

```bash
go install github.com/croutoncreations/sb-heartbeat/cmd/sb-heartbeat@v0.3.3
```

Prebuilt archives and checksums are available from the
[GitHub releases page](https://github.com/croutoncreations/sb-heartbeat/releases/tag/v0.3.3).
Verify downloaded release assets as described in
[Release verification](releasing.md).

## 2. Initialize

```bash
sb-heartbeat init \
  --non-interactive \
  --project-name demo \
  --migration-output supabase/migrations/20260815000000_sb-heartbeat.sql
```

Interactive `sb-heartbeat init` asks for the same non-secret metadata. It never asks
for an API-key value. Use `--url-env` and `--api-key-env` only to override the
derived `SB_HEARTBEAT_DEMO_URL` and `SB_HEARTBEAT_DEMO_API_KEY` names.
Inside a Git repository, the wizard suggests a normalized project label from
the repository directory. It shows whether each derived binding belongs in a
GitHub variable or secret, can collect additional Supabase projects, and
generates `sb-heartbeat.sql` beside the configuration by default. Review and
move that migration into the repository's established migration path when
needed. Initialization generates SQL but never applies it.

## 3. Review and apply SQL

Review the migration written by `init`, then apply it through your established
Supabase migration process. SB Heartbeat deliberately has no command that
applies SQL. If initialization did not generate a migration, create one at an
explicit unused path with `sb-heartbeat migration install --output PATH`.

## 4. Set runtime values

```bash
export SB_HEARTBEAT_DEMO_URL=https://your-project-ref.supabase.co
export SB_HEARTBEAT_DEMO_API_KEY=sb_publishable_your_key
```

A legacy anon JWT works too. Do not use a secret or service-role key.

## 5. Diagnose and run

```bash
sb-heartbeat doctor
sb-heartbeat run
sb-heartbeat run --output json
```

`doctor` makes the same fixed read-only request as `run`. It does not attempt a
write to prove that writes are forbidden.
