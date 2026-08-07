SHELL := /bin/bash

COMPOSE_FILE := deploy/docker-compose.dev.yml

.PHONY: help dev up down restart rebuild logs ps backend-test agent-test frontend-install frontend-i18n-check frontend-build installer-test release-bundle check clean db-reset dev-traffic-usage

help:
	@echo "RouteGate developer commands"
	@echo ""
	@echo "Usage:"
	@echo "  make dev                  Start full dev stack"
	@echo "  make up                   Start full dev stack"
	@echo "  make down                 Stop dev stack"
	@echo "  make restart              Restart dev stack"
	@echo "  make rebuild              Rebuild and start dev stack"
	@echo "  make logs                 Follow dev stack logs"
	@echo "  make ps                   Show dev stack containers"
	@echo "  make backend-test         Run Go backend tests"
	@echo "  make agent-test           Run Go agent tests"
	@echo "  make frontend-install     Install frontend dependencies"
	@echo "  make frontend-i18n-check  Run frontend localization QA check"
	@echo "  make frontend-build       Build frontend"
	@echo "  make installer-test       Validate and test the Clean VPS Installer"
	@echo "  make release-bundle       Build native bundles (requires VERSION)"
	@echo "  make check                Run backend, Agent, frontend and installer checks"
	@echo "  make db-reset             Stop stack and remove dev database volume"
	@echo "  make dev-traffic-usage    Write file-based dev traffic counters"
	@echo "  make clean                Remove generated local build files"

dev: up

up:
	docker compose -f $(COMPOSE_FILE) up --build

down:
	docker compose -f $(COMPOSE_FILE) down

restart:
	docker compose -f $(COMPOSE_FILE) restart manager frontend

rebuild:
	docker compose -f $(COMPOSE_FILE) down
	docker compose -f $(COMPOSE_FILE) up --build

logs:
	docker compose -f $(COMPOSE_FILE) logs -f

ps:
	docker compose -f $(COMPOSE_FILE) ps

backend-test:
	cd backend && go test ./...

agent-test:
	cd agent && go test ./...

frontend-install:
	cd frontend && npm install

frontend-i18n-check:
	cd frontend && npm run i18n:check

frontend-build:
	cd frontend && npm run build

installer-test:
	bash -n install.sh scripts/build-release-bundle.sh scripts/test-clean-vps-installer.sh
	scripts/test-clean-vps-installer.sh

release-bundle:
	@test -n "$(VERSION)" || (echo "VERSION is required, for example: make release-bundle VERSION=v1.0.0" >&2; exit 1)
	VERSION="$(VERSION)" scripts/build-release-bundle.sh

check: backend-test agent-test frontend-i18n-check frontend-build installer-test

db-reset:
	docker compose -f $(COMPOSE_FILE) down -v

dev-traffic-usage:
	bash scripts/dev-traffic-usage.sh "$$VPN_ACCOUNT_ID" "$$RX_BYTES" "$$TX_BYTES" "$${TRAFFIC_USAGE_FILE:-}"

clean:
	rm -rf frontend/dist dist
	rm -f frontend/tsconfig.tsbuildinfo
