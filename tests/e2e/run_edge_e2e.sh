#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

IMAGE_TAG="${DOCKVAULT_E2E_IMAGE:-dockvault:edge-e2e-test}"
PORT="${DOCKVAULT_EDGE_E2E_PORT:-18090}"
PREFIX="${DOCKVAULT_EDGE_E2E_PREFIX:-dv-edge-e2e-$$}"
SKIP_BUILD="${DOCKVAULT_E2E_SKIP_BUILD:-0}"
KEEP_IMAGE="${DOCKVAULT_E2E_KEEP_IMAGE:-0}"
SKIP_CLOUD="${DOCKVAULT_E2E_SKIP_CLOUD:-0}"

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
    --skip-cloud)
      SKIP_CLOUD=1
      shift
      ;;
    -h|--help)
      cat <<EOF
Usage: $(basename "$0") [--image TAG] [--port PORT] [--skip-build] [--keep-image] [--skip-cloud]

Runs chaos, security, cloud-storage (Azurite + SeaweedFS S3), and edge-case E2E tests
against an isolated DockVault container.
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

WORKDIR="$(mktemp -d 2>/dev/null || mktemp -d -t dockvault-edge-e2e)"
mkdir -p "$WORKDIR/data" "$WORKDIR/backups" "$WORKDIR/seaweedfs"

NET_NAME="${PREFIX}-net"
DOCKVAULT_C="${PREFIX}-dockvault"
TRICKY_C="${PREFIX}-tricky"
STOPPED_C="${PREFIX}-stopped"
MULTI_CHUNK_C="${PREFIX}-big"
AZURITE_C="${PREFIX}-azurite"
S3_C="${PREFIX}-s3"

TRICKY_V="${PREFIX}-tricky-vol"
EMPTY_V="${PREFIX}-empty-vol"
STOPPED_V="${PREFIX}-stopped-vol"
MULTI_CHUNK_V="${PREFIX}-big-vol"
GFS_V="${PREFIX}-gfs-vol"

cleanup() {
  local exit_code=$?
  if [[ $exit_code -ne 0 ]]; then
    echo "--- Edge E2E failed (exit $exit_code); DockVault container logs ---" >&2
    docker logs "$DOCKVAULT_C" >&2 || true
  fi
  docker rm -f \
    "$DOCKVAULT_C" "$TRICKY_C" "$STOPPED_C" "$MULTI_CHUNK_C" "$AZURITE_C" "$S3_C" \
    >/dev/null 2>&1 || true
  docker volume rm -f \
    "$TRICKY_V" "$EMPTY_V" "$STOPPED_V" "$MULTI_CHUNK_V" "$GFS_V" \
    >/dev/null 2>&1 || true
  docker network rm "$NET_NAME" >/dev/null 2>&1 || true
  if [[ "$SKIP_BUILD" != "1" && "$KEEP_IMAGE" != "1" ]]; then
    docker rmi "$IMAGE_TAG" >/dev/null 2>&1 || true
  fi
  rm -rf "$WORKDIR"
}
trap cleanup EXIT

if [[ "$SKIP_BUILD" != "1" ]]; then
  e2e_build_image "$IMAGE_TAG" "$REPO_ROOT" "$WORKDIR/build.log"
fi

printf "  ▶ Provisioning edge & cloud fixtures (prefix=%s, skip_cloud=%s) ... " "$PREFIX" "$SKIP_CLOUD"
docker network create "$NET_NAME" >/dev/null
docker volume create "$TRICKY_V" >/dev/null
docker volume create "$EMPTY_V" >/dev/null
docker volume create "$STOPPED_V" >/dev/null
docker volume create "$MULTI_CHUNK_V" >/dev/null
docker volume create "$GFS_V" >/dev/null

# 1. Seed tricky filenames, dotfiles, UTF-8, quotes, 0-byte file, symlink
docker run --rm -v "${TRICKY_V}:/data" alpine:3.21 sh -c '
  mkdir -p /data/.hidden /data/"dir with spaces" &&
  echo "space-ok" > /data/"file with spaces.txt" &&
  echo "utf8-euro-ok" > /data/"ümlaut-€-file.txt" &&
  echo "quote-amp-ok" > /data/"quote'\''s & ampersand.txt" &&
  echo "nested-space" > /data/"dir with spaces"/"inner file.conf" &&
  echo "SECRET=top" > /data/.env.secret &&
  echo "hidden-nested" > /data/.hidden/token &&
  : > /data/empty.zero &&
  ln -s "file with spaces.txt" /data/sym-link &&
  echo "exclude-me" > /data/"ignored space.tmp"
'

# 2. Seed stopped container volume
docker run --rm -v "${STOPPED_V}:/data" alpine:3.21 sh -c 'echo "stopped-baseline" > /data/state.txt'
docker create --name "$STOPPED_C" -v "${STOPPED_V}:/data" alpine:3.21 sleep 300 >/dev/null

# 3. Seed >160KB incompressible volume (guarantees >=3 AES-256-GCM 64KiB chunks even compressed)
docker run --rm -v "${MULTI_CHUNK_V}:/data" alpine:3.21 sh -c '
  dd if=/dev/urandom of=/data/blob1.bin bs=1024 count=100 2>/dev/null &&
  dd if=/dev/urandom of=/data/blob2.bin bs=1024 count=80 2>/dev/null &&
  echo "canary=baseline" > /data/canary.txt
'

docker run -d --name "$TRICKY_C" -v "${TRICKY_V}:/data" alpine:3.21 sleep 900 >/dev/null
docker run -d --name "$MULTI_CHUNK_C" -v "${MULTI_CHUNK_V}:/data" alpine:3.21 sleep 900 >/dev/null

if [[ "$SKIP_CLOUD" != "1" ]]; then
  docker run -d --name "$AZURITE_C" --network "$NET_NAME" \
    --entrypoint azurite-blob \
    mcr.microsoft.com/azure-storage/azurite:latest \
    --blobHost 0.0.0.0 --blobPort 10000 --inMemoryPersistence --skipApiVersionCheck --silent >/dev/null

  cat > "$WORKDIR/seaweedfs/s3.json" <<'EOF'
{
  "identities": [
    {
      "name": "e2eadmin",
      "credentials": [
        {
          "accessKey": "e2eaccesskey",
          "secretKey": "e2esecretkey123"
        }
      ],
      "actions": ["Admin", "Read", "Write", "List", "Tagging"]
    }
  ]
}
EOF
  docker run -d --name "$S3_C" --network "$NET_NAME" \
    -v "${WORKDIR}/seaweedfs:/etc/seaweedfs" \
    --entrypoint weed \
    chrislusf/seaweedfs:latest \
    mini -dir=/tmp/weed -ip.bind=0.0.0.0 -s3 -s3.port=8333 -s3.config=/etc/seaweedfs/s3.json >/dev/null
fi

docker run -d --name "$DOCKVAULT_C" \
  --network "$NET_NAME" \
  -p "127.0.0.1:${PORT}:8080" \
  -e TZ=Europe/Berlin \
  -e DOCKVAULT_ENCRYPTION_KEY="edge-fallback-key" \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v "${WORKDIR}/data:/app/data" \
  -v "${WORKDIR}/backups:/app/backups" \
  "$IMAGE_TAG" >/dev/null
printf "ready\n"

e2e_wait_healthy "$PORT" "DockVault Edge E2E"

DOCKVAULT_EDGE_E2E_URL="http://127.0.0.1:${PORT}/api" \
DOCKVAULT_EDGE_E2E_PREFIX="$PREFIX" \
DOCKVAULT_EDGE_E2E_WORKDIR="$WORKDIR" \
DOCKVAULT_EDGE_E2E_SKIP_CLOUD="$SKIP_CLOUD" \
  python3 "$SCRIPT_DIR/edge_e2e_test.py"
