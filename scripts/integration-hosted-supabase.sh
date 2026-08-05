#!/usr/bin/env bash
set -euo pipefail

if [[ "${SB_HEARTBEAT_REQUIRE_HOSTED:-}" != "1" ]]; then
  echo "Refusing to run without SB_HEARTBEAT_REQUIRE_HOSTED=1" >&2
  exit 2
fi
for required_name in \
  SB_HEARTBEAT_HOSTED_DATABASE_URL \
  SB_HEARTBEAT_HOSTED_URL \
  SB_HEARTBEAT_HOSTED_PUBLISHABLE_KEY \
  SB_HEARTBEAT_HOSTED_ANON_KEY; do
  if [[ -z "${!required_name:-}" ]]; then
    echo "Required hosted integration input is missing: ${required_name}" >&2
    exit 2
  fi
done
if [[ $# -ne 1 || ! -x "$1" ]]; then
  echo "usage: integration-hosted-supabase.sh /absolute/path/to/sb-heartbeat" >&2
  exit 2
fi

binary="$1"
psql_command=(psql --no-psqlrc --set ON_ERROR_STOP=1 --quiet "${SB_HEARTBEAT_HOSTED_DATABASE_URL}")

fixture_metadata_check="$("${psql_command[@]}" --tuples-only --no-align <<'SQL'
select count(*) = 1 and bool_and(
  obj_description(n.oid, 'pg_namespace') = 'sb-heartbeat:release-fixture:v1'
  and c.relkind = 'r'
  and obj_description(c.oid, 'pg_class') = 'sb-heartbeat:release-fixture:v1'
  and (
    select count(*) = 1
      and bool_and(a.attname = 'marker' and a.atttypid = 'text'::regtype and a.attnotnull)
    from pg_attribute a
    where a.attrelid = c.oid
      and a.attnum > 0
      and not a.attisdropped
  )
  and (
    select count(*) = 2 and bool_and(
      (k.conname = 'sb_heartbeat_release_fixture_pkey' and k.contype = 'p')
      or (k.conname = 'sb_heartbeat_release_fixture_value' and k.contype = 'c')
    )
    from pg_constraint k
    where k.conrelid = c.oid
  )
)
from pg_class c
join pg_namespace n on n.oid = c.relnamespace
where n.nspname = 'sb_heartbeat_release'
  and c.relname = 'fixture';
SQL
)"
if [[ "${fixture_metadata_check}" != "t" ]]; then
  echo "Refusing to use a project without the dedicated SB Heartbeat release fixture marker." >&2
  exit 2
fi
fixture_row_check="$("${psql_command[@]}" --tuples-only --no-align --command "select count(*) = 1 and bool_and(marker = 'sb-heartbeat:release-fixture:v1') from sb_heartbeat_release.fixture;")"
if [[ "${fixture_row_check}" != "t" ]]; then
  echo "Refusing to use a project without the dedicated SB Heartbeat release fixture marker." >&2
  exit 2
fi

temporary_directory="$(mktemp -d)"
config_path="${temporary_directory}/sb-heartbeat.yaml"

cleanup() {
  "${binary}" migration install | "${psql_command[@]}" >/dev/null 2>&1 || true
  rm -f "${config_path}" >/dev/null 2>&1 || true
  rmdir "${temporary_directory}" >/dev/null 2>&1 || true
}
trap cleanup EXIT

wait_for_restored_heartbeat() {
  local attempt
  local last_output=""
  for attempt in 1 2 3 4 5 6; do
    if last_output="$(
      SB_HEARTBEAT_HOSTED_KEY="${SB_HEARTBEAT_HOSTED_PUBLISHABLE_KEY}" \
        "${binary}" --config "${config_path}" run --output text 2>&1
    )"; then
      return 0
    fi
    if [[ "${attempt}" != "6" ]]; then
      echo "Hosted integration: waiting for the restored heartbeat table (${attempt}/6)" >&2
      sleep 2
    fi
  done
  echo "Hosted integration: restored heartbeat table did not become healthy" >&2
  printf '%s\n' "${last_output}" >&2
  return 1
}

"${binary}" migration uninstall | "${psql_command[@]}"

"${binary}" init --non-interactive \
  --project-name hosted-release-test \
  --url-env SB_HEARTBEAT_HOSTED_URL \
  --api-key-env SB_HEARTBEAT_HOSTED_KEY \
  --output-path "${config_path}" >/dev/null

"${binary}" migration install | "${psql_command[@]}"
"${binary}" migration install | "${psql_command[@]}"

grant_check="$("${psql_command[@]}" --tuples-only --no-align --command "select has_column_privilege('anon','public.sb_heartbeat','id','select'), has_column_privilege('anon','public.sb_heartbeat','created_at','select'), has_column_privilege('authenticated','public.sb_heartbeat','id','select'), has_column_privilege('service_role','public.sb_heartbeat','id','select');")"
mutation_check="$("${psql_command[@]}" --tuples-only --no-align --command "select has_table_privilege('anon','public.sb_heartbeat','insert'), has_any_column_privilege('anon','public.sb_heartbeat','insert'), has_table_privilege('anon','public.sb_heartbeat','update'), has_any_column_privilege('anon','public.sb_heartbeat','update'), has_table_privilege('anon','public.sb_heartbeat','delete'), has_table_privilege('authenticated','public.sb_heartbeat','insert'), has_any_column_privilege('authenticated','public.sb_heartbeat','insert'), has_table_privilege('authenticated','public.sb_heartbeat','update'), has_any_column_privilege('authenticated','public.sb_heartbeat','update'), has_table_privilege('authenticated','public.sb_heartbeat','delete'), has_table_privilege('service_role','public.sb_heartbeat','insert'), has_any_column_privilege('service_role','public.sb_heartbeat','insert'), has_table_privilege('service_role','public.sb_heartbeat','update'), has_any_column_privilege('service_role','public.sb_heartbeat','update'), has_table_privilege('service_role','public.sb_heartbeat','delete');")"
[[ "${grant_check}" == "t|f|f|f" ]]
[[ "${mutation_check}" == "f|f|f|f|f|f|f|f|f|f|f|f|f|f|f" ]]

SB_HEARTBEAT_HOSTED_KEY="${SB_HEARTBEAT_HOSTED_PUBLISHABLE_KEY}" \
  "${binary}" --config "${config_path}" run --output json >/dev/null
SB_HEARTBEAT_HOSTED_KEY="${SB_HEARTBEAT_HOSTED_ANON_KEY}" \
  "${binary}" --config "${config_path}" run --output json >/dev/null

"${binary}" migration uninstall | "${psql_command[@]}"
[[ "$("${psql_command[@]}" --tuples-only --no-align --command "select to_regclass('public.sb_heartbeat') is null;")" == "t" ]]
"${binary}" migration install | "${psql_command[@]}"
wait_for_restored_heartbeat

echo "Hosted Supabase integration: PASS"
