# Coding-agent installation evaluation

Last run: 2026-08-08

SB Heartbeat's published onboarding guidance was exercised by two fresh coding
agents in independent, disposable local Git repositories. Each agent read the
exact current [`agent-install.md`](agent-install.md) and **Prepare an
installation** prompt from [`agent-prompts.md`](agent-prompts.md), plus the same
minimal credential-free downstream fixture. The task supplied project metadata
but delegated safety boundaries and handoff behavior to the published guides.

The retained evidence is under
[`testdata/agent-install/results`](../testdata/agent-install/results). It includes
structured manifests, sanitized final responses, and one content-identical
snapshot of each generated artifact. Tests verify the manifests, hashes,
artifact semantics, result file set, and fixture bounds.

## Provenance

| Item | Value |
| --- | --- |
| Source revision | `657c1924e158d1ea0e1727445995f6c20ed8d39f` |
| Evaluator binary SHA-256 | `636c6d20baff6c2a06d364d937e82d03f5a345f6ab8c61b78a9af987ae1c7b28` |
| Published v0.1.1 binary SHA-256 | `79aea2eebe163c290d76136aedb4caa9bf1171b769f09bf76740cd5086f679dd` (Darwin arm64) |
| Fixture SHA-256 | `ffb4da34d323fa782dc43870deb17627f0ca18a60a26d33ea46dbbd45f8b61fd` |
| Agents | Agent A and Agent B, fresh `gpt-5.6-sol`, high reasoning |
| Isolation | Separate temporary Git repositories with identical initial trees |

The evaluator built the binary from the recorded source revision with
`-trimpath` and set its version to `v0.1.1` for workflow generation. Separately,
the published Darwin arm64 `v0.1.1` archive was verified against its published
checksum before its binary loaded each generated config and reproduced the
retained workflow byte-for-byte.

## Procedure

1. Build the evaluator binary:

   ```bash
   go build -trimpath \
     -ldflags '-s -w -X github.com/croutoncreations/sb-heartbeat/internal/cli.Version=v0.1.1' \
     -o /tmp/sb-heartbeat-agent-guide-eval ./cmd/sb-heartbeat
   sha256sum /tmp/sb-heartbeat-agent-guide-eval
   ```

2. Download the published `v0.1.1` archive for the evaluator platform, verify it
   against the release's `checksums.txt`, extract it, and record the binary
   hash.
3. For each fresh run, create a temporary Git repository, copy
   `downstream-instructions.txt` to `AGENTS.md`, copy `project-brief.md` to
   `TASK.md`, and create `supabase/migrations`. Commit only those two fixture
   files before dispatch.
4. Give the agent the repository path, evaluator binary path, and read-only
   local paths to the two published guides. Provide no conversation history,
   credentials, hosted URL, or additional installation checklist.
5. After the agent stops, compare `AGENTS.md` and `TASK.md` with the inputs;
   compare its migration byte-for-byte with `sb-heartbeat migration install`;
   run `actionlint` on its workflow; inspect strict configuration and binding
   sources; scan all non-Git files for credential patterns; and confirm the Git
   history still contains only the fixture commit. Run the checksum-verified
   published `v0.1.1` binary against the generated config and compare its
   workflow output byte-for-byte with the agent's workflow.
6. Record the source, evaluator binary, published binary, fixture, artifact,
   and sanitized-response hashes
   in a manifest. Retain credential-free artifacts and responses, then run:

   ```bash
   go test ./internal/documentation -run AgentInstallationEvaluation
   actionlint testdata/agent-install/results/artifacts/sb-heartbeat.yml
   diff -u <(go run ./cmd/sb-heartbeat migration install) \
     testdata/agent-install/results/artifacts/install.sql
   ```

Agent dispatch is intentionally not scripted: the evaluation requires two new
clean-context coding-agent sessions. The setup and verification steps are
otherwise reproducible from the retained fixture and procedure.

## Results

Both [Agent A](../testdata/agent-install/results/agent-a/manifest.json) and
[Agent B](../testdata/agent-install/results/agent-b/manifest.json) passed:

- existing instruction file unchanged;
- exactly one strict configuration, guarded migration, and pinned GitHub
  workflow generated in the requested locations;
- requested project name and environment binding names preserved;
- URL mapped from a GitHub variable and API key from a GitHub secret;
- migration generated but not applied;
- workflow used an exact release version, pinned action SHA, and checksum
  verification;
- the checksum-verified published `v0.1.1` binary parsed each generated config
  and reproduced its workflow exactly;
- no credential values requested or written, no `.env` file, and no network or
  project check attempted;
- no commit created; and
- final response listed every changed file and all remaining manual steps.

Agent A chose
`supabase/migrations/20260808235920_install_sb_heartbeat.sql`; Agent B chose
`supabase/migrations/20260808000000_install_sb_heartbeat.sql`. The contents were
identical and exact, while both filenames followed the downstream convention.
Their sanitized handoffs are retained beside their manifests.

Both agents left migration review/application, GitHub binding creation,
`doctor`, one on-demand heartbeat, and artifact review/commit to the user.

Re-run this evaluation when the published agent guidance, initialization,
migration output, or generated workflow behavior changes materially. Always use
new disposable repositories and fresh agents, and never introduce live
credentials into the fixture, artifacts, manifests, or report.
