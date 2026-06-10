#!/usr/bin/env bash
set -euo pipefail

ACTION="${1:-up}"
PROJECT_NAME="${PROJECT_NAME:-rtime-status-board}"
GO_IMAGE="${GO_IMAGE:-golang:1.22-bookworm}"
GO_PROXY="${GO_PROXY:-https://goproxy.cn,direct}"
GO_STEP_TIMEOUT_SECONDS="${GO_STEP_TIMEOUT_SECONDS:-600}"
REMOTE_GO_CACHE_DIR="${REMOTE_GO_CACHE_DIR:-/tmp/rtime-status-board-go-cache}"
BUILD_CONTAINER="rsb-prod-backend-build-$$"
COMPOSE=(docker compose -p "$PROJECT_NAME" -f compose.prod.yml --env-file .env.production)

case "$ACTION" in
  up|logs|ps|down) ;;
  *)
    echo "[ERROR] unsupported action: $ACTION" >&2
    echo "        expected: up, logs, ps, down" >&2
    exit 2
    ;;
esac

if ! docker compose version >/dev/null 2>&1; then
  echo "[ERROR] docker compose plugin is missing on this host" >&2
  exit 2
fi

cleanup() {
  docker rm -f "$BUILD_CONTAINER" >/dev/null 2>&1 || true
}
trap cleanup EXIT

run_backend_build() {
  if [[ ! -f frontend/dist/index.html ]]; then
    echo "[ERROR] frontend/dist/index.html is missing. Run make deploy-sh-core after frontend build." >&2
    exit 2
  fi

  mkdir -p dist "$REMOTE_GO_CACHE_DIR/pkgmod" "$REMOTE_GO_CACHE_DIR/build"
  docker rm -f "$BUILD_CONTAINER" >/dev/null 2>&1 || true

  echo "[REMOTE] backend artifact build"
  if command -v timeout >/dev/null 2>&1; then
    timeout --kill-after=10s "$GO_STEP_TIMEOUT_SECONDS" \
      docker run --name "$BUILD_CONTAINER" --rm \
        -v "$PWD":/workspace \
        -v "$REMOTE_GO_CACHE_DIR/pkgmod":/go/pkg/mod \
        -v "$REMOTE_GO_CACHE_DIR/build":/root/.cache/go-build \
        -e GOPROXY="$GO_PROXY" \
        -w /workspace/backend \
        "$GO_IMAGE" \
        sh -c 'go mod download && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /workspace/dist/statusd-linux-amd64 ./cmd/statusd'
  else
    docker run --name "$BUILD_CONTAINER" --rm \
      -v "$PWD":/workspace \
      -v "$REMOTE_GO_CACHE_DIR/pkgmod":/go/pkg/mod \
      -v "$REMOTE_GO_CACHE_DIR/build":/root/.cache/go-build \
      -e GOPROXY="$GO_PROXY" \
      -w /workspace/backend \
      "$GO_IMAGE" \
      sh -c 'go mod download && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /workspace/dist/statusd-linux-amd64 ./cmd/statusd'
  fi

  chown "$(id -u):$(id -g)" dist/statusd-linux-amd64 >/dev/null 2>&1 || true
}

case "$ACTION" in
  up)
    run_backend_build
    echo "[REMOTE] docker compose config"
    "${COMPOSE[@]}" config >/tmp/rtime-status-board.prod.compose.yml
    echo "[REMOTE] docker compose up -d --build"
    "${COMPOSE[@]}" up -d --build
    echo "[REMOTE] docker compose ps"
    "${COMPOSE[@]}" ps
    ;;
  logs)
    "${COMPOSE[@]}" logs -f
    ;;
  ps)
    "${COMPOSE[@]}" ps
    ;;
  down)
    "${COMPOSE[@]}" down
    ;;
esac
