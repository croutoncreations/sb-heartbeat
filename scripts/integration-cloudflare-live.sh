#!/usr/bin/env bash
set -euo pipefail

mode="${1:-}"
script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repository_root="$(cd "${script_dir}/.." && pwd)"
prepared_dir=""
integration_dir=""
worker_name=""
ownership_marker=""
dev_pid=""

usage() {
  echo "usage: $0 prepare /absolute/path/to/sb-heartbeat /absolute/prepared-dir" >&2
  echo "       $0 live /absolute/prepared-dir" >&2
  exit 2
}

prepare() {
  if [[ "$#" != 2 || ! -x "$1" || "$2" != /* || -e "$2" ]]; then
    usage
  fi
  local binary="$1"
  prepared_dir="$2"
  local config_path
  config_path="$(mktemp "${TMPDIR:-/tmp}/sb-heartbeat-cloudflare-config.XXXXXXXX")"
  trap 'rm -f "${config_path}"' RETURN

  cat >"${config_path}" <<'YAML'
version: 1
projects:
  - name: publishable-fixture
    url:
      env: PUBLISHABLE_FIXTURE_URL
    api_key:
      env: PUBLISHABLE_FIXTURE_KEY
  - name: anon-fixture
    url:
      env: ANON_FIXTURE_URL
    api_key:
      env: ANON_FIXTURE_KEY
scheduler:
  cron: "* * * * *"
YAML

  "${binary}" --config "${config_path}" install cloudflare \
    --output-dir "${prepared_dir}" --worker-name sb-heartbeat-live-contract
  cp "${repository_root}/internal/scheduler/testdata/cloudflare-live-package/package-lock.json" \
    "${prepared_dir}/package-lock.json"
  (
    cd "${prepared_dir}"
    npm ci --ignore-scripts --no-audit --no-fund
    npm test -- --run
    npm run check
  )
}

require_live_inputs() {
  local name
  for name in \
    SB_HEARTBEAT_CLOUDFLARE_ACCOUNT_ID \
    SB_HEARTBEAT_CLOUDFLARE_API_TOKEN \
    SB_HEARTBEAT_CLOUDFLARE_PUBLISHABLE_URL \
    SB_HEARTBEAT_CLOUDFLARE_PUBLISHABLE_KEY \
    SB_HEARTBEAT_CLOUDFLARE_ANON_URL \
    SB_HEARTBEAT_CLOUDFLARE_ANON_KEY; do
    if [[ -z "${!name:-}" ]]; then
      echo "Cloudflare live integration requires ${name}" >&2
      exit 2
    fi
  done
  if [[ "${SB_HEARTBEAT_CLOUDFLARE_PUBLISHABLE_KEY}" != sb_publishable_* ]]; then
    echo "Cloudflare live integration requires an actual publishable-key fixture" >&2
    exit 2
  fi
  if ! python3 - <<'PY'
import base64
import json
import os
import re
import sys

key = os.environ["SB_HEARTBEAT_CLOUDFLARE_ANON_KEY"]
parts = key.split(".")
if len(parts) != 3 or any(re.fullmatch(r"[A-Za-z0-9_-]+", part) is None for part in parts):
    sys.exit(1)
try:
    payload = base64.b64decode(parts[1] + "=" * (-len(parts[1]) % 4), altchars=b"-_", validate=True)
    claims = json.loads(payload)
except (ValueError, UnicodeDecodeError, json.JSONDecodeError):
    sys.exit(1)
sys.exit(0 if isinstance(claims, dict) and claims.get("role") == "anon" else 1)
PY
  then
    echo "Cloudflare live integration requires an actual legacy anon-key fixture" >&2
    exit 2
  fi
  if ! python3 - <<'PY'
import os
import re
import sys

pattern = re.compile(r"https://([a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)\.supabase\.co/?")
origins = []
for name in ("SB_HEARTBEAT_CLOUDFLARE_PUBLISHABLE_URL", "SB_HEARTBEAT_CLOUDFLARE_ANON_URL"):
    value = os.environ[name]
    match = pattern.fullmatch(value)
    if match is None:
        sys.exit(1)
    origins.append(f"https://{match.group(1)}.supabase.co")
sys.exit(0 if origins[0] != origins[1] else 1)
PY
  then
    echo "Cloudflare live integration requires two distinct hosted Supabase projects" >&2
    exit 2
  fi
}

api_get() {
  local path="$1"
  local output="$2"
  curl --silent --show-error --output "${output}" --write-out '%{http_code}' \
    --header "Authorization: Bearer ${CLOUDFLARE_API_TOKEN}" \
    "https://api.cloudflare.com/client/v4/accounts/${CLOUDFLARE_ACCOUNT_ID}/workers/scripts/${worker_name}${path}"
}

# shellcheck disable=SC2329 # Invoked through the EXIT trap.
cleanup() {
  local status=$?
  trap - EXIT
  if [[ -n "${dev_pid}" ]]; then
    kill "${dev_pid}" >/dev/null 2>&1 || true
    wait "${dev_pid}" >/dev/null 2>&1 || true
  fi
  if [[ -n "${integration_dir}" && -n "${worker_name}" && -n "${ownership_marker}" ]]; then
    local script_copy="${integration_dir}/cleanup-worker"
    local http_code
    http_code="$(api_get "" "${script_copy}" || true)"
    if [[ "${http_code}" == "200" ]]; then
      if grep -F -- "${ownership_marker}" "${script_copy}" >/dev/null; then
        if ! npx --no-install wrangler delete "${worker_name}" --force >/dev/null; then
          echo "Cloudflare live integration could not delete its owned Worker" >&2
          status=1
        fi
      else
        echo "Cloudflare live integration could not prove Worker ownership; refusing cleanup" >&2
        status=1
      fi
    elif [[ "${http_code}" != "404" ]]; then
      echo "Cloudflare live integration could not determine whether cleanup was required" >&2
      status=1
    fi
  fi
  if [[ -n "${integration_dir}" && -d "${integration_dir}" ]]; then
    rm -r "${integration_dir}"
  fi
  exit "${status}"
}

live() {
  if [[ "$#" != 1 || "$1" != /* || ! -d "$1" ]]; then
    usage
  fi
  prepared_dir="$1"
  require_live_inputs

  export CLOUDFLARE_ACCOUNT_ID="${SB_HEARTBEAT_CLOUDFLARE_ACCOUNT_ID}"
  export CLOUDFLARE_API_TOKEN="${SB_HEARTBEAT_CLOUDFLARE_API_TOKEN}"
  integration_dir="$(mktemp -d "${TMPDIR:-/tmp}/sb-heartbeat-cloudflare-live.XXXXXXXX")"
  local run_id
  run_id="$(openssl rand -hex 16)"
  worker_name="sb-heartbeat-live-${run_id}"
  ownership_marker="sb-heartbeat-live-ownership:${run_id}"
  local secrets_path="${integration_dir}/worker-secrets.env"
  local api_result="${integration_dir}/api-result"
  local tail_log="${integration_dir}/deployed-tail.log"
  trap cleanup EXIT
  trap 'exit 130' INT
  trap 'exit 143' TERM

  printf '\n// %s\n' "${ownership_marker}" >>"${prepared_dir}/src/index.ts"
  cat >"${secrets_path}" <<EOF
PUBLISHABLE_FIXTURE_URL=${SB_HEARTBEAT_CLOUDFLARE_PUBLISHABLE_URL}
PUBLISHABLE_FIXTURE_KEY=${SB_HEARTBEAT_CLOUDFLARE_PUBLISHABLE_KEY}
ANON_FIXTURE_URL=${SB_HEARTBEAT_CLOUDFLARE_ANON_URL}
ANON_FIXTURE_KEY=${SB_HEARTBEAT_CLOUDFLARE_ANON_KEY}
EOF
  chmod 600 "${secrets_path}"

  cd "${prepared_dir}"
  local http_code
  http_code="$(api_get "" "${api_result}")"
  if [[ "${http_code}" != "404" ]]; then
    echo "Cloudflare live integration could not prove the unique Worker name is unused" >&2
    exit 1
  fi

  npx --no-install wrangler deploy --strict --name "${worker_name}" \
    --secrets-file "${secrets_path}" >/dev/null

  http_code="$(api_get "" "${api_result}")"
  if [[ "${http_code}" != "200" ]] || ! grep -F -- "${ownership_marker}" "${api_result}" >/dev/null; then
    echo "Cloudflare live integration could not verify its ownership marker after deployment" >&2
    exit 1
  fi

  http_code="$(api_get "/subdomain" "${api_result}")"
  if [[ "${http_code}" != "200" ]] ||
    ! jq -e '.success == true and .result.enabled == false and .result.previews_enabled == false' \
      "${api_result}" >/dev/null; then
    echo "Cloudflare live integration found public deployed routing" >&2
    exit 1
  fi

  http_code="$(api_get "/schedules" "${api_result}")"
  if [[ "${http_code}" != "200" ]] ||
    ! jq -e '(.result.schedules // .result) as $s | ($s | length) == 1 and $s[0].cron == "* * * * *"' \
      "${api_result}" >/dev/null; then
    echo "Cloudflare live integration did not find the exact deployed Cron Trigger" >&2
    exit 1
  fi

  http_code="$(api_get "/settings" "${api_result}")"
  if [[ "${http_code}" != "200" ]] ||
    ! jq -e '[.result.bindings[] | select(.type == "secret_text") | .name] as $names |
      ["PUBLISHABLE_FIXTURE_URL", "PUBLISHABLE_FIXTURE_KEY", "ANON_FIXTURE_URL", "ANON_FIXTURE_KEY"] |
      all(. as $required | $names | index($required))' "${api_result}" >/dev/null; then
    echo "Cloudflare live integration did not find every deployed secret binding" >&2
    exit 1
  fi

  # Observe an actual deployed Cron Trigger. Cloudflare documents up to 15 minutes
  # for a new trigger to propagate, so the release gate waits a bounded 17 minutes.
  npx --no-install wrangler tail "${worker_name}" --format json >"${tail_log}" 2>&1 &
  dev_pid=$!
  local delivered=0
  for _ in $(seq 1 204); do
    if jq -s -e '
      [.. | strings] as $strings |
      any($strings[]; contains("\"success\":true")) and
      any($strings[]; contains("\"name\":\"publishable-fixture\",\"status\":\"healthy\"")) and
      any($strings[]; contains("\"name\":\"anon-fixture\",\"status\":\"healthy\""))
    ' "${tail_log}" >/dev/null 2>&1; then
      delivered=1
      break
    fi
    if ! kill -0 "${dev_pid}" 2>/dev/null; then
      break
    fi
    sleep 5
  done
  if [[ "${delivered}" != "1" ]]; then
    echo "Cloudflare actual deployed Cron Trigger did not report both healthy fixtures" >&2
    exit 1
  fi

  echo "Cloudflare publishable/anon multi-project live integration: PASS"
}

case "${mode}" in
  prepare)
    shift
    prepare "$@"
    ;;
  live)
    shift
    live "$@"
    ;;
  *)
    usage
    ;;
esac
