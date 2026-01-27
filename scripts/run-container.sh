#!/usr/bin/env bash
set -euo pipefail

# Start a peerdrive CLI container using per-node mounts in containers/<node_name>.
# Forwards all extra args to `peerdrive init` (container entrypoint).
#
# Usage:
#   scripts/run-container.sh <node_name> [--image <image>] [--ports] [--detach] [--name <container_name>] [--no-host-net] [--] [init flags...]
#
# Defaults:
# - image: peerdrive-cli
# - Linux: uses --network host unless --ports or --no-host-net is set
# - Non-Linux: maps ports (-p) from config.json

require_cmd() { command -v "$1" >/dev/null 2>&1 || { echo "Missing required command: $1" >&2; exit 1; }; }

if [[ ${1:-} == "" ]]; then
  echo "Usage: $0 <node_name> [--image <image>] [--ports] [--detach] [--name <container_name>] [--no-host-net] [--] [init flags...]" >&2
  exit 1
fi

NODE_NAME="$1"; shift
IMAGE="peerdrive-cli"
DETACH=""
USE_HOST_NET="auto"   # auto|yes|no
MAP_PORTS="no"
CONTAINER_NAME="peerdrive-${NODE_NAME}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --image)
      IMAGE="${2:-}"; shift 2;;
    --detach|-d)
      DETACH="-d"; shift;;
    --name)
      CONTAINER_NAME="${2:-}"; shift 2;;
    --no-host-net)
      USE_HOST_NET="no"; shift;;
    --ports)
      MAP_PORTS="yes"; shift;;
    --)
      shift; break;;
    *)
      break;;
  esac
done

BASE_DIR="./containers/${NODE_NAME}"
CONF_DIR="${BASE_DIR}/config/peerdrive"
BLOCK_DIR="${BASE_DIR}/peerdrive"
CONF_PATH="${CONF_DIR}/config.json"

# Ensure seeded structure exists
if [[ ! -f "${CONF_PATH}" ]]; then
  echo "Config not found at ${CONF_PATH}; seeding..."
  require_cmd bash
  bash "$(dirname "$0")/seed-container.sh" "${NODE_NAME}"
fi
mkdir -p "${CONF_DIR}" "${BLOCK_DIR}"

# Determine networking mode
OS=$(uname -s || echo unknown)
if [[ "${OS}" != "Linux" ]]; then
  # Host network rarely available outside Linux; map ports
  MAP_PORTS="yes"
fi
if [[ "${USE_HOST_NET}" == "no" ]]; then
  MAP_PORTS="yes"
fi

PORT_FLAGS=()
if [[ "${MAP_PORTS}" == "yes" ]]; then
  # Read tcpPort and httpPort from config.json
  TCP_PORT=""
  HTTP_PORT=""
  if command -v jq >/dev/null 2>&1; then
    TCP_PORT=$(jq -r '.tcpPort' "${CONF_PATH}")
    HTTP_PORT=$(jq -r '.httpPort' "${CONF_PATH}")
  else
    # Fallback to Python
    require_cmd python3
    readarray -t READ_PORTS < <(python3 - <<PY
import json,sys
p=json.load(open(sys.argv[1]))
print(p.get('tcpPort',''))
print(p.get('httpPort',''))
PY
"${CONF_PATH}")
    TCP_PORT="${READ_PORTS[0]}"
    HTTP_PORT="${READ_PORTS[1]}"
  fi
  if [[ -z "${TCP_PORT}" || -z "${HTTP_PORT}" ]]; then
    echo "Could not determine ports from ${CONF_PATH}" >&2
    exit 1
  fi
  PORT_FLAGS=( -p "${TCP_PORT}:${TCP_PORT}" -p "${HTTP_PORT}:${HTTP_PORT}" )
fi

NET_FLAGS=()
if [[ "${MAP_PORTS}" != "yes" ]]; then
  NET_FLAGS=( --network host )
fi

echo "Running container ${CONTAINER_NAME} (image: ${IMAGE}) for node '${NODE_NAME}'"
set -x
docker run --rm ${DETACH} \
  --name "${CONTAINER_NAME}" \
  "${NET_FLAGS[@]}" \
  "${PORT_FLAGS[@]}" \
  -v "${CONF_DIR}:/root/.config/peerdrive" \
  -v "${BLOCK_DIR}:/root/.peerdrive" \
  "${IMAGE}" "$@"
set +x
