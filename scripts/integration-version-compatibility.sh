#!/usr/bin/env bash
set -euo pipefail

if [[ "$#" != "2" || "$1" != /* || "$2" != /* ]]; then
	echo "usage: integration-version-compatibility.sh /absolute/path/to/v0.1.1/sb-heartbeat /absolute/path/to/current/sb-heartbeat" >&2
	exit 2
fi

legacy_binary="$1"
current_binary="$2"
integration_dir="$(mktemp -d "${TMPDIR:-/tmp}/sb-heartbeat-version-compatibility.XXXXXXXX")"
cleanup() {
	rm -r "${integration_dir}"
}
trap cleanup EXIT

"${current_binary}" init --non-interactive \
	--output-path "${integration_dir}/sb-heartbeat.yaml" \
	--project-name compatibility-check \
	--scheduler github \
	--workflow-output "${integration_dir}/current.yml" \
	--workflow-config sb-heartbeat.yaml \
	--sb-heartbeat-version v0.1.1 >/dev/null

if grep -q 'github:' "${integration_dir}/sb-heartbeat.yaml"; then
	echo "Version compatibility integration: default config exposes post-v0.1.1 fields" >&2
	exit 1
fi

"${legacy_binary}" \
	--config "${integration_dir}/sb-heartbeat.yaml" \
	install github \
	--sb-heartbeat-version v0.1.1 \
	--workflow-config sb-heartbeat.yaml \
	--output-path "${integration_dir}/legacy.yml" >/dev/null

diff -u "${integration_dir}/current.yml" "${integration_dir}/legacy.yml"
echo "Version compatibility integration: PASS"
