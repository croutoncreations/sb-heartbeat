#!/usr/bin/env bash
set -euo pipefail

if [[ "${SB_HEARTBEAT_ALLOW_DISPOSABLE_DATABASE:-}" != "1" ]]; then
  echo "Refusing to run without SB_HEARTBEAT_ALLOW_DISPOSABLE_DATABASE=1" >&2
  exit 2
fi
if [[ -z "${SB_HEARTBEAT_TEST_DATABASE_URL:-}" ]]; then
  echo "SB_HEARTBEAT_TEST_DATABASE_URL is required" >&2
  exit 2
fi
if [[ $# -ne 1 || ! -x "$1" ]]; then
  echo "usage: integration-postgres.sh /absolute/path/to/sb-heartbeat" >&2
  exit 2
fi

binary="$1"
script_directory="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
psql_command=(psql --no-psqlrc --set ON_ERROR_STOP=1 --quiet "${SB_HEARTBEAT_TEST_DATABASE_URL}")
wrapper_directory="$(mktemp -d)"
failure_binary="${wrapper_directory}/sb-heartbeat-fail-run"
run_count_file="${wrapper_directory}/run-count"

cleanup_database() {
  "${psql_command[@]}" >/dev/null 2>&1 <<'SQL' || true
do $cleanup$
declare
  object_kind "char";
begin
  if to_regclass('public.sb_heartbeat') is not null then
    select relkind into object_kind
    from pg_class
    where oid = 'public.sb_heartbeat'::regclass;
    if object_kind = 'v' then
      drop view public.sb_heartbeat;
    else
      drop table public.sb_heartbeat;
    end if;
  end if;
end
$cleanup$;
drop role if exists anon;
drop role if exists authenticated;
drop role if exists service_role;
drop schema if exists sb_heartbeat_release cascade;
SQL
}

cleanup() {
  cleanup_database
  rm -f "${failure_binary}" >/dev/null 2>&1 || true
  rm -f "${run_count_file}" >/dev/null 2>&1 || true
  rmdir "${wrapper_directory}" >/dev/null 2>&1 || true
}
trap cleanup EXIT

cleanup_database
"${psql_command[@]}" <<'SQL'
create role anon;
create role authenticated;
create role service_role;
SQL

cat >"${failure_binary}" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
for argument in "$@"; do
  if [[ "${argument}" == "run" ]]; then
    if [[ "${SB_HEARTBEAT_TEST_RUN_MODE}" == "fail" ]]; then
      exit 42
    fi
    run_count=0
    if [[ -f "${SB_HEARTBEAT_TEST_RUN_COUNT_FILE}" ]]; then
      run_count="$(<"${SB_HEARTBEAT_TEST_RUN_COUNT_FILE}")"
    fi
    run_count=$((run_count + 1))
    printf '%s\n' "${run_count}" >"${SB_HEARTBEAT_TEST_RUN_COUNT_FILE}"
    if [[ "${run_count}" -le 4 ]]; then
      if [[ "${run_count}" -le 2 ]]; then
        exit 0
      fi
      exit 1
    fi
    exit 0
  fi
done
exec "${SB_HEARTBEAT_TEST_REAL_BINARY}" "$@"
SH
chmod +x "${failure_binary}"

"${binary}" migration install | "${psql_command[@]}"
"${binary}" migration install | "${psql_command[@]}"

row_check="$("${psql_command[@]}" --tuples-only --no-align --command "select count(*) = 1 and bool_and(id) from public.sb_heartbeat;")"
grant_check="$("${psql_command[@]}" --tuples-only --no-align --command "select has_column_privilege('anon','public.sb_heartbeat','id','select'), has_column_privilege('anon','public.sb_heartbeat','created_at','select'), has_column_privilege('authenticated','public.sb_heartbeat','id','select'), has_column_privilege('service_role','public.sb_heartbeat','id','select');")"
mutation_check="$("${psql_command[@]}" --tuples-only --no-align --command "select has_table_privilege('anon','public.sb_heartbeat','insert'), has_any_column_privilege('anon','public.sb_heartbeat','insert'), has_table_privilege('anon','public.sb_heartbeat','update'), has_any_column_privilege('anon','public.sb_heartbeat','update'), has_table_privilege('anon','public.sb_heartbeat','delete'), has_table_privilege('authenticated','public.sb_heartbeat','insert'), has_any_column_privilege('authenticated','public.sb_heartbeat','insert'), has_table_privilege('authenticated','public.sb_heartbeat','update'), has_any_column_privilege('authenticated','public.sb_heartbeat','update'), has_table_privilege('authenticated','public.sb_heartbeat','delete'), has_table_privilege('service_role','public.sb_heartbeat','insert'), has_any_column_privilege('service_role','public.sb_heartbeat','insert'), has_table_privilege('service_role','public.sb_heartbeat','update'), has_any_column_privilege('service_role','public.sb_heartbeat','update'), has_table_privilege('service_role','public.sb_heartbeat','delete');")"
marker_check="$("${psql_command[@]}" --tuples-only --no-align --command "select obj_description('public.sb_heartbeat'::regclass, 'pg_class');")"
[[ "${row_check}" == "t" ]]
[[ "${grant_check}" == "t|f|f|f" ]]
[[ "${mutation_check}" == "f|f|f|f|f|f|f|f|f|f|f|f|f|f|f" ]]
[[ "${marker_check}" == "sb-heartbeat:managed:v1" ]]
"${psql_command[@]}" --command "set role anon; select id from public.sb_heartbeat;" >/dev/null
if "${psql_command[@]}" --command "set role anon; select created_at from public.sb_heartbeat;" >/dev/null 2>&1; then
  echo "anon unexpectedly read created_at" >&2
  exit 1
fi
if "${psql_command[@]}" --command "set role anon; insert into public.sb_heartbeat(id) values (true);" >/dev/null 2>&1; then
  echo "anon unexpectedly inserted" >&2
  exit 1
fi

set +e
hosted_preflight_output="$(
  SB_HEARTBEAT_REQUIRE_HOSTED=1 \
  SB_HEARTBEAT_HOSTED_DATABASE_URL="${SB_HEARTBEAT_TEST_DATABASE_URL}" \
  SB_HEARTBEAT_HOSTED_URL="https://unused.invalid" \
  SB_HEARTBEAT_HOSTED_PUBLISHABLE_KEY="unused-publishable-fixture" \
  SB_HEARTBEAT_HOSTED_ANON_KEY="unused-anon-fixture" \
    "${script_directory}/integration-hosted-supabase.sh" "${binary}" 2>&1
)"
hosted_preflight_status=$?
set -e
[[ "${hosted_preflight_status}" -eq 2 ]]
[[ "${hosted_preflight_output}" == *"dedicated SB Heartbeat release fixture"* ]]
[[ "$("${psql_command[@]}" --tuples-only --no-align --command "select obj_description('public.sb_heartbeat'::regclass, 'pg_class');")" == "sb-heartbeat:managed:v1" ]]

"${psql_command[@]}" --file "${script_directory}/release-fixture-install.sql"
"${psql_command[@]}" --file "${script_directory}/release-fixture-install.sql"
fixture_check="$("${psql_command[@]}" --tuples-only --no-align --command "select count(*) = 1 and bool_and(marker = 'sb-heartbeat:release-fixture:v1') from sb_heartbeat_release.fixture;")"
fixture_grant_check="$("${psql_command[@]}" --tuples-only --no-align --command "select has_schema_privilege('anon','sb_heartbeat_release','usage'), has_schema_privilege('authenticated','sb_heartbeat_release','usage'), has_schema_privilege('service_role','sb_heartbeat_release','usage'), has_table_privilege('anon','sb_heartbeat_release.fixture','select'), has_table_privilege('authenticated','sb_heartbeat_release.fixture','select'), has_table_privilege('service_role','sb_heartbeat_release.fixture','select');")"
[[ "${fixture_check}" == "t" ]]
[[ "${fixture_grant_check}" == "f|f|f|f|f|f" ]]

set +e
SB_HEARTBEAT_TEST_REAL_BINARY="${binary}" \
SB_HEARTBEAT_TEST_RUN_MODE="fail" \
SB_HEARTBEAT_TEST_RUN_COUNT_FILE="${run_count_file}" \
SB_HEARTBEAT_REQUIRE_HOSTED=1 \
SB_HEARTBEAT_HOSTED_DATABASE_URL="${SB_HEARTBEAT_TEST_DATABASE_URL}" \
SB_HEARTBEAT_HOSTED_URL="https://unused.invalid" \
SB_HEARTBEAT_HOSTED_PUBLISHABLE_KEY="unused-publishable-fixture" \
SB_HEARTBEAT_HOSTED_ANON_KEY="unused-anon-fixture" \
  "${script_directory}/integration-hosted-supabase.sh" "${failure_binary}" >/dev/null 2>&1
hosted_failure_status=$?
set -e
[[ "${hosted_failure_status}" -eq 42 ]]
[[ "$("${psql_command[@]}" --tuples-only --no-align --command "select obj_description('public.sb_heartbeat'::regclass, 'pg_class');")" == "sb-heartbeat:managed:v1" ]]

hosted_retry_output="$(
  SB_HEARTBEAT_TEST_REAL_BINARY="${binary}" \
  SB_HEARTBEAT_TEST_RUN_MODE="delayed" \
  SB_HEARTBEAT_TEST_RUN_COUNT_FILE="${run_count_file}" \
  SB_HEARTBEAT_REQUIRE_HOSTED=1 \
  SB_HEARTBEAT_HOSTED_DATABASE_URL="${SB_HEARTBEAT_TEST_DATABASE_URL}" \
  SB_HEARTBEAT_HOSTED_URL="https://unused.invalid" \
  SB_HEARTBEAT_HOSTED_PUBLISHABLE_KEY="unused-publishable-fixture" \
  SB_HEARTBEAT_HOSTED_ANON_KEY="unused-anon-fixture" \
    "${script_directory}/integration-hosted-supabase.sh" "${failure_binary}" 2>&1
)"
[[ "${hosted_retry_output}" == *"waiting for the restored heartbeat table"* ]]
[[ "${hosted_retry_output}" == *"Hosted Supabase integration: PASS"* ]]
[[ "$(<"${run_count_file}")" -eq 5 ]]
[[ "$("${psql_command[@]}" --tuples-only --no-align --command "select obj_description('public.sb_heartbeat'::regclass, 'pg_class');")" == "sb-heartbeat:managed:v1" ]]

"${psql_command[@]}" --command "drop schema sb_heartbeat_release cascade; create schema sb_heartbeat_release; create table sb_heartbeat_release.fixture(note text); insert into sb_heartbeat_release.fixture values ('preserve-me');" >/dev/null
if "${psql_command[@]}" --file "${script_directory}/release-fixture-install.sql" >/dev/null 2>&1; then
  echo "release fixture install unexpectedly accepted an unrelated schema" >&2
  exit 1
fi
[[ "$("${psql_command[@]}" --tuples-only --no-align --command "select note from sb_heartbeat_release.fixture;")" == "preserve-me" ]]
"${psql_command[@]}" --command "drop schema sb_heartbeat_release cascade;" >/dev/null

"${binary}" migration uninstall | "${psql_command[@]}"

"${psql_command[@]}" --command "create table public.sb_heartbeat(note text); insert into public.sb_heartbeat values ('preserve-me');" >/dev/null
if "${binary}" migration install | "${psql_command[@]}" >/dev/null 2>&1; then
  echo "install unexpectedly accepted an unrelated table" >&2
  exit 1
fi
if "${binary}" migration uninstall | "${psql_command[@]}" >/dev/null 2>&1; then
  echo "uninstall unexpectedly accepted an unrelated table" >&2
  exit 1
fi
[[ "$("${psql_command[@]}" --tuples-only --no-align --command "select note from public.sb_heartbeat;")" == "preserve-me" ]]
"${psql_command[@]}" --command "drop table public.sb_heartbeat;" >/dev/null

"${psql_command[@]}" --command "create view public.sb_heartbeat as select true as id;" >/dev/null
if "${binary}" migration install | "${psql_command[@]}" >/dev/null 2>&1; then
  echo "install unexpectedly accepted a same-name view" >&2
  exit 1
fi
if "${binary}" migration uninstall | "${psql_command[@]}" >/dev/null 2>&1; then
  echo "uninstall unexpectedly accepted a same-name view" >&2
  exit 1
fi
"${psql_command[@]}" --command "drop view public.sb_heartbeat;" >/dev/null

"${psql_command[@]}" <<'SQL'
create table public.sb_heartbeat (id boolean primary key, created_at timestamptz);
comment on table public.sb_heartbeat is 'sb-heartbeat:managed:v1';
SQL
if "${binary}" migration install | "${psql_command[@]}" >/dev/null 2>&1; then
  echo "install unexpectedly accepted a malformed marked table" >&2
  exit 1
fi
if "${binary}" migration uninstall | "${psql_command[@]}" >/dev/null 2>&1; then
  echo "uninstall unexpectedly accepted a malformed marked table" >&2
  exit 1
fi
"${psql_command[@]}" --command "drop table public.sb_heartbeat;" >/dev/null

"${binary}" migration install | "${psql_command[@]}"
"${psql_command[@]}" --command "comment on table public.sb_heartbeat is 'sb-heartbeat:managed:forged';" >/dev/null
if "${binary}" migration install | "${psql_command[@]}" >/dev/null 2>&1; then
  echo "install unexpectedly accepted a forged ownership marker" >&2
  exit 1
fi
if "${binary}" migration uninstall | "${psql_command[@]}" >/dev/null 2>&1; then
  echo "uninstall unexpectedly accepted a forged ownership marker" >&2
  exit 1
fi
"${psql_command[@]}" --command "comment on table public.sb_heartbeat is 'sb-heartbeat:managed:v1';" >/dev/null
"${binary}" migration uninstall | "${psql_command[@]}"

echo "Disposable PostgreSQL integration: PASS"
