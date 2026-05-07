SHELL := /bin/bash

COMPOSE_FILE := deploy/docker-compose.dev.yml

.PHONY: help dev up down restart rebuild logs ps backend-test frontend-install frontend-build check clean db-reset

help:
	@echo "RouteGate developer commands"
	@echo ""
	@echo "Usage:"
	@echo "  make dev              Start full dev stack"
	@echo "  make up               Start full dev stack"
	@echo "  make down             Stop dev stack"
	@echo "  make restart          Restart dev stack"
	@echo "  make rebuild          Rebuild and start dev stack"
	@echo "  make logs             Follow dev stack logs"
	@echo "  make ps               Show dev stack containers"
	@echo "  make backend-test     Run Go backend tests"
	@echo "  make frontend-install Install frontend dependencies"
	@echo "  make frontend-build   Build frontend"
	@echo "  make check            Run backend tests and frontend build"
	@echo "  make db-reset         Stop stack and remove dev database volume"
	@echo "  make clean            Remove generated local build files"

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

frontend-install:
	cd frontend && npm install

frontend-build:
	cd frontend && npm run build

check: backend-test frontend-build

db-reset:
	docker compose -f $(COMPOSE_FILE) down -v

clean:
	rm -rf frontend/dist
	rm -f frontend/tsconfig.tsbuildinfo
