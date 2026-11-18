#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

IMAGE_TAG="${DOCKVAULT_E2E_IMAGE:-dockvault:db-e2e-test}"
PORT="${DOCKVAULT_DB_E2E_PORT:-18089}"
PREFIX="${DOCKVAULT_DB_E2E_PREFIX:-dv-db-e2e-$$}"
SKIP_BUILD="${DOCKVAULT_E2E_SKIP_BUILD:-0}"
KEEP_IMAGE="${DOCKVAULT_E2E_KEEP_IMAGE:-0}"
ENGINES="${DOCKVAULT_DB_ENGINES:-postgres,mariadb,redis}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --image)
      IMAGE_TAG="$2"
      shift 2
      ;;
    --port)
      PORT="$2"
      shift 2
      ;;
    --engines)
      ENGINES="$2"
      shift 2
      ;;
    --skip-build)
      SKIP_BUILD=1
      shift
      ;;
    --keep-image)
      KEEP_IMAGE=1
      shift
      ;;
    -h|--help)
      cat <<EOF
Usage: $(basename "$0") [--image TAG] [--port PORT] [--engines postgres,mariadb,redis] [--skip-build] [--keep-image]

Runs end-to-end database backup, disaster corruption, volume restore, hot-dump restore,
and post-restore read/write health checks for PostgreSQL, MariaDB, and Redis.
EOF
      exit 0
      ;;
    *)
      echo "Unknown option: $1" >&2
      exit 2
      ;;
  esac
done

source "$SCRIPT_DIR/common.sh"
e2e_require_cmds docker python3 curl

WORKDIR="$(mktemp -d 2>/dev/null || mktemp -d -t dockvault-db-e2e)"
mkdir -p "$WORKDIR/data" "$WORKDIR/backups" "$WORKDIR/dumps"

DOCKVAULT_C="${PREFIX}-dockvault"
PG_C="${PREFIX}-pg"
PG_REC_C="${PREFIX}-pg-rec"
MARIADB_C="${PREFIX}-mariadb"
MARIADB_REC_C="${PREFIX}-mariadb-rec"
REDIS_C="${PREFIX}-redis"
REDIS_REC_C="${PREFIX}-redis-rec"

PG_V="${PREFIX}-pg-vol"
PG_REC_V="${PREFIX}-pg-rec-vol"
MARIADB_V="${PREFIX}-mariadb-vol"
MARIADB_REC_V="${PREFIX}-mariadb-rec-vol"
REDIS_V="${PREFIX}-redis-vol"
REDIS_REC_V="${PREFIX}-redis-rec-vol"

cleanup() {
  local exit_code=$?
  if [[ $exit_code -ne 0 ]]; then
    echo "--- DB E2E failed (exit $exit_code); DockVault container logs ---" >&2
    docker logs "$DOCKVAULT_C" >&2 || true
  fi
  docker rm -f \
    "$DOCKVAULT_C" \
    "$PG_C" "$PG_REC_C" \
    "$MARIADB_C" "$MARIADB_REC_C" \
    "$REDIS_C" "$REDIS_REC_C" \
    >/dev/null 2>&1 || true
  docker volume rm -f \
    "$PG_V" "$PG_REC_V" \
    "$MARIADB_V" "$MARIADB_REC_V" \
    "$REDIS_V" "$REDIS_REC_V" \
    >/dev/null 2>&1 || true
  if [[ "$SKIP_BUILD" != "1" && "$KEEP_IMAGE" != "1" ]]; then
    docker rmi "$IMAGE_TAG" >/dev/null 2>&1 || true
  fi
  rm -rf "$WORKDIR"
}
trap cleanup EXIT

if [[ "$SKIP_BUILD" != "1" ]]; then
  e2e_build_image "$IMAGE_TAG" "$REPO_ROOT" "$WORKDIR/build.log"
fi

printf "  ▶ Starting DockVault DB container (engines=%s) ... " "$ENGINES"
docker run -d --name "$DOCKVAULT_C" \
  -p "127.0.0.1:${PORT}:8080" \
  -e TZ=Europe/Berlin \
  -e DOCKVAULT_ENCRYPTION_KEY="db-e2e-secret" \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v "${WORKDIR}/data:/app/data" \
  -v "${WORKDIR}/backups:/app/backups" \
  "$IMAGE_TAG" >/dev/null
printf "ready\n"

e2e_wait_healthy "$PORT" "DockVault DB E2E"

DOCKVAULT_DB_E2E_URL="http://127.0.0.1:${PORT}/api" \
DOCKVAULT_DB_E2E_PREFIX="$PREFIX" \
DOCKVAULT_DB_E2E_WORKDIR="$WORKDIR" \
DOCKVAULT_DB_ENGINES="$ENGINES" \
  python3 "$SCRIPT_DIR/db_e2e_test.py"
