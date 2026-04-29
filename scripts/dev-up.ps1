Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

docker compose -f deploy/docker-compose.dev.yml up -d
