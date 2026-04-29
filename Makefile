SHELL := /bin/bash

.PHONY: dev-up dev-down backend agent tidy

dev-up:
	docker compose -f deploy/docker-compose.dev.yml up -d

dev-down:
	docker compose -f deploy/docker-compose.dev.yml down

backend:
	cd backend && go run ./cmd/routegate-manager

agent:
	cd agent && go run ./cmd/routegate-agent

tidy:
	cd backend && go mod tidy
	cd agent && go mod tidy
