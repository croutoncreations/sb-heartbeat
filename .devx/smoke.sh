#!/usr/bin/env bash
set -euo pipefail

project_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${project_dir}"

: "${POSTGRES:?DevX must provide a POSTGRES port}"
if [[ ! "${POSTGRES}" =~ ^[0-9]+$ ]] || ((POSTGRES < 1 || POSTGRES > 65535)); then
  echo "POSTGRES must be a valid TCP port" >&2
  exit 2
fi

for tool in go psql; do
  command -v "${tool}" >/dev/null 2>&1 || {
    echo "${tool} is required for the DevX smoke check" >&2
    exit 1
  }
done

runtime_dir="$(mktemp -d "${TMPDIR:-/tmp}/sb-heartbeat-devx.XXXXXXXX")"
# shellcheck disable=SC2329 # Invoked through the signal/exit trap.
cleanup() {
  rm -r "${runtime_dir}"
}
trap cleanup EXIT HUP INT TERM

binary="${runtime_dir}/sb-heartbeat"
database_url="postgres://postgres:postgres@127.0.0.1:${POSTGRES}/postgres?sslmode=disable"

go build -trimpath -o "${binary}" ./cmd/sb-heartbeat
"${binary}" version
"${binary}" completion bash >"${runtime_dir}/completion.bash"
"${binary}" migration install >"${runtime_dir}/install.sql"
grep -q 'sb-heartbeat:managed:v1' "${runtime_dir}/install.sql"

"${binary}" init \
  --non-interactive \
  --project-name devx-fixture \
  --output-path "${runtime_dir}/sb-heartbeat.yaml" \
  --migration-output "${runtime_dir}/generated-install.sql"

SB_HEARTBEAT_ALLOW_DISPOSABLE_DATABASE=1 \
SB_HEARTBEAT_TEST_DATABASE_URL="${database_url}" \
  scripts/integration-postgres.sh "${binary}"

echo "SB Heartbeat DevX smoke check: PASS"
