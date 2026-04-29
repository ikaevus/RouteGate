Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

Push-Location agent
try {
    go run ./cmd/routegate-agent
}
finally {
    Pop-Location
}
