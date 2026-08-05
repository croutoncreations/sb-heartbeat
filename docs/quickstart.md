# Quickstart

## 1. Initialize

```bash
sb-heartbeat init \
  --non-interactive \
  --project-name demo \
  --url-env DEMO_SUPABASE_URL \
  --api-key-env DEMO_SUPABASE_API_KEY
```

Interactive `sb-heartbeat init` asks for the same non-secret metadata. It never asks
for an API-key value.

## 2. Generate and apply SQL

```bash
sb-heartbeat migration install --output supabase/migrations/20260804_sb-heartbeat.sql
```

Read the migration, then apply it through your established Supabase migration
process. SB Heartbeat deliberately has no command that applies SQL.

## 3. Set runtime values

```bash
export DEMO_SUPABASE_URL=https://your-project-ref.supabase.co
export DEMO_SUPABASE_API_KEY=sb_publishable_your_key
```

A legacy anon JWT works too. Do not use a secret or service-role key.

## 4. Diagnose and run

```bash
sb-heartbeat doctor
sb-heartbeat run
sb-heartbeat run --output json
```

`doctor` makes the same fixed read-only request as `run`. It does not attempt a
write to prove that writes are forbidden.
