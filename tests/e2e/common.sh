#!/usr/bin/env bash
# Shared setup/logging helpers for DockVault E2E shell runners.

e2e_require_cmds() {
  local cmd
  for cmd in "$@"; do
    command -v "$cmd" >/dev/null 2>&1 || {
      echo "Missing required command: $cmd" >&2
      exit 1
    }
  done
}

e2e_build_image() {
  local tag="$1"
  local repo_root="$2"
  local log_file="$3"
  local start_ts elapsed

  start_ts=$(date +%s)
  printf "  ▶ Building image %s ... " "$tag"
  if docker build -t "$tag" "$repo_root" >"$log_file" 2>&1; then
    elapsed=$(( $(date +%s) - start_ts ))
    printf "ok (%ss)\n" "$elapsed"
  else
    printf "FAILED\n"
    echo "--- Docker build log ---" >&2
    cat "$log_file" >&2 || true
    return 1
  fi
}

e2e_wait_healthy() {
  local port="$1"
  local label="${2:-DockVault}"
  local ok=0
  printf "  ▶ Waiting for %s on :%s ... " "$label" "$port"
  for _ in $(seq 1 40); do
    if curl -sf "http://127.0.0.1:${port}/api/health" >/dev/null; then
      ok=1
      break
    fi
    sleep 0.5
  done
  if [[ "$ok" == "1" ]]; then
    printf "healthy\n"
  else
    printf "TIMEOUT\n" >&2
    return 1
  fi
}
