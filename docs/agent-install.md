# Agent installation guide

This is task guidance for installing SB Heartbeat. Do not modify the target
repository's `AGENTS.md`, `CLAUDE.md`, or equivalent instruction files.

1. Read the target repository's existing instructions.
2. Inspect its Supabase and migration conventions.
3. Install an exact SB Heartbeat release through a verified package or checksum.
4. Run `sb-heartbeat init --non-interactive` using environment-variable names, not
   key values.
5. Generate installation SQL. Do not apply it without the user's authorization
   and an established migration path.
6. Generate GitHub Actions only when requested, using an exact released version.
   For local cron, print the suggestion and let the user review and install it.
7. Tell the user which GitHub variables and secrets to create. Never request a
   secret or service-role key.
8. When values are available in the environment, run `sb-heartbeat doctor` and an
   on-demand `sb-heartbeat run`.
9. Summarize changed files and every remaining manual step.

Never select an application table, add a mutation, expose a public monitoring
endpoint, print a key, or commit an `.env` file. The MVP supports only the fixed
heartbeat object.

Copyable task prompts are available in [agent-prompts.md](agent-prompts.md).
