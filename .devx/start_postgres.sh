#!/usr/bin/env bash
set -euo pipefail

: "${POSTGRES:?DevX must provide a POSTGRES port}"
if [[ ! "${POSTGRES}" =~ ^[0-9]+$ ]] || ((POSTGRES < 1 || POSTGRES > 65535)); then
  echo "POSTGRES must be a valid TCP port" >&2
  exit 2
fi

command -v docker >/dev/null 2>&1 || {
  echo "Docker is required for the disposable PostgreSQL service" >&2
  exit 1
}
docker info >/dev/null

readonly image='postgres:16.14-alpine3.24@sha256:57c72fd2a128e416c7fcc499958864df5301e940bca0a56f58fddf30ffc07777'
readonly container="sb-heartbeat-devx-${POSTGRES}"

if docker container inspect "${container}" >/dev/null 2>&1; then
  echo "Refusing to reuse existing container ${container}" >&2
  exit 1
fi

container_started=0
# shellcheck disable=SC2329 # Invoked through the signal/exit trap.
cleanup() {
  if [[ "${container_started}" == "1" ]]; then
    docker container stop --time 5 "${container}" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT HUP INT TERM

docker run --rm \
  --name "${container}" \
  --label io.github.croutoncreations.sb-heartbeat.devx=true \
  --publish "127.0.0.1:${POSTGRES}:5432" \
  --tmpfs /var/lib/postgresql/data:rw,nosuid,nodev,size=512m \
  --env POSTGRES_PASSWORD=postgres \
  --health-cmd 'pg_isready -U postgres' \
  --health-interval 1s \
  --health-timeout 5s \
  --health-retries 30 \
  "${image}" &
docker_pid=$!
container_started=1

for _ in {1..60}; do
  if ! kill -0 "${docker_pid}" >/dev/null 2>&1; then
    wait "${docker_pid}"
    exit $?
  fi
  if [[ "$(docker container inspect --format '{{.State.Health.Status}}' "${container}" 2>/dev/null || true)" == "healthy" ]]; then
    echo "Disposable PostgreSQL ready on 127.0.0.1:${POSTGRES} (${container})"
    wait "${docker_pid}"
    exit $?
  fi
  sleep 0.5
done

echo "Disposable PostgreSQL did not become healthy (${container})" >&2
exit 1
