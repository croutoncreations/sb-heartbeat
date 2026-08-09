#!/usr/bin/env bash
set -euo pipefail

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
integration_dir="$(mktemp -d "${TMPDIR:-/tmp}/sb-heartbeat-docker.XXXXXXXX")"
run_id="$(basename "${integration_dir}")"
image="sb-heartbeat:integration-${run_id}"
builder="sb-heartbeat-integration-${run_id}"
buildkit_image="moby/buildkit:v0.23.1@sha256:dbc2dfd9342fd5c891ea94e9774c15cab985681e5ff995a9e366066aa0b9b2b4"
version="v0.0.0-test"
builder_created=0
image_created=0

cleanup() {
	if [[ "${builder_created}" == "1" ]]; then
		docker buildx rm "${builder}" >/dev/null 2>&1 || true
	fi
	if [[ "${image_created}" == "1" ]]; then
		docker image rm --force "${image}" >/dev/null 2>&1 || true
	fi
  rm -r "${integration_dir}"
}
trap cleanup EXIT

if docker image inspect "${image}" >/dev/null 2>&1 || docker buildx inspect "${builder}" >/dev/null 2>&1; then
	echo "Docker integration: refusing to reuse existing temporary resources" >&2
	exit 1
fi

docker build \
  --build-arg "VERSION=${version}" \
  --tag "${image}" \
  "${repository_root}"
image_created=1

configured_user="$(docker image inspect --format '{{.Config.User}}' "${image}")"
if [[ "${configured_user}" != "65532:65532" ]]; then
  echo "Docker integration: image user is ${configured_user}, want 65532:65532" >&2
  exit 1
fi

version_output="$(docker run --rm --read-only "${image}" version)"
if [[ "${version_output}" != "${version}" ]]; then
  echo "Docker integration: version output is ${version_output}" >&2
  exit 1
fi

docker run --rm --read-only "${image}" completion bash >/dev/null
docker run --rm --read-only "${image}" migration install >"${integration_dir}/install.sql"
if ! grep -q "sb-heartbeat:managed:v1" "${integration_dir}/install.sql"; then
  echo "Docker integration: migration output is incomplete" >&2
  exit 1
fi

docker buildx create \
	--name "${builder}" \
	--driver docker-container \
	--driver-opt "image=${buildkit_image}" >/dev/null
builder_created=1
docker buildx inspect "${builder}" --bootstrap >/dev/null

docker buildx build \
  --builder "${builder}" \
  --platform linux/amd64,linux/arm64 \
  --build-arg "VERSION=${version}" \
  --output "type=oci,dest=${integration_dir}/sb-heartbeat-multiarch.tar" \
  "${repository_root}"

test -s "${integration_dir}/sb-heartbeat-multiarch.tar"
python3 "${repository_root}/scripts/inspect-docker-oci.py" \
	"${integration_dir}/sb-heartbeat-multiarch.tar" "${version}"
echo "Docker integration: PASS"
