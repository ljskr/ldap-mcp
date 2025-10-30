#!/usr/bin/env bash

set -euo pipefail

# Build helper for producing cross-platform binaries under .build/targets.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
OUTPUT_DIR="${PROJECT_ROOT}/.build/targets"
BIN_NAME="ldap-mcp"

OS_TARGETS=${OS_TARGETS:-"linux darwin"}
ARCH_TARGETS=${ARCH_TARGETS:-"amd64 arm64"}
CGO_ENABLED=${CGO_ENABLED:-0}

if ! command -v go >/dev/null 2>&1; then
	echo "go toolchain not found in PATH" >&2
	exit 1
fi

mkdir -p "${OUTPUT_DIR}"

for os in ${OS_TARGETS}; do
	for arch in ${ARCH_TARGETS}; do
		artifact="${OUTPUT_DIR}/${BIN_NAME}-${os}-${arch}"
		echo "[build] ${os}/${arch} -> ${artifact}"
		GOOS="${os}" GOARCH="${arch}" CGO_ENABLED="${CGO_ENABLED}" \
			go build -o "${artifact}" "${PROJECT_ROOT}/cmd/server"
		chmod +x "${artifact}"
	done
done

echo "Artifacts stored in ${OUTPUT_DIR}" 
