#!/usr/bin/env bash
set -euo pipefail

if [[ "$(uname -s)" != "Darwin" ]]; then
  echo "launchd calendar smoke requires macOS" >&2
  exit 1
fi
if [[ "$#" != 1 || ! -x "$1" ]]; then
  echo "usage: $0 /absolute/path/to/sb-heartbeat" >&2
  exit 2
fi

sb_heartbeat_binary="$(cd "$(dirname "$1")" && pwd)/$(basename "$1")"
integration_dir=""
loaded=0
timeout_seconds="${SB_HEARTBEAT_CALENDAR_TIMEOUT_SECONDS:-240}"
domain="gui/$(id -u)"

# shellcheck disable=SC2329 # Invoked through the EXIT trap.
cleanup() {
  if [[ "${loaded}" == "1" ]]; then
    launchctl bootout "${domain}/${label:-}" >/dev/null 2>&1 || true
  fi
  if [[ -n "${integration_dir}" && -d "${integration_dir}" ]]; then
    rm -r "${integration_dir}"
  fi
}
trap cleanup EXIT

if [[ ! "${timeout_seconds}" =~ ^[0-9]+$ ]] || (( timeout_seconds < 240 || timeout_seconds > 600 )); then
  echo "SB_HEARTBEAT_CALENDAR_TIMEOUT_SECONDS must be an integer from 240 through 600" >&2
  exit 2
fi

integration_dir="$(mktemp -d "${TMPDIR:-/tmp}/sb-heartbeat-launchd-calendar.XXXXXXXX")"
run_id="$(basename "${integration_dir}" | tr -cd 'A-Za-z0-9')"
label="io.github.croutoncreations.sb-heartbeat.calendar.${run_id}"
plist_path="${integration_dir}/${label}.plist"
marker_path="${integration_dir}/calendar-delivered"
stdout_path="${integration_dir}/stdout.log"
stderr_path="${integration_dir}/stderr.log"
config_path="${integration_dir}/sb-heartbeat.yaml"
env_path="${integration_dir}/heartbeat.env"
probe_path="${integration_dir}/calendar-probe"
cat >"${config_path}" <<'YAML'
version: 1
projects:
  - name: calendar-smoke
    url:
      env: CALENDAR_SMOKE_URL
    api_key:
      env: CALENDAR_SMOKE_API_KEY
scheduler:
  cron: "0 0 * * *"
YAML
printf '%s\n' 'CALENDAR_SMOKE_URL=unused' 'CALENDAR_SMOKE_API_KEY=unused' >"${env_path}"
chmod 600 "${env_path}"
cat >"${probe_path}" <<EOF
#!/bin/sh
set -eu
printf '%s\n' 'launchd calendar delivery observed' >'${marker_path}'
EOF
chmod 700 "${probe_path}"

target_schedule="$(date -v+2M '+%M %H * * *')"
python3 - "${config_path}" "${target_schedule}" <<'PY'
from pathlib import Path
import sys

path = Path(sys.argv[1])
text = path.read_text()
path.write_text(text.replace('0 0 * * *', sys.argv[2]))
PY

"${sb_heartbeat_binary}" \
  --config "${config_path}" \
  --env-file "${env_path}" \
  install launchd \
  --binary-path "${probe_path}" \
  --label "${label}" \
  --output-path "${plist_path}" \
  --stdout-path "${stdout_path}" \
  --stderr-path "${stderr_path}"

plutil -lint "${plist_path}" >/dev/null
grep -q 'StartCalendarInterval' "${plist_path}"
if launchctl bootstrap "${domain}" "${plist_path}"; then
  loaded=1
else
  if launchctl print "${domain}/${label}" 2>/dev/null | grep -F -- "${probe_path}" >/dev/null; then
    loaded=1
  fi
  echo "launchd refused the disposable calendar agent" >&2
  exit 1
fi

deadline=$((SECONDS + timeout_seconds))
while (( SECONDS < deadline )); do
  if [[ -s "${marker_path}" ]]; then
    echo "launchd calendar delivery: PASS"
    exit 0
  fi
  sleep 2
done

echo "launchd calendar delivery was not observed within ${timeout_seconds} seconds" >&2
launchctl print "${domain}/${label}" >&2 || true
exit 1
