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

if [[ "$("${psql_command[@]}" --tuples-only --no-align --command "select to_regclass('public.sb_heartbeat') is null;")" != "t" ]]; then
  echo "Refusing to use a project that already contains public.sb_heartbeat; configure a dedicated disposable Supabase project for hosted release integration." >&2
  exit 2
fi

temporary_directory="$(mktemp -d)"
config_path="${temporary_directory}/sb-heartbeat.yaml"

cleanup() {
  "${binary}" migration uninstall | "${psql_command[@]}" >/dev/null 2>&1 || true
  rm -f "${config_path}" >/dev/null 2>&1 || true
  rmdir "${temporary_directory}" >/dev/null 2>&1 || true
}
trap cleanup EXIT

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

echo "Hosted Supabase integration: PASS"
