param(
    [string]$EnvFile = ".env"
)

$ErrorActionPreference = "Stop"

if (-not (Test-Path -LiteralPath $EnvFile)) {
    throw "Environment file not found: $EnvFile (copy .env.example to .env first)"
}

$composeArgs = @("-f", "docker-compose.pull.yml", "--env-file", $EnvFile)

Write-Host "Pulling published images..."
& docker compose @composeArgs pull
if ($LASTEXITCODE -ne 0) {
    throw "Docker image pull failed"
}

Write-Host "Starting services..."
& docker compose @composeArgs up -d
if ($LASTEXITCODE -ne 0) {
    throw "Docker Compose startup failed"
}

Write-Host "Service status:"
& docker compose @composeArgs ps
