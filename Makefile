SHELL := /bin/bash

PROJECT_NAME := rtime-status-board
REMOTE_NODE := sh-core
REMOTE_DIR := /opt/rtime-status-board
GO_IMAGE := golang:1.22-bookworm
GO_PROXY ?= https://goproxy.cn,direct
GO_TIMEOUT_SECONDS ?= 120
REMOTE_BACKEND_TIMEOUT_SECONDS ?= 900
REMOTE_GO_STEP_TIMEOUT_SECONDS ?= 600

COMPOSE_DEV := docker compose -p $(PROJECT_NAME)-dev -f compose.dev.yml --env-file .env
COMPOSE_PROD := docker compose -p $(PROJECT_NAME) -f compose.prod.yml --env-file .env.production
DOCKER_GO := ./scripts/run-with-timeout.sh $(GO_TIMEOUT_SECONDS) docker run --rm -e GOPROXY=$(GO_PROXY) -v "$$(pwd)":/workspace -w /workspace/backend $(GO_IMAGE)

.PHONY: help init-env init-prod-env init-config dev dev-down build prod-artifacts backend-prod-artifact frontend-prod-artifact test verify verify-local clean-generated backend-test backend-test-sh-core clean-sh-core-check-cache frontend-test config-check compose-dev-config compose-prod-config deploy-sh-core verify-sh-core install-compose-sh-core install-status-https-sh-core prod-up-sh-core prod-logs-sh-core prod-ps-sh-core prod-down-sh-core prod-up prod-logs prod-ps prod-down

help:
	@sed -n '1,140p' Makefile

init-env:
	@test -f .env || cp .env.example .env

init-prod-env:
	@test -f .env.production || cp .env.example .env.production

init-config:
	@test -f config/status-board.yaml || cp config/status-board.example.yaml config/status-board.yaml
	@test -f deploy/gatus/config.yaml || cp deploy/gatus/config.example.yaml deploy/gatus/config.yaml
	@test -f deploy/nginx/status.local.conf || cp deploy/nginx/status-board.example.conf deploy/nginx/status.local.conf

dev: init-env init-config
	$(COMPOSE_DEV) up --build

dev-down: init-env
	$(COMPOSE_DEV) down

build:
	docker build -f Dockerfile.backend -t $(PROJECT_NAME)/statusd:local .

prod-artifacts: frontend-prod-artifact backend-prod-artifact

frontend-prod-artifact:
	cd frontend && npm install && npm run build

backend-prod-artifact:
	mkdir -p dist
	$(DOCKER_GO) sh -c 'go mod download && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o ../dist/statusd-linux-amd64 ./cmd/statusd'

test: config-check backend-test frontend-test compose-dev-config compose-prod-config

verify: clean-generated frontend-test compose-dev-config compose-prod-config
	CLEAN_REMOTE_GO_CACHE=1 GO_TIMEOUT_SECONDS=$(REMOTE_BACKEND_TIMEOUT_SECONDS) GO_STEP_TIMEOUT_SECONDS=$(REMOTE_GO_STEP_TIMEOUT_SECONDS) GO_PROXY=$(GO_PROXY) ./scripts/backend-check-sh-core.sh
	$(MAKE) verify-sh-core

verify-local: clean-generated test

clean-generated:
	find deploy frontend backend -type d -name __pycache__ -prune -exec rm -rf {} +
	find . -name '*.pyc' -delete

backend-test:
	$(DOCKER_GO) go test ./...

backend-test-sh-core:
	GO_TIMEOUT_SECONDS=$(REMOTE_BACKEND_TIMEOUT_SECONDS) GO_STEP_TIMEOUT_SECONDS=$(REMOTE_GO_STEP_TIMEOUT_SECONDS) GO_PROXY=$(GO_PROXY) ./scripts/backend-check-sh-core.sh

clean-sh-core-check-cache:
	./scripts/clean-sh-core-check-cache.sh

frontend-test:
	cd frontend && npm install && npm run typecheck && npm run build

config-check: init-config
	$(DOCKER_GO) go run ./cmd/statusd -config /workspace/config/status-board.yaml -check-config

compose-dev-config: init-env init-config
	$(COMPOSE_DEV) config >/tmp/$(PROJECT_NAME).dev.compose.yml

compose-prod-config: init-prod-env init-config
	$(COMPOSE_PROD) config >/tmp/$(PROJECT_NAME).prod.compose.yml

deploy-sh-core: init-config prod-artifacts
	./scripts/deploy-sh-core.sh

verify-sh-core:
	./scripts/verify-sh-core.sh

install-compose-sh-core:
	./scripts/install-compose-sh-core.sh

install-status-https-sh-core:
	./scripts/install-status-https-sh-core.sh

prod-up-sh-core:
	./scripts/prod-compose-sh-core.sh up

prod-logs-sh-core:
	./scripts/prod-compose-sh-core.sh logs

prod-ps-sh-core:
	./scripts/prod-compose-sh-core.sh ps

prod-down-sh-core:
	./scripts/prod-compose-sh-core.sh down

prod-up: init-prod-env init-config
	@test -x dist/statusd-linux-amd64 || { echo "[ERROR] dist/statusd-linux-amd64 is missing. Use make prod-up-sh-core for sh-core production, or build local artifacts first."; exit 2; }
	@test -f frontend/dist/index.html || { echo "[ERROR] frontend/dist/index.html is missing. Run make frontend-prod-artifact first."; exit 2; }
	$(COMPOSE_PROD) up -d --build

prod-logs: init-prod-env
	$(COMPOSE_PROD) logs -f

prod-ps: init-prod-env
	$(COMPOSE_PROD) ps

prod-down: init-prod-env
	$(COMPOSE_PROD) down
