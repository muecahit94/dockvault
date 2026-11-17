# DockVault

**Self-hosted Docker Volume & Compose Backup Vault**

[![Latest Release](https://img.shields.io/github/v/release/muecahit94/dockvault?label=version&color=007ec6)](https://github.com/muecahit94/dockvault/releases/latest)
[![Docker Image Size](https://img.shields.io/docker/image-size/muecahit/dockvault/latest?label=size&color=007ec6)](https://hub.docker.com/r/muecahit/dockvault)
[![Docker Hub Pulls](https://img.shields.io/docker/pulls/muecahit/dockvault?logo=docker&label=pulls&color=007ec6)](https://hub.docker.com/r/muecahit/dockvault)
[![Architectures](https://img.shields.io/badge/arch-amd64%20%7C%20arm64-007ec6?logo=docker)](https://hub.docker.com/r/muecahit/dockvault/tags)
[![License](https://img.shields.io/badge/license-MIT-007ec6)](https://github.com/muecahit94/dockvault/blob/main/LICENSE)

DockVault is a lightweight, single-container backup appliance with a clean web UI for Docker containers, Compose stacks, bind mounts, and orphan volumes. Connect `/var/run/docker.sock` and manage encrypted backups, S3/Azure replication, retention pruning, alerting, and one-click recovery without custom scripts.

- **Source & Full Documentation:** [github.com/muecahit94/dockvault](https://github.com/muecahit94/dockvault)
- **Container Registries:** `muecahit/dockvault` (Docker Hub) & `ghcr.io/muecahit94/dockvault` (GHCR)
- **Architectures:** `linux/amd64`, `linux/arm64`

---

## Supported Tags

| Tag | Description |
|-----|-------------|
| `latest` | Latest stable release |
| `<major>.<minor>.<patch>` | Pinned release version (e.g., `0.3.0`) |
| `<major>.<minor>` | Latest patch release in a minor series (e.g., `0.3`) |

---

## Quick Start

### Docker Compose (Recommended)

```yaml
services:
  dockvault:
    image: muecahit/dockvault:latest
    container_name: dockvault
    restart: unless-stopped
    ports:
      - "8080:8080"
    environment:
      - TZ=Europe/Berlin
      # Optional: bootstrap built-in admin login (or configure in Settings -> Security & Auth)
      # - DOCKVAULT_AUTH_USERNAME=admin
      # - DOCKVAULT_AUTH_PASSWORD=change-me-please
      # Optional: fallback passphrase for AES-256-GCM archive encryption
      # - DOCKVAULT_ENCRYPTION_KEY=your-strong-passphrase
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock # Discover containers & stream volumes
      - ./data:/app/data                          # SQLite database & settings
      - ./backups:/app/backups                    # Default local backup directory
```

```bash
docker compose up -d
```

Open **`http://localhost:8080`** in your browser.

### Docker CLI

```bash
docker run -d \
  --name dockvault \
  --restart unless-stopped \
  -p 8080:8080 \
  -e TZ=UTC \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v dockvault_data:/app/data \
  -v dockvault_backups:/app/backups \
  muecahit/dockvault:latest
```

---

## Key Features

- **Automatic Docker & Compose Discovery** — Groups Compose stacks (`com.docker.compose.project`), lists all attached mounts, and surfaces unattached orphan volumes.
- **Zero-Downtime DB Hot Dumps** — Auto-detects `postgres`, `mysql`, `mariadb`, `mongodb`, and `redis` and appends consistent database dumps into `db_dumps/` inside the archive without pausing DB containers.
- **Pause-Consistent Snapshots & Exclude Patterns** — Temporarily pauses target containers (configurable), skips `.gitignore`-style `--exclude` globs, and streams `gzip` or `zstd` archives via a lightweight Alpine helper.
- **Local, S3 & Azure Blob + 3-2-1 Replication** — Store locally (`/app/backups`), on S3-compatible object storage (AWS S3, MinIO, R2, B2, Wasabi), or Azure Blob Storage (Account Key, Entra ID Service Principal, Connection String, SAS), with automatic secondary offsite replication and **Scan & Import** disaster recovery.
- **Client-Side AES-256-GCM Encryption** — Encrypts archives locally (`scrypt` + chunked AES-256-GCM `.enc`) with an **Encryption & Key Health** overview, SHA-256 verification, in-browser archive tree inspection, and single-file extraction.
- **Safer Restores** — Restore in place or into an isolated **Staging Volume**, with an optional **Pre-Restore Safety Snapshot** created automatically before modifying data.
- **Observability & Analytics** — Real-time SSE **Live Backup Progress & Job Step Timeline**, **30/90-Day Backup Health Heatmap**, **24-Hour Cron Schedule Timeline**, and **Storage Analytics & Footprint** breakdown.
- **Scheduling, Retention & Alerts** — Cron schedules (`TZ`-aware), Keep-N / GFS retention with snapshot pinning, declarative **GitOps Labels**, reusable schedule policies, overdue-backup alerts, and independent **Telegram**, **Discord**, **Slack**, **ntfy**, or **Webhook** alerts.
- **Notes, Compare & Selective Restore** — Annotate backups, diff two snapshots, bulk pin/verify/delete, and restore only selected files or folders.
- **Auth & Reverse-Proxy SSO** — Built-in `bcrypt` login or trusted reverse-proxy header SSO (`Remote-User` / Authentik / Authelia) plus optional iframe embedding (`DOCKVAULT_FRAME_ANCESTORS`).

---

## Volumes & Ports

| Path / Port | Purpose |
|-------------|---------|
| `/var/run/docker.sock` | Read/control access to discover containers, stacks, and stream volumes |
| `/app/data` | Persistent SQLite database (`dockvault.db`) and configuration |
| `/app/backups` | Default local backup archive storage |
| `8080/tcp` | Web UI and REST API port (configurable via `DOCKVAULT_PORT`) |

---

## Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `DOCKVAULT_PORT` | HTTP server listen port (`PORT` also supported) | `8080` |
| `DOCKVAULT_DATA_DIR` | Directory storing `dockvault.db` | `/app/data` |
| `DOCKVAULT_BACKUP_DIR` | Default local backup directory | `/app/backups` |
| `DOCKVAULT_HELPER_IMAGE` | Lightweight helper image used to archive/extract volumes | `alpine:3.21` |
| `DOCKVAULT_AUTH_USERNAME` | Optional bootstrap admin username for built-in login | _(unset)_ |
| `DOCKVAULT_AUTH_PASSWORD` | Optional bootstrap admin password (min 6 chars; enables auth) | _(unset)_ |
| `DOCKVAULT_TRUSTED_PROXY_HEADER` | Reverse-proxy SSO header (e.g., `Remote-User`, `X-authentik-username`) | _(unset)_ |
| `DOCKVAULT_ENCRYPTION_KEY` | Fallback passphrase for AES-256-GCM archive encryption | _(unset)_ |
| `DOCKVAULT_FRAME_ANCESTORS` | Origins allowed to iframe-embed the UI (replaces `X-Frame-Options: DENY`) | _(unset)_ |
| `DOCKVAULT_TRUSTED_PROXIES` | Reverse-proxy IPs/CIDRs whose `X-Forwarded-For` identifies clients for login rate limiting | _(unset)_ |
| `TZ` | Timezone for cron evaluation and timestamps | `UTC` |

---

## Declarative Container Labels

Configure backup schedules, exclusions, compression, hot dumps, and hooks directly on target containers:

```yaml
services:
  postgres:
    image: postgres:16-alpine
    volumes:
      - pgdata:/var/lib/postgresql/data
    labels:
      - dockvault.schedule=0 3 * * *
      - dockvault.compression=zstd
      - dockvault.db-dump=true
      - dockvault.retention=14
      - dockvault.exclude-paths=*.tmp,pg_wal/**
```

| Label | Description |
|-------|-------------|
| `dockvault.schedule` | Cron expression synced on startup or via **Sync GitOps Labels** |
| `dockvault.exclude-paths` | Comma- or semicolon-separated `tar --exclude` glob patterns |
| `dockvault.compression` | Preferred compression (`gzip` or `zstd`) |
| `dockvault.retention` | Keep count for GitOps-managed schedules (default `7`) |
| `dockvault.db-dump` | `true` / `false` toggle for zero-downtime DB hot dumps |
| `dockvault.pre-backup` | Command executed inside container before pausing/archiving |
| `dockvault.post-backup` | Command executed inside container after unpausing |
| `dockvault.exclude=true` | Exclude container from DockVault discovery (`dockvault.enable=false` also supported) |

---

## Security Notes

- Access to `/var/run/docker.sock` grants control over the host Docker daemon. Enable **Built-in Authentication** or place DockVault behind an authenticated reverse proxy using `DOCKVAULT_TRUSTED_PROXY_HEADER` before exposing it beyond localhost.
- `/var/run/docker.sock` bind mounts are automatically excluded from all archives.
- Storage credentials, encryption keys, and webhook/bot tokens are never returned by API read endpoints.

---

## Links & License

- **GitHub Repository:** [muecahit94/dockvault](https://github.com/muecahit94/dockvault)
- **Bug Reports & Feature Requests:** [GitHub Issues](https://github.com/muecahit94/dockvault/issues)
- **Release Notes:** [GitHub Releases](https://github.com/muecahit94/dockvault/releases)
- **License:** [MIT](https://github.com/muecahit94/dockvault/blob/main/LICENSE)
