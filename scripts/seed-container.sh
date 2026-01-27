#!/usr/bin/env bash
set -euo pipefail

if [[ ${1:-} == "" ]]; then
  echo "Usage: $0 <node_name> [tcp_port]" >&2
  exit 1
fi

NODE_NAME="$1"
TCP_PORT_INPUT="${2:-}"

BASE_DIR="containers/${NODE_NAME}"
CONF_DIR="${BASE_DIR}/.config/peerdrive"
BLOCK_DIR="${BASE_DIR}/.peerdrive"
CONF_PATH="${CONF_DIR}/config.json"

mkdir -p "${CONF_DIR}" "${BLOCK_DIR}"

# Decide TCP/HTTP ports
if [[ -n "${TCP_PORT_INPUT}" ]]; then
  TCP_PORT="${TCP_PORT_INPUT}"
else
  # Random in [30000, 30100]
  MIN=30000
  MAX=30100
  RANGE=$((MAX - MIN + 1))
  RAND=${RANDOM}
  TCP_PORT=$(( MIN + (RAND % RANGE) ))
fi
HTTP_PORT=$((8000 + (TCP_PORT % 1000)))

# Generate 32 random bytes as JSON array of 32 integers [0..255]
gen_node_id() {
  # portable: use /dev/urandom + od
  # shellcheck disable=SC2002
  BYTES=$(head -c 32 /dev/urandom | od -An -t u1 -w32 -v | tr -s ' ' | sed 's/^ //; s/ /,/g')
  echo "[${BYTES}]"
}

NODE_ID_JSON=$(gen_node_id)

# Do not overwrite existing config unless empty
if [[ -s "${CONF_PATH}" ]]; then
  echo "Config exists: ${CONF_PATH} (skipping)."
else
  cat >"${CONF_PATH}" <<JSON
{
  "nodeId": ${NODE_ID_JSON},
  "tcpPort": ${TCP_PORT},
  "httpPort": ${HTTP_PORT},
  "relay": "",
  "blockstorePath": "/root/.peerdrive",
  "acceptForeignBlocks": true
}
JSON
  echo "Wrote ${CONF_PATH}"
fi

echo "Ensured directories:"
echo " - ${CONF_DIR}"
echo " - ${BLOCK_DIR}"
