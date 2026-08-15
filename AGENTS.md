# Contributor instructions for coding agents

These instructions apply to development of SB Heartbeat itself. They must not be
copied into repositories where SB Heartbeat is installed.

- Read `docs/product-spec.md` before changing behavior.
- Use tests first for every behavior-bearing change.
- Run the full normal, race, and vet suites before committing a phase.
- Never add real Supabase URLs, publishable keys, anon keys, secret keys,
  service-role keys, `.env` files, or captured response bodies.
- Never weaken redirect rejection, host validation, response limits, fixed-query
  behavior, collision guards, explicit grants, or elevated-key rejection.
- Migration commands generate SQL only; they never apply it.
- Generated workflows use exact versions, pinned action SHAs, and checksums.
- Keep README, docs, release examples, and `llms.txt` synchronized. Run the
  documentation tests and relative-link audit before a release.
- Do not create or modify a downstream repository's `AGENTS.md`, `CLAUDE.md`,
  or equivalent instruction file.
- Commit only after tests and a blocking review pass succeed.
