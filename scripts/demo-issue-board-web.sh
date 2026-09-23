#!/usr/bin/env bash
# Runs the issue-board Kanban UI in a browser, backed by the real Cloud HTTP API on PostgreSQL 17.
# Requires: Docker (cloud-postgres-1), Go toolchain. Ctrl+C to stop (the demo schema is dropped).
set -euo pipefail

cd "$(dirname "$0")/.."

export GOPROXY="${GOPROXY:-https://goproxy.cn,direct}"
export GOCACHE="${GOCACHE:-$PWD/.local/gocache}"
export GOTMPDIR="${GOTMPDIR:-$PWD/.local/gotmp}"
export DEMO_DATABASE_URL="${DEMO_DATABASE_URL:-host=127.0.0.1 port=55432 user=ora password=ora-local dbname=ora sslmode=disable}"
# Address the UI listens on, and whether to open a browser automatically.
export DEMO_WEB_ADDR="${DEMO_WEB_ADDR:-127.0.0.1:8899}"
export DEMO_WEB_OPEN="${DEMO_WEB_OPEN:-1}"

if ! docker ps --format '{{.Names}}' | grep -q '^cloud-postgres-1$'; then
  echo "Starting PostgreSQL 17 (cloud-postgres-1)…"
  docker compose up -d postgres
fi
until docker exec cloud-postgres-1 pg_isready -U ora -d ora >/dev/null 2>&1; do
  echo "waiting for postgres…"; sleep 1
done

go run ./cmd/demo-issue-board-web