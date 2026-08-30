param(
    [string]$EnvFile = ".env",
    [ValidateRange(15, 300)]
    [int]$TimeoutSeconds = 90
)

$ErrorActionPreference = "Stop"

if (-not (Test-Path -LiteralPath $EnvFile)) {
    throw "Environment file not found: $EnvFile (copy .env.example to .env first)"
}

$composeArgs = @("-f", "docker-compose.pull.yml", "--env-file", $EnvFile)

function Get-ServiceState {
    param([Parameter(Mandatory = $true)][string]$Service)

    $containerID = (& docker compose @composeArgs ps --all -q $Service).Trim()
    if ([string]::IsNullOrWhiteSpace($containerID)) {
        return $null
    }

    $container = (& docker inspect $containerID | ConvertFrom-Json)[0]
    [PSCustomObject]@{
        ID           = $containerID
        Status       = $container.State.Status
        ExitCode     = $container.State.ExitCode
        Health       = if ($null -eq $container.State.Health) { "none" } else { $container.State.Health.Status }
        RestartCount = $container.RestartCount
    }
}

function Show-Diagnostics {
    Write-Host "`nCompose status:"
    & docker compose @composeArgs ps --all
    Write-Host "`nRecent service logs:"
    & docker compose @composeArgs logs --tail 100 bot postgres
}

function Wait-ForServices {
    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    $stableSince = $null
    $stableRestartCount = $null
    $lastStatus = ""

    while ((Get-Date) -lt $deadline) {
        $postgres = Get-ServiceState -Service "postgres"
        $bot = Get-ServiceState -Service "bot"

        if ($null -eq $postgres -or $null -eq $bot) {
            $lastStatus = "waiting for Compose to create both containers"
            Start-Sleep -Seconds 2
            continue
        }

        if ($postgres.Status -ne "running" -or $postgres.Health -eq "unhealthy") {
            throw "PostgreSQL did not start successfully (status=$($postgres.Status), health=$($postgres.Health), exitCode=$($postgres.ExitCode))."
        }
        if ($bot.Status -ne "running") {
            throw "Bot did not remain running (status=$($bot.Status), exitCode=$($bot.ExitCode))."
        }

        $lastStatus = "postgres=$($postgres.Health), bot=$($bot.Status), botRestarts=$($bot.RestartCount)"
        if ($postgres.Health -eq "healthy") {
            if ($null -eq $stableSince) {
                $stableSince = Get-Date
                $stableRestartCount = $bot.RestartCount
            } elseif ($bot.RestartCount -ne $stableRestartCount) {
                $stableSince = Get-Date
                $stableRestartCount = $bot.RestartCount
            }
            if (((Get-Date) - $stableSince).TotalSeconds -ge 10) {
                return
            }
        } else {
            $stableSince = $null
        }

        Start-Sleep -Seconds 2
    }

    throw "Services did not become stable within $TimeoutSeconds seconds ($lastStatus)."
}

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

try {
    Write-Host "Waiting for PostgreSQL health and a stable bot process..."
    Wait-ForServices
} catch {
    Show-Diagnostics
    throw
}

Write-Host "Deployment completed. PostgreSQL is healthy and the bot remained running for 10 seconds."
& docker compose @composeArgs ps --all
