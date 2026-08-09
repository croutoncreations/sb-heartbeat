#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 2 || $# -gt 3 || ( $# -eq 3 && $3 != "--exact" ) ]]; then
  echo "usage: verify-release-artifacts.sh ARTIFACT_DIRECTORY VERSION [--exact]" >&2
  exit 2
fi

artifact_directory=$1
version=$2
exact=false
if [[ $# -eq 3 ]]; then
  exact=true
fi
if [[ ! "${version}" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "release artifact version must be an exact semantic version without a v prefix" >&2
  exit 2
fi
if [[ ! -d "${artifact_directory}" || -L "${artifact_directory}" ]]; then
  echo "release artifact directory must be a real directory" >&2
  exit 1
fi

expected=(
  "sb-heartbeat_${version}_darwin_amd64.tar.gz"
  "sb-heartbeat_${version}_darwin_arm64.tar.gz"
  "sb-heartbeat_${version}_linux_amd64.tar.gz"
  "sb-heartbeat_${version}_linux_arm64.tar.gz"
  "sb-heartbeat_${version}_windows_amd64.zip"
  "sb-heartbeat_${version}_windows_arm64.zip"
)
checksum_path="${artifact_directory}/checksums.txt"
if [[ ! -f "${checksum_path}" || -L "${checksum_path}" ]]; then
  echo "checksums.txt must be a regular non-symlink file" >&2
  exit 1
fi

seen=$'\n'
count=0
checksum_pattern='^([0-9a-f]{64})  ([A-Za-z0-9._-]+)$'
while IFS= read -r line || [[ -n "${line}" ]]; do
  count=$((count + 1))
  if [[ ! "${line}" =~ ${checksum_pattern} ]]; then
    echo "checksums.txt contains an invalid entry" >&2
    exit 1
  fi
  filename=${BASH_REMATCH[2]}
  recognized=false
  for expected_name in "${expected[@]}"; do
    if [[ "${filename}" == "${expected_name}" ]]; then
      recognized=true
      break
    fi
  done
  if [[ "${recognized}" != true || "${seen}" == *$'\n'"${filename}"$'\n'* ]]; then
    echo "checksums.txt contains an unexpected or duplicate artifact" >&2
    exit 1
  fi
  seen="${seen}${filename}"$'\n'
done < "${checksum_path}"

if [[ ${count} -ne ${#expected[@]} ]]; then
  echo "checksums.txt does not contain the exact release artifact set" >&2
  exit 1
fi

for expected_name in "${expected[@]}"; do
  path="${artifact_directory}/${expected_name}"
  if [[ ! -f "${path}" || -L "${path}" || "${seen}" != *$'\n'"${expected_name}"$'\n'* ]]; then
    echo "release artifact set is incomplete or contains a symlink" >&2
    exit 1
  fi
done

for path in "${artifact_directory}"/sb-heartbeat_*.tar.gz "${artifact_directory}"/sb-heartbeat_*.zip; do
  [[ -e "${path}" || -L "${path}" ]] || continue
  filename=${path##*/}
  if [[ "${seen}" != *$'\n'"${filename}"$'\n'* ]]; then
    echo "release artifact directory contains an unexpected archive" >&2
    exit 1
  fi
done

if [[ "${exact}" == true ]]; then
  asset_count=0
  for path in "${artifact_directory}"/* "${artifact_directory}"/.[!.]* "${artifact_directory}"/..?*; do
    [[ -e "${path}" || -L "${path}" ]] || continue
    filename=${path##*/}
    recognized=false
    if [[ "${filename}" == checksums.txt ]]; then
      recognized=true
    else
      for expected_name in "${expected[@]}"; do
        if [[ "${filename}" == "${expected_name}" ]]; then
          recognized=true
          break
        fi
      done
    fi
    if [[ "${recognized}" != true || ! -f "${path}" || -L "${path}" ]]; then
      echo "release artifact directory does not contain the exact asset set" >&2
      exit 1
    fi
    asset_count=$((asset_count + 1))
  done
  if [[ ${asset_count} -ne $(( ${#expected[@]} + 1 )) ]]; then
    echo "release artifact directory does not contain the exact asset set" >&2
    exit 1
  fi
fi

if ! (cd "${artifact_directory}" && sha256sum --check --strict checksums.txt >/dev/null); then
  echo "release artifact checksum verification failed" >&2
  exit 1
fi

echo "Release artifacts: PASS"
