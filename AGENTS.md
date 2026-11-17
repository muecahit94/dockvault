# AGENTS.md — DockVault Agent Handbook

This document provides AI coding assistants with a concise, accurate reference for the `dockvault` codebase, architecture, invariants, and workflows.

---

## 1. Architecture Overview

DockVault is a self-hosted, single-container web application that connects to `/var/run/docker.sock` to discover Docker containers and Compose stacks, execute safe volume backups, store archives locally or on S3-compatible endpoints, and manage recurring cron schedules.

- **Backend (`backend/`)**: Go + Fiber (`v2`), pure-Go SQLite (`github.com/glebarez/sqlite` + GORM), official Docker SDK (`github.com/docker/docker/client`), `github.com/robfig/cron/v3`, `github.com/minio/minio-go/v7`, and `github.com/Azure/azure-sdk-for-go/sdk/storage/azblob`.
- **Frontend (`frontend/`)**: React 19 + Vite + Tailwind CSS v4 + `lucide-react`.
- **Single-Binary Distribution**: In production builds (`Dockerfile`), `frontend/dist` is copied into `backend/static/dist` and embedded into the Go binary via `//go:embed all:dist` ([embed.go](backend/static/embed.go)). A `.gitkeep` placeholder ensures `backend/` compiles cleanly even without building the frontend first.

### Directory Map

```text
dockvault/
├── backend/
│   ├── main.go                    # Entrypoint: config, DB, Docker service, cron sync, graceful shutdown
│   ├── config/config.go           # Env config (DOCKVAULT_PORT, DATA_DIR, BACKUP_DIR, HELPER_IMAGE, TRUSTED_PROXIES, ...)
│   ├── auth/auth.go               # Password hashing (bcrypt) & HMAC-SHA256 session cookie signing/verification
│   ├── crypto/archive.go          # Streaming chunked AES-256-GCM archive encryption & decryption (DVENC1)
│   ├── models/
│   │   ├── db.go                  # SQLite init (WAL mode), migrations, default seeds, session secret, stale log recovery
│   │   └── models.go              # User, AppSetting, StorageProvider (+ DTO), BackupSchedule, BackupLog, NotificationConfig
│   ├── docker/
│   │   ├── client.go              # Docker discovery, Compose stacks, volumes/orphans, exec hook labels, mount filtering
│   │   ├── retention.go           # Count-based & GFS (daily/weekly/monthly) pruning while protecting pinned backups
│   │   ├── archive_filter.go      # Tar stream filtering for selective restore (selected paths + ancestor dirs)
│   │   └── backup.go              # Safe backup/restore engine (hooks, pause, alpine tar stream, encryption, 3-2-1, prune)
│   ├── storage/
│   │   ├── storage.go             # Driver interface, factory, optional DetailedTester/SASURLGenerator, SafeFileName
│   │   ├── local.go               # Local disk driver with base-dir containment checks
│   │   ├── retry.go               # Operation-level retry with backoff for transient S3/Azure failures
│   │   ├── s3.go                  # MinIO/S3-compatible driver (normalizeEndpoint, Put/Get/Remove/Test)
│   │   └── azure.go               # Azure Blob Storage driver (SharedKey/ConnStr/SAS, AccessTier, AutoCreate, SAS links)
│   ├── notify/notify.go           # Notifications (Telegram Bot, Discord, Slack, ntfy, generic JSON); backup + stale-schedule alerts
│   ├── scheduler/scheduler.go     # robfig/cron/v3 manager synced with enabled DB schedules
│   ├── scheduler/stale.go         # Overdue-schedule monitor (AlertAfterHours; alerts once per overdue period)
│   ├── handlers/                  # Fiber HTTP API controllers + embedded SPA fallback + security headers + login rate limiter
│   └── static/                    # Embedded React dist assets
├── frontend/
│   └── src/
│       ├── App.jsx                # Navigation shell, polling while backups run, modal/toast orchestration
│       ├── api/client.js          # Typed fetch wrapper + byte/duration/date formatters
│       ├── components/            # ui.jsx, BackupModal.jsx, ScheduleModal.jsx, ApplyScheduleModal.jsx, SetupChecklist.jsx
│       ├── utils/                 # desktopNotifications.js (per-browser opt-in, localStorage)
│       └── pages/                 # Dashboard.jsx, Containers.jsx, Schedules.jsx, History.jsx, Settings.jsx
├── .github/workflows/
│   ├── ci.yml                     # Frontend lint/build + backend go vet/test/build
│   └── release-please-docker-build.yml # Release Please + native amd64/arm64 builds -> GHCR manifest
├── Dockerfile                     # Multi-stage Node 22 -> Go 1.26 (CGO_ENABLED=0) -> Alpine 3.21
├── docker-compose.yaml            # Production/local compose setup
├── release-please-config.json     # Simple release-type bumping VERSION, CHANGELOG.md, frontend/package.json
├── .release-please-manifest.json  # Current release version
├── renovate.json                  # Renovate config (config:recommended)
└── docs/SPEC.md                   # Original project specification
```

---

## 2. Core Implementation Invariants

Follow these rules whenever modifying the codebase:

1. **Never Trust GORM `default:` Tags on Booleans or Zero-Valid Ints**
   - In `backend/models/models.go`, do **not** add `gorm:"default:true"` to `bool` fields (e.g., `UseSSL`, `PauseContainer`, `Enabled`) or `gorm:"default:7"` to `RetentionCount`. GORM treats Go zero values (`false`, `0`) as unset and overrides them on `Create()`. Apply defaults in the Go request handlers using pointer fields (`*bool`, `*int`).
2. **Safe Backup Lifecycle (`backend/docker/backup.go`)**
   - Always acquire the per-target lock (`targetType:targetId`) before starting a backup (`ErrBackupInProgress` -> HTTP `409 Conflict`).
   - Never pause DockVault itself (`IsSelf` check in `client.go`).
   - Always exclude `/var/run/docker.sock` mounts (`IsDockerSocket` / `Backupable == false`).
   - Always unpause containers in a `defer` using a fresh `context.WithTimeout(context.Background(), 30*time.Second)` so containers are unpaused even if the job fails or its context cancels, and unpause immediately after streaming the archive (before uploading to S3).
   - Stream `tar -czf - -C /source .` over `ContainerAttach` (`stdcopy.StdCopy`) instead of writing to host paths from the sibling helper container so backups work identically on macOS Docker Desktop, Linux Docker, and inside containers.
3. **Security & Path Containment**
   - Never serialize `StorageProvider.SecretKey` (`json:"-"` + `StorageProviderDTO` with `HasSecretKey`). On `PUT /api/storage/:id`, preserve the existing `SecretKey` when the incoming `secretKey` is blank.
   - Local storage operations must always enforce `SafeFileName` and `isContained` (`filepath.Rel` within `baseDir`).
   - Preserve HTTP security headers (`X-Content-Type-Options`, `X-Frame-Options`, `Referrer-Policy`, `Permissions-Policy`) in `backend/handlers/router.go`.
4. **Frontend Modal State & Polling**
   - `App.jsx` polls `/api/*` every 2.5s while any backup has `status === 'running'`. Modal `useEffect` hooks (such as `ScheduleModal.jsx`) must not depend on polling-refreshed arrays (`containers`, `stacks`) or user form input will reset mid-edit.

---

## 3. Common Development & Verification Commands

### Backend (from `backend/`)
```bash
go vet ./...
go test -race -count=1 ./...
CGO_ENABLED=0 go build -o /tmp/dockvault .
```

### Frontend (from `frontend/`)
```bash
npm ci
npm run lint
npm run build
```

### Local Full-Stack Dev
```bash
# Terminal 1
cd backend && go run .

# Terminal 2 (proxies /api -> http://localhost:8080)
cd frontend && npm run dev
```

### Docker Build & E2E Validation (from repo root)
```bash
docker compose config -q
docker build --build-arg VERSION=0.1.0 -t dockvault:local .
./tests/e2e/run_e2e.sh
```

---

## 4. CI/CD & Release Workflow

- **CI (`.github/workflows/ci.yml`)**: Runs on every pull request and non-`main` push (`frontend` lint + build; `backend` vet + `go test -race ./...` + `CGO_ENABLED=0 go build`; `e2e` live Docker suite via `./tests/e2e/run_e2e.sh`).
- **Release & Multi-Arch Images (`.github/workflows/release-please-docker-build.yml`)**:
  - Uses `googleapis/release-please-action@v4` with `release-please-config.json` (`release-type: simple`) and `.release-please-manifest.json` on pushes to `main`.
  - When a release is created, builds `linux/amd64` (`ubuntu-24.04`) and `linux/arm64` (`ubuntu-24.04-arm`) natively by digest with `VERSION` injected via `ldflags`, then merges them via `docker buildx imagetools create` to `ghcr.io/${{ github.repository }}` (`{{version}}`, `{{major}}.{{minor}}`, `latest`).
- **Dependencies**: Managed by Renovate via `renovate.json`.
