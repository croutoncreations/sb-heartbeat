# Prometheus metrics

`run` and `doctor` can atomically replace a Prometheus text exposition file
after every completed check:

```bash
sb-heartbeat run --metrics /var/lib/node_exporter/textfile_collector/sb-heartbeat.prom
```

Point `--metrics` at a directory read by a Prometheus textfile collector, such
as the node exporter's textfile collector, or another consumer of the
Prometheus text format. SB Heartbeat writes the file with mode `0644`, creates
missing parent directories, and safely replaces a prior regular file. It
refuses to overwrite a symlink or non-regular target. The metrics path must
differ from the configuration, history, and notification-state paths. Metrics
export is unavailable on Windows because SB Heartbeat cannot guarantee atomic
replacement there; requesting it fails before network access.

The file describes only the latest completed run:

- `sb_heartbeat_run_success`: `1` when every selected project was healthy;
- `sb_heartbeat_run_timestamp_seconds`: Unix completion time;
- `sb_heartbeat_project_healthy`: `1` for a healthy project, otherwise `0`;
- `sb_heartbeat_project_status`: one current stable-status series per project;
- `sb_heartbeat_project_attempts`: HTTP attempts used by the check;
- `sb_heartbeat_project_latency_seconds`: total check latency, including retries and backoff, when available; and
- `sb_heartbeat_project_http_status`: HTTP response status when available.

Project names and the finite stable status set are the only labels. The file
does not contain project URLs, credentials, response bodies, webhook values, or
diagnostic messages. A failed heartbeat still writes its current metrics and
then returns the usual exit code `1`. An output-write failure returns exit code
`3`.

This option does not start an HTTP metrics server, push to a remote service, or
retain history. The surrounding collector owns scraping, access control, and
retention. Use `--history` separately when bounded local run history is needed.
