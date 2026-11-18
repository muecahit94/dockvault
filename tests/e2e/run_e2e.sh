#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

IMAGE_TAG="${DOCKVAULT_E2E_IMAGE:-dockvault:e2e-test}"
PORT="${DOCKVAULT_E2E_PORT:-18088}"
PREFIX="${DOCKVAULT_E2E_PREFIX:-dv-e2e-$$}"
SKIP_BUILD="${DOCKVAULT_E2E_SKIP_BUILD:-0}"
KEEP_IMAGE="${DOCKVAULT_E2E_KEEP_IMAGE:-0}"
WITH_DB="${DOCKVAULT_E2E_WITH_DB:-0}"
WITH_EDGE="${DOCKVAULT_E2E_WITH_EDGE:-0}"

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
    --skip-build)
      SKIP_BUILD=1
      shift
      ;;
    --keep-image)
      KEEP_IMAGE=1
      shift
      ;;
    --with-db)
      WITH_DB=1
      shift
      ;;
    --with-edge)
      WITH_EDGE=1
      shift
      ;;
    --all)
      WITH_DB=1
      WITH_EDGE=1
      shift
      ;;
    -h|--help)
      cat <<EOF
Usage: $(basename "$0") [--image TAG] [--port PORT] [--skip-build] [--keep-image] [--with-db] [--with-edge] [--all]

Spins up an isolated DockVault container and live Docker fixture containers/volumes,
runs tests/e2e/e2e_test.py (12 scenarios), optionally runs the database suite (--with-db)
and edge/chaos/cloud suite (--with-edge, or --all for both), and removes all temporary test resources on exit.
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

WORKDIR="$(mktemp -d 2>/dev/null || mktemp -d -t dockvault-e2e)"
mkdir -p "$WORKDIR/data" "$WORKDIR/backups" "$WORKDIR/bind_data"

DOCKVAULT_C="${PREFIX}-dockvault"
APP_C="${PREFIX}-app"
EXCLUDED_C="${PREFIX}-excluded"
STACK_NAME="${PREFIX}-stack"
WEB_C="${PREFIX}-stack-web"
API_C="${PREFIX}-stack-api"
REDIS_C="${PREFIX}-redis"

APP_V="${PREFIX}-app-vol"
ORPHAN_V="${PREFIX}-orphan-vol"
DELETE_V="${PREFIX}-delete-vol"
REDIS_V="${PREFIX}-redis-vol"
WEB_V="${PREFIX}-web-vol"
API_V="${PREFIX}-api-vol"
STAGING_V="${PREFIX}-staging-vol"
SELECTIVE_V="${PREFIX}-selective-vol"

cleanup() {
  local exit_code=$?
  if [[ $exit_code -ne 0 ]]; then
    echo "--- E2E failed (exit $exit_code); DockVault container logs ---" >&2
    docker logs "$DOCKVAULT_C" >&2 || true
  fi
  docker rm -f \
    "$DOCKVAULT_C" "$APP_C" "$EXCLUDED_C" "$WEB_C" "$API_C" "$REDIS_C" \
    >/dev/null 2>&1 || true
  docker volume rm -f \
    "$APP_V" "$ORPHAN_V" "$DELETE_V" "$REDIS_V" "$WEB_V" "$API_V" "$STAGING_V" "$SELECTIVE_V" \
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

printf "  ▶ Provisioning core fixtures (prefix=%s) ... " "$PREFIX"
docker volume create "$APP_V" >/dev/null
docker volume create "$ORPHAN_V" >/dev/null
docker volume create "$DELETE_V" >/dev/null
docker volume create "$REDIS_V" >/dev/null
docker volume create "$WEB_V" >/dev/null
docker volume create "$API_V" >/dev/null

# Seed bind mount directory
echo "bind-config=enabled" > "$WORKDIR/bind_data/bind.conf"

# Seed primary app volume
docker run --rm -v "${APP_V}:/data" alpine:3.21 sh -c '
  mkdir -p /data/config /data/cache &&
  echo "app_env=production" > /data/config/app.conf &&
  echo "important-data-v1" > /data/data.txt &&
  echo "skip-me-temp" > /data/debug.tmp &&
  echo "skip-me-cache" > /data/cache/blob.bin
'

# Seed orphan volumes
docker run --rm -v "${ORPHAN_V}:/data" alpine:3.21 sh -c '
  echo "orphan-precious-data" > /data/orphan.txt
'
docker run --rm -v "${DELETE_V}:/data" alpine:3.21 sh -c '
  echo "disposable-data" > /data/temp.txt
'

# Seed Compose stack volumes
docker run --rm -v "${WEB_V}:/data" alpine:3.21 sh -c 'echo "<h1>web-v1</h1>" > /data/index.html'
docker run --rm -v "${API_V}:/data" alpine:3.21 sh -c 'echo "{\"version\":1}" > /data/state.json'

# App container with GitOps labels, volume, bind mount, and docker.sock (to verify socket exclusion)
docker run -d --name "$APP_C" \
  -v "${APP_V}:/data" \
  -v "${WORKDIR}/bind_data:/bind_data" \
  -v /var/run/docker.sock:/var/run/docker.sock:ro \
  --label "dockvault.schedule=15 4 * * *" \
  --label "dockvault.exclude-paths=*.tmp,cache/**" \
  --label "dockvault.compression=zstd" \
  --label "dockvault.retention=14" \
  alpine:3.21 sleep 900 >/dev/null

# Excluded container (should be flagged excluded / ignored by discovery)
docker run -d --name "$EXCLUDED_C" \
  -v "${APP_V}:/ro_data:ro" \
  --label "dockvault.exclude=true" \
  alpine:3.21 sleep 900 >/dev/null

# Compose stack containers (simulated via com.docker.compose labels)
docker run -d --name "$WEB_C" \
  -v "${WEB_V}:/usr/share/nginx/html" \
  --label "com.docker.compose.project=${STACK_NAME}" \
  --label "com.docker.compose.service=web" \
  alpine:3.21 sleep 900 >/dev/null

docker run -d --name "$API_C" \
  -v "${API_V}:/app/state" \
  --label "com.docker.compose.project=${STACK_NAME}" \
  --label "com.docker.compose.service=api" \
  alpine:3.21 sleep 900 >/dev/null

# Redis container for zero-downtime hot dump test
docker run -d --name "$REDIS_C" \
  -v "${REDIS_V}:/data" \
  redis:7-alpine >/dev/null

for _ in $(seq 1 20); do
  if docker exec "$REDIS_C" redis-cli PING >/dev/null 2>&1; then
    break
  fi
  sleep 0.3
done
docker exec "$REDIS_C" redis-cli SET e2e:key1 "hello-dockvault" >/dev/null
docker exec "$REDIS_C" redis-cli SET e2e:key2 "zero-downtime-dump" >/dev/null

# DockVault under test
docker run -d --name "$DOCKVAULT_C" \
  --add-host=host.docker.internal:host-gateway \
  -p "127.0.0.1:${PORT}:8080" \
  -e TZ=Europe/Berlin \
  -e DOCKVAULT_ENCRYPTION_KEY="e2e-secret-passphrase" \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v "${WORKDIR}/data:/app/data" \
  -v "${WORKDIR}/backups:/app/backups" \
  "$IMAGE_TAG" >/dev/null
printf "ready\n"

e2e_wait_healthy "$PORT" "DockVault Core E2E"

DOCKVAULT_E2E_URL="http://127.0.0.1:${PORT}/api" \
DOCKVAULT_E2E_WEB_URL="http://127.0.0.1:${PORT}/" \
DOCKVAULT_E2E_PREFIX="$PREFIX" \
DOCKVAULT_E2E_BIND_DIR="${WORKDIR}/bind_data" \
  python3 "$SCRIPT_DIR/e2e_test.py"

if [[ "$WITH_DB" == "1" ]]; then
  "$SCRIPT_DIR/run_db_e2e.sh" --image "$IMAGE_TAG" --skip-build
fi

if [[ "$WITH_EDGE" == "1" ]]; then
  "$SCRIPT_DIR/run_edge_e2e.sh" --image "$IMAGE_TAG" --skip-build
fi
