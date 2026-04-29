Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

Push-Location backend
try {
    go run ./cmd/routegate-manager
}
finally {
    Pop-Location
}
