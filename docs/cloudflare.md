# Cloudflare Worker generator

`sb-heartbeat install cloudflare` generates a dedicated, cron-only TypeScript
Worker project. It never deploys the Worker, runs Wrangler, or logs in to
Cloudflare, and it never stores credential values.

```bash
sb-heartbeat \
  --config sb-heartbeat.yaml \
  install cloudflare \
  --output-dir sb-heartbeat-cloudflare \
  --worker-name my-project-heartbeat
```

The generated project includes exact direct development-tool versions,
Wrangler configuration, TypeScript source, and executable Workers-runtime
tests. Run `npm install`, commit the resulting package lock in the repository
that owns the deployment, then run both `npm test` and `npm run check`.

## Security contract

Every configured URL and API-key binding is declared as a required Cloudflare
secret. Use only a publishable key or legacy anon key; a secret/service-role
key is rejected before any request. Local `.dev.vars*` and `.env*` files are
ignored by the generated project and must not be committed.

The Worker has only a scheduled handler. `workers_dev` and `preview_urls` are
both false, so deployment creates no public heartbeat endpoint. It accepts only
bare hosted Supabase HTTPS origins, rejects redirects, makes the fixed
read-only heartbeat query, caps responses at 64 KiB, validates the exact row,
honors valid `Retry-After` values only up to 30 seconds, and logs only sanitized
project status metadata. The handler disables Cloudflare platform retries so
only the configured, bounded SB Heartbeat retries can create extra requests.

Cloudflare Cron Triggers run in UTC. SB Heartbeat rewrites numeric POSIX
weekdays to names because Cloudflare uses different numeric weekday values. A
schedule that restricts both day-of-month and weekday is rejected rather than
risking changed semantics.

The generated contract stays within the Cloudflare free plan's 64-variable and
50-subrequest limits, including configured retries. With the default one retry,
that permits up to 25 projects in one Worker. Larger installations should be
split across generated Workers or deliberately adapted for a paid plan. The
generator also keeps the configured worst-case batches within 14 minutes,
leaving a one-minute margin under Cloudflare's scheduled invocation limit.

## Local one-off validation

Create a private `.dev.vars` containing each required `NAME=value`, set mode
`600`, and start the local runtime:

```bash
npm run dev
```

In another terminal, invoke Wrangler's local scheduled-event route:

```bash
curl "http://localhost:8787/cdn-cgi/handler/scheduled?format=json"
```

This development-only route does not enable `workers.dev` or a Preview URL in
production.

## Deliberate deployment

Review the generated source and `wrangler.jsonc`, authenticate to the intended
Cloudflare account, then upload the initial code and secrets together:

```bash
npx wrangler deploy --secrets-file .dev.vars
```

Delete the local secrets file when it is no longer needed. Cron changes can
take up to 15 minutes to propagate. Use Workers Logs or `npx wrangler tail` to
inspect sanitized scheduled results.
