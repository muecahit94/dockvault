# Project Specification: DockVault (Docker Volume & Compose Backup UI)

## 1. Project Overview

**Name:** DockVault (or user preference)
**Description:** A self-hosted, single-container web application that mounts the Docker socket to automatically discover running containers, their associated volumes, and `docker-compose.yml` files. It provides a beautiful web GUI to schedule, manage, and execute safe backups to local storage or S3-compatible endpoints.
**Goal:** Create a plug-and-play UI that replaces custom bash scripts for Docker backups.

## 2. Tech Stack Recommendation

* **Backend:** Go (Golang) with the Fiber or Echo framework. (Why? Single compiled binary, tiny footprint, excellent official Docker SDK, very AI-friendly).
* **Frontend:** React (Vite) + Tailwind CSS + shadcn/ui. (Why? AI agents are exceptionally good at writing React/shadcn code, meaning you'll get a beautiful UI instantly).
* **Database:** SQLite (embedded, no extra containers required).
* **Docker SDK:** `[github.com/docker/docker/client](https://github.com/docker/docker/client)` (Go package).
* **Cron/Scheduling:** `[github.com/robfig/cron/v3](https://github.com/robfig/cron/v3)` (Go package).

## 3. Core Architecture & Mechanisms

### A. Container Discovery

The Go backend will mount `/var/run/docker.sock`. It will use the Docker API to list all containers, filtering by Docker Compose labels (`com.docker.compose.project`). It will extract the mount points (volumes and bind mounts) associated with each container/project.

### B. The "Safe Backup" Workflow

When a backup is triggered via the UI or Cron, the backend must execute this exact flow:

1. **Identify:** Find the container(s) and their attached volumes.
2. **Pause:** Send a `docker pause` command to the container(s) to freeze I/O and prevent database corruption.
3. **Archive:** Use Go's `archive/tar` and `compress/gzip` to compress the volume data located on the host (or via a temporary busybox worker container).
4. **Unpause:** Immediately send a `docker unpause` command.
5. **Export:** Save the `.tar.gz` locally or upload it to an S3 endpoint (using `[github.com/minio/minio-go](https://github.com/minio/minio-go)`).

## 4. Ideal Repository Structure

Feed this to your AI to establish the workspace:

```text
/dockvault
├── /backend
│   ├── /handlers       # API route controllers
│   ├── /docker         # Docker socket integration & backup logic
│   ├── /models         # SQLite DB schemas (Jobs, Schedules, Logs)
│   ├── /storage        # S3 and Local storage drivers
│   ├── main.go         # Go entrypoint
│   └── go.mod
├── /frontend
│   ├── /src
│   │   ├── /components # React components (shadcn/ui)
│   │   ├── /pages      # Dashboard, Containers, Settings, History
│   │   └── /api        # Axios/Fetch calls to backend
│   ├── package.json
│   └── vite.config.js
├── Dockerfile          # Multi-stage build (builds frontend, builds Go, runs together)
└── docker-compose.yml  # For testing the app itself

```

## 5. Implementation Phases (Prompts for your AI)

To get the best results, do not ask the AI to "build the whole thing" at once. Feed it these phases one by one:

### Phase 1: Foundation & Docker Connection

**Prompt to AI:** *"I am building DockVault. Please initialize a Go backend using Fiber and a React frontend using Vite and Tailwind. In the Go backend, create a service that connects to the local Docker socket using the official Docker SDK. Create a REST API endpoint `/api/containers` that lists all running containers, their names, states, and attached volumes. Display this as a simple table in the React frontend."*

### Phase 2: The Backup Engine (Local)

**Prompt to AI:** *"Now, let's implement the backup engine in Go. Create a function that takes a container ID. It should: 1. Pause the container using the Docker SDK. 2. Spin up a temporary lightweight alpine container that mounts the target container's volumes and creates a `.tar.gz` archive of them into a `/backups` folder. 3. Unpause the original container. 4. Destroy the temporary alpine container. Expose this via an API endpoint and add a 'Run Backup' button to the frontend table."*

### Phase 3: S3 Storage & Database Integration

**Prompt to AI:** *"Let's add state. Initialize a SQLite database using GORM. Create models for `StorageProvider` (to hold S3 credentials/local paths) and `BackupLog` (to track success/failure). Implement the `minio-go` library so that after the `.tar.gz` is created, it uploads to an S3 bucket if configured. Build a 'Settings' page in the React frontend to input S3 credentials."*

### Phase 4: Scheduling

**Prompt to AI:** *"Integrate `robfig/cron/v3` into the Go backend. Create a database model for `BackupSchedule` (container ID, cron string, target storage ID). Allow the user to define a cron schedule from the frontend UI. The Go backend should listen to these cron jobs and trigger the backup engine automatically. Log the results to the `BackupLog` table."*

### Phase 5: The Dockerfile

**Prompt to AI:** *"Write a multi-stage Dockerfile for this project. Stage 1 should build the React frontend. Stage 2 should compile the Go backend, embedding the built React static files to serve them directly from Fiber. The final image should be based on alpine, expose port 8080, and require `/var/run/docker.sock` to be mounted."*

---

### 💡 Pro-Tip for your GitHub launch

When you are ready to publish, make sure your `README.md` includes a simple `docker-compose.yml` that users can copy-paste to install your app.

```yaml
version: '3.8'
services:
  dockvault:
    image: ghcr.io/yourusername/dockvault:latest
    container_name: dockvault
    restart: unless-stopped
    ports:
      - "8080:8080"
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock # Required to command Docker
      - ./data:/app/data                          # SQLite DB location
      - ./backups:/app/backups                    # Local backup destination

```