# GitHub observability

Generated workflows always write the bounded SB Heartbeat JSON envelope to the
GitHub job summary. Two stricter observability features are disabled by default
and can be enabled independently:

```bash
sb-heartbeat install github \
  --sb-heartbeat-version v0.2.0 \
  --github-annotations \
  --github-artifact-retention-days 7
```

`--github-annotations` emits one GitHub error annotation for each unhealthy
project using only its configured project name and stable status. Invocation
failures emit only the stable failure code. Annotations never interpolate an
upstream or CLI error message.

Workflow-level failures use the stable codes `workflow_missing_result` when no
result file exists and `observability_sanitization_failed` when the result
cannot be reduced to the documented safe schema.

Before either surface is produced, the workflow strictly validates one complete
schema-v1 result envelope, including scalar types, ranges, project-name grammar,
stable status/code allowlists, and success consistency. Annotation values are
workflow-command escaped as defense in depth. The artifact is atomically
published to its final temporary path only after validation succeeds, so a
failed sanitizer cannot leave a file for the upload step.

`--github-artifact-retention-days` accepts 1 through 90; `0` leaves upload
disabled. The retained JSON contains timestamps, overall success, project name,
stable status, HTTP status, latency, and attempt count. For invocation failures
it contains only schema version, success, and the stable error code. It never
contains URLs, environment names or values, API keys, headers, response bodies,
or error messages.

The workflow creates the sanitized file in the runner's temporary directory,
outside the checkout, and gives the upload step no project bindings. Artifact
availability and deletion follow the repository's GitHub Actions access and
retention policies. Treat even sanitized operational timing and status data as
repository information when choosing retention.

Uploads use the official `actions/upload-artifact` action pinned to reviewed
commit `043fb46d1a93c77aae656e7c1c64a875d1fc6a0a` (`v7.0.1`). This optional upload
targets GitHub.com and the generated `ubuntu-latest` runner. GitHub Enterprise
Server does not support the modern artifact backend used by this action; leave
artifact retention disabled there. Built-in annotations do not require the
upload action.

Re-run generation with the same options and `--force` when replacing an
existing workflow. Combined `init --scheduler github` accepts the same two
flags. The heartbeat execution remains compatible with exact `v0.1.1`; these
features are workflow-side processing and do not add network or database
authority to SB Heartbeat.
