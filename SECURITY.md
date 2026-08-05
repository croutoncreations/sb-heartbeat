# Security policy

## Reporting a vulnerability

Please do not open a public issue for a suspected vulnerability involving key
exposure, request redirection, generated SQL permissions, workflow command
injection, or release integrity. Use GitHub's private vulnerability-reporting
feature for this repository. If it is not enabled, contact the maintainer
privately before publishing details.

Include the affected version, reproduction steps, impact, and any suggested
mitigation. Do not include live Supabase keys or production response bodies.

## Supported credentials

SB Heartbeat accepts publishable keys and legacy anon JWT keys. It intentionally
rejects `sb_secret_...` keys and legacy JWTs with the `service_role` claim.
Reports proposing elevated credentials for normal heartbeats will not be
accepted as feature requests.

## Scope

Security-sensitive areas include credential classification and redaction,
host parsing, redirect behavior, response bounds, SQL ownership guards,
generated workflow pinning, and atomic generated-file writes.
