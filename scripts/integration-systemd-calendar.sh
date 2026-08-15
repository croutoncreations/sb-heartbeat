#!/usr/bin/env bash
set -euo pipefail

case "$(uname -s)" in
  Linux|Darwin) ;;
  *) echo "systemd calendar smoke requires Linux or Docker Desktop on macOS" >&2; exit 1 ;;
esac
if [[ "$#" != 1 || ! -x "$1" ]]; then
  echo "usage: $0 /absolute/path/to/sb-heartbeat" >&2
  exit 2
fi

sb_heartbeat_binary="$(cd "$(dirname "$1")" && pwd)/$(basename "$1")"
integration_dir=""
container_name=""
image_name=""
container_id_path=""
image_id_path=""
timeout_seconds="${SB_HEARTBEAT_CALENDAR_TIMEOUT_SECONDS:-240}"

# shellcheck disable=SC2329 # Invoked through the EXIT trap.
cleanup() {
  if [[ -n "${container_id_path}" && -s "${container_id_path}" ]]; then
    container_id="$(<"${container_id_path}")"
    if [[ "${container_id}" =~ ^[0-9a-f]{64}$ ]]; then
      docker rm --force "${container_id}" >/dev/null 2>&1 || true
    fi
  fi
  if [[ -n "${image_id_path}" && -s "${image_id_path}" ]]; then
    image_id="$(<"${image_id_path}")"
    if [[ "${image_id}" =~ ^sha256:[0-9a-f]{64}$ ]]; then
      tagged_image_id="$(docker image inspect --format '{{.Id}}' "${image_name}" 2>/dev/null || true)"
      if [[ "${tagged_image_id}" == "${image_id}" ]]; then
        docker image rm --force "${image_name}" >/dev/null 2>&1 || true
      fi
    fi
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

integration_dir="$(mktemp -d "${TMPDIR:-/tmp}/sb-heartbeat-systemd-calendar.XXXXXXXX")"
container_id_path="${integration_dir}/container.id"
image_id_path="${integration_dir}/image.id"
run_id="$(basename "${integration_dir}" | tr -cd 'A-Za-z0-9')"
container_name="sb-heartbeat-systemd-${run_id}"
image_name="sb-heartbeat-systemd-smoke:${run_id}"
unit_name="sb-heartbeat-calendar-${run_id}"
config_path="${integration_dir}/sb-heartbeat.yaml"
env_path="${integration_dir}/heartbeat.env"
service_user_path="${integration_dir}/service-user.conf"
marker="systemd-calendar-delivery-${run_id}"
ubuntu_image="ubuntu:24.04@sha256:561618e2c15bf2397621dd04f96926663a3b5616c189cf7e38db7e82f5c538ea"

if docker container inspect "${container_name}" >/dev/null 2>&1 || docker image inspect "${image_name}" >/dev/null 2>&1; then
  echo "systemd calendar smoke refuses to reuse existing temporary resources" >&2
  exit 1
fi

cat >"${integration_dir}/Dockerfile" <<EOF
FROM ${ubuntu_image}
ENV container=docker
RUN apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install --yes --no-install-recommends systemd dbus libpam-systemd && rm -rf /var/lib/apt/lists/* && useradd --create-home --uid 2000 calendar-smoke
STOPSIGNAL SIGRTMIN+3
CMD ["/usr/lib/systemd/systemd"]
EOF
docker build --iidfile "${image_id_path}" --tag "${image_name}" "${integration_dir}"

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

target_schedule="$(python3 - <<'PY'
from datetime import datetime, timedelta, timezone
target = datetime.now(timezone.utc) + timedelta(minutes=2)
print(target.strftime('%M %H * * *'))
PY
)"
python3 - "${config_path}" "${target_schedule}" <<'PY'
from pathlib import Path
import sys

path = Path(sys.argv[1])
text = path.read_text()
path.write_text(text.replace('0 0 * * *', sys.argv[2]))
PY

docker run --cidfile "${container_id_path}" --detach --privileged --cgroupns=host --name "${container_name}" "${image_name}" >/dev/null
for _ in $(seq 1 30); do
  if docker exec "${container_name}" systemctl is-system-running --wait >/dev/null 2>&1; then
    break
  fi
  sleep 1
done
docker exec "${container_name}" systemctl is-system-running --wait >/dev/null

cat >"${integration_dir}/calendar-probe" <<EOF
#!/bin/sh
set -eu
echo '${marker}'
EOF
chmod 755 "${integration_dir}/calendar-probe"
docker exec "${container_name}" mkdir -p /home/calendar-smoke/smoke /home/calendar-smoke/.config/systemd/user
docker cp "${sb_heartbeat_binary}" "${container_name}:/home/calendar-smoke/smoke/sb-heartbeat"
docker cp "${config_path}" "${container_name}:/home/calendar-smoke/smoke/sb-heartbeat.yaml"
docker cp "${env_path}" "${container_name}:/home/calendar-smoke/smoke/heartbeat.env"
docker cp "${integration_dir}/calendar-probe" "${container_name}:/usr/local/bin/calendar-probe"
docker exec "${container_name}" chown -R 2000:2000 /home/calendar-smoke
docker exec "${container_name}" chmod 600 /home/calendar-smoke/smoke/heartbeat.env
docker exec --user 2000:2000 "${container_name}" \
  /home/calendar-smoke/smoke/sb-heartbeat \
  --config /home/calendar-smoke/smoke/sb-heartbeat.yaml \
  --env-file /home/calendar-smoke/smoke/heartbeat.env \
  install systemd \
  --binary-path /usr/local/bin/calendar-probe \
  --service-output "/home/calendar-smoke/.config/systemd/user/${unit_name}.service" \
  --timer-output "/home/calendar-smoke/.config/systemd/user/${unit_name}.timer"
docker exec "${container_name}" grep -q '^OnCalendar=' "/home/calendar-smoke/.config/systemd/user/${unit_name}.timer"

# GitHub's nested container kernel cannot apply capability hardening from an
# unprivileged user manager. Load the exact generated units into the disposable
# system manager and use a drop-in to keep the scheduled probe non-root.
docker exec "${container_name}" grep -qx 'CapabilityBoundingSet=' \
  "/home/calendar-smoke/.config/systemd/user/${unit_name}.service"
printf '%s\n' '[Service]' 'User=calendar-smoke' >"${service_user_path}"
docker exec "${container_name}" mkdir -p \
  "/etc/systemd/system/${unit_name}.service.d"
docker exec "${container_name}" cp \
  "/home/calendar-smoke/.config/systemd/user/${unit_name}.service" \
  "/etc/systemd/system/${unit_name}.service"
docker exec "${container_name}" cp \
  "/home/calendar-smoke/.config/systemd/user/${unit_name}.timer" \
  "/etc/systemd/system/${unit_name}.timer"
docker cp "${service_user_path}" \
  "${container_name}:/etc/systemd/system/${unit_name}.service.d/smoke-user.conf"
system_systemctl=(docker exec "${container_name}" systemctl)
"${system_systemctl[@]}" daemon-reload
"${system_systemctl[@]}" enable --now "${unit_name}.timer"

deadline=$((SECONDS + timeout_seconds))
while (( SECONDS < deadline )); do
  if docker exec "${container_name}" journalctl --quiet "_SYSTEMD_UNIT=${unit_name}.service" --grep "${marker}" | grep -q "${marker}"; then
    echo "systemd calendar delivery: PASS"
    exit 0
  fi
  sleep 2
done

echo "systemd calendar delivery was not observed within ${timeout_seconds} seconds" >&2
"${system_systemctl[@]}" status "${unit_name}.service" "${unit_name}.timer" >&2 || true
docker exec "${container_name}" journalctl "_SYSTEMD_UNIT=${unit_name}.service" >&2 || true
exit 1
