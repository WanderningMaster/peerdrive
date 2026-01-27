#!/usr/bin/env bash

# Run churn experiments for e2e and e2e-local with controlled sweeps
# - Baseline with defaults
# - Sweep keep-local (e2e-churn only)
# - Sweep chunk size
# - Sweep replicas

set -u

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR" || exit 1

timestamp() { date +"%Y-%m-%d_%H-%M-%S"; }

REPORT_DIR="$ROOT_DIR/reports"
mkdir -p "$REPORT_DIR"
OUT="$REPORT_DIR/churn_experiments_$(timestamp).log"

echo "Writing results to: $OUT"

log() { echo -e "$*" | tee -a "$OUT"; }

run_case() {
  local title="$1"; shift
  log "\n===== $title ====="
  log "cmd: $*"
  # Run and capture both stdout and stderr without aborting the whole script
  # even if a particular run fails.
  if "$@" 2>&1 | tee -a "$OUT"; then
    log "status: OK"
  else
    log "status: FAILED (exit=$?)"
  fi
}

# Helper to run go programs from this repo
go_e2e_churn() { go run ./cmd/e2e-churn "$@"; }
go_e2e_local_churn() { go run ./cmd/e2e-local-churn "$@"; }

# Baseline runs (defaults)
# run_case "baseline: e2e-churn (defaults)" go_e2e_churn
# run_case "baseline: e2e-local-churn (defaults)" go_e2e_local_churn

# Parameter sweeps (one-at-a-time vs baseline)

# 1) keep-local (only for e2e-churn)
# for KEEP in 0.20 0.50 0.90; do
#   run_case "sweep keep-local=${KEEP}: e2e-churn" go_e2e_churn --keep-local "${KEEP}"
# done

# 2) chunk size (bytes)
for CHUNK in 262144 524288 1048576; do # 256KiB, 512KiB
  run_case "sweep chunk=${CHUNK}: e2e-churn" go_e2e_churn --chunk "${CHUNK}" --keep-local=0.5
  run_case "sweep chunk=${CHUNK}: e2e-local-churn" go_e2e_local_churn --chunk "${CHUNK}"
done
for CHUNK in 262144 524288 1048576; do # 256KiB, 512KiB
  run_case "sweep chunk=${CHUNK}: e2e-churn" go_e2e_churn --chunk "${CHUNK}" --size 5242880 --keep-local=0.5
  run_case "sweep chunk=${CHUNK}: e2e-local-churn" go_e2e_local_churn --chunk "${CHUNK}" --size 5242880
done

log "\nAll runs complete. Report: $OUT"
