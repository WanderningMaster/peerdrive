#!/usr/bin/env bash

# Run final distributed approach experiments for cmd/e2e-final
# Sweeps:
# - keep-local fraction
# - chunk size
# - file size
# Auto-computes --mutate per run to change ~MUTATE_FRAC of chunks.

set -u

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR" || exit 1

timestamp() { date +"%Y-%m-%d_%H-%M-%S"; }

REPORT_DIR="$ROOT_DIR/reports"
mkdir -p "$REPORT_DIR"
OUT="$REPORT_DIR/final_experiments_$(timestamp).log"

echo "Writing results to: $OUT"

log() { echo -e "$*" | tee -a "$OUT"; }

run_case() {
  local title="$1"; shift
  log "\n===== $title ====="
  log "cmd: $*"
  if "$@" 2>&1 | tee -a "$OUT"; then
    log "status: OK"
  else
    log "status: FAILED (exit=$?)"
  fi
}

# Helper to run go program
go_e2e_final() { go run ./cmd/e2e-final "$@"; }

# Defaults for runs (overridable via env)
NODES="${NODES:-25}"
TRIALS="${TRIALS:-150}"
FETCH_PAR="${FETCH_PAR:-16}"
REUSE_BASE="${REUSE_BASE:-true}"
# Target fraction of chunks mutated per upload (0<f<1). 0.10 = 10%.
MUTATE_FRAC="${MUTATE_FRAC:-0.40}"

# Compute mutate count for given size/chunk to touch ~MUTATE_FRAC of chunks
# m = ceil(-n * ln(1 - f)), where n=ceil(size/chunk)
mutate_for() {
  local size="$1"; local chunk="$2"; local frac="${3:-$MUTATE_FRAC}"
  if [[ -z "$size" || -z "$chunk" || "$chunk" -le 0 ]]; then
    echo 1; return
  fi
  local n=$(( (size + chunk - 1) / chunk ))
  if [[ "$n" -le 0 ]]; then echo 1; return; fi
  awk -v n="$n" -v f="$frac" 'BEGIN{ m = n * (-log(1.0 - f)); if (m<1) m=1; printf "%d\n", int(m+0.999999); }'
}

# 2) chunk size sweep at size=2MiB and size=5MiB
# for CHUNK in 262144 524288 1048576; do # 256KiB, 512KiB, 1MiB
#   for SIZE in 2097152 5242880 20971520; do # 2MiB, 5MiB
#     MUTE=$(mutate_for "$SIZE" "$CHUNK")
#     run_case "sweep chunk=${CHUNK} size=${SIZE}: e2e-final (mutate=${MUTE})" \
#       go_e2e_final --nodes "$NODES" --trials "$TRIALS" --fetch-par "$FETCH_PAR" \
#         --chunk "$CHUNK" --size "$SIZE" --keep-local 0.50 --reuse-base="${REUSE_BASE}" --mutate="${MUTE}"
#   done
# done

for FETCH_PARALLEL in 10 20; do
    SIZE=5242880
    CHUNK=262144
    MUTE=$(mutate_for "$SIZE" "$CHUNK")
    run_case "sweep chunk=${CHUNK} size=${SIZE}: e2e-final (mutate=${MUTE})" \
      go_e2e_final --nodes "$NODES" --trials "$TRIALS" --fetch-par "$FETCH_PARALLEL" \
        --chunk 262144 --size 5242880 --keep-local 0.50 --reuse-base="${REUSE_BASE}" --mutate="${MUTE}" --nodes=50
done

log "\nAll runs complete. Report: $OUT"
