# Development Runbook

## Start PostgreSQL

```bash
docker compose -f deploy/docker-compose.dev.yml up -d postgres
```

## Start Manager

```bash
cd backend
go mod tidy
go run ./cmd/routegate-manager
```

## Check Manager

```bash
curl http://localhost:8080/api/admin/health
```

## Start Agent

```bash
cd agent
go mod tidy
go run ./cmd/routegate-agent
```
