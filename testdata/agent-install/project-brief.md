# SB Heartbeat installation evaluation

Prepare SB Heartbeat for this disposable downstream repository using the
evaluator-provided binary. Before acting, read the published
[`docs/agent-install.md`](https://github.com/croutoncreations/sb-heartbeat/blob/main/docs/agent-install.md)
guide and follow the **Prepare an installation** task in
[`docs/agent-prompts.md`](https://github.com/croutoncreations/sb-heartbeat/blob/main/docs/agent-prompts.md).
The evaluator supplies local read-only copies of those exact documents. This is
an artifact-generation test, so the supplied binary replaces the guide's
release-install step.

- Project label: `evaluation-stage`
- URL environment binding: `EVALUATION_SUPABASE_URL`
- API-key environment binding: `EVALUATION_SUPABASE_API_KEY`
- Keep the default three-times-daily schedule.
- Generate a GitHub Actions workflow pinned to exact release version `v0.1.1`.
- Generate the guarded install migration in the repository's established
  migration directory and naming convention.

No credential values are available, and this task grants no authority to apply
SQL or change GitHub repository settings. Finish with the handoff required by
the published guide and prompt.
