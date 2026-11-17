# Stage 1: Build React frontend
FROM node:22-alpine AS frontend-builder

WORKDIR /frontend

COPY frontend/package.json frontend/package-lock.json* ./
RUN npm ci --prefer-offline --no-audit 2>/dev/null || npm install --prefer-offline --no-audit

COPY frontend/ ./
RUN npm run build

# Stage 2: Build Go backend with embedded frontend assets
FROM golang:1.26-alpine AS backend-builder

WORKDIR /backend

COPY backend/go.mod backend/go.sum ./
RUN go mod download

COPY backend/ ./
COPY --from=frontend-builder /frontend/dist ./static/dist

ARG VERSION=dev
RUN CGO_ENABLED=0 go build -ldflags="-s -w -X github.com/muecahit94/dockvault/config.Version=${VERSION}" -o /dockvault .

# Stage 3: Minimal production runtime
FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata \
    && mkdir -p /app/data /app/backups

WORKDIR /app

COPY --from=backend-builder /dockvault /usr/local/bin/dockvault

ENV DOCKVAULT_PORT=8080
ENV DOCKVAULT_DATA_DIR=/app/data
ENV DOCKVAULT_BACKUP_DIR=/app/backups
ENV DOCKVAULT_HELPER_IMAGE=alpine:3.21

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -qO- http://127.0.0.1:${DOCKVAULT_PORT}/api/health || exit 1

CMD ["dockvault"]
