# Security model

## Database access

The generated migration marks its table with `sb-heartbeat:managed:v1` and checks
the marker, relation type, columns, primary key, and check constraint before a
rerun or uninstall. A collision aborts without modifying the existing object.

The migration revokes direct table access from `public`, `anon`,
`authenticated`, and `service_role`, then grants only `SELECT (id)` to `anon`.
RLS permits the fixed `id = true` row. No write policy exists.

## Keys

Publishable and legacy anon keys are low-privilege client keys; actual data
access still depends on grants and RLS. SB Heartbeat recommends environment
variables and GitHub secrets as defense-in-depth, but repository owners decide
how to store public client keys in their own projects.

Elevated key forms are rejected before network access. SB Heartbeat never logs a
complete key or authorization header.

## HTTP boundary

The MVP accepts only a bare `https://<project-ref>.supabase.co` origin. It
rejects userinfo, ports, paths, queries, fragments, Unicode or uppercase hosts,
extra labels, and deceptive suffixes. Redirects are disabled.

The request method, table, fields, filter, and limit are fixed. Each attempt has
a timeout. Responses over 64 KiB, non-JSON content, redirects, extra fields,
multiple rows, and unexpected values fail closed.

## Diagnostic limits

A low-privilege request cannot prove whether a permission denial came from
schema exposure/`USAGE`, column grants, RLS, or Data API configuration. Doctor
reports observable classifications and likely causes without claiming elevated
introspection.
