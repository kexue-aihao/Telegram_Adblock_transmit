$ErrorActionPreference = "Stop"

# Use a dedicated Compose project and database so this script cannot touch the
# default deployment volume. Docker Desktop must be running before invocation.
$projectName = "telegram-adblock-integration"
$testDatabase = "telegram_adblock_test"
$postgresPassword = "integration-only-password"
$env:POSTGRES_PASSWORD = $postgresPassword
$env:BOT_TOKEN = "integration-test-token"

try {
    docker compose -p $projectName up -d postgres
    for ($attempt = 0; $attempt -lt 30; $attempt++) {
        docker compose -p $projectName exec -T postgres pg_isready -U telegram_bot -d postgres *> $null
        if ($LASTEXITCODE -eq 0) {
            break
        }
        Start-Sleep -Seconds 1
    }
    if ($LASTEXITCODE -ne 0) {
        throw "PostgreSQL did not become ready within 30 seconds"
    }

    docker compose -p $projectName exec -T postgres psql -U telegram_bot -d postgres -v ON_ERROR_STOP=1 -c "DROP DATABASE IF EXISTS $testDatabase WITH (FORCE);"
    docker compose -p $projectName exec -T postgres psql -U telegram_bot -d postgres -v ON_ERROR_STOP=1 -c "CREATE DATABASE $testDatabase;"

    # Apply every embedded migration in filename order so integration runs match
    # the bot's startup migrations exactly (scripts/applied in production).
    $migrationsDir = Join-Path $PSScriptRoot "..\migrations"
    $applied = 0
    foreach ($migrationFile in (Get-ChildItem (Join-Path $migrationsDir "*.sql") | Sort-Object Name)) {
        $migration = Get-Content $migrationFile.FullName -Raw
        $upSQL = ($migration -split "(?m)^-- \+goose Down")[0] -replace "(?m)^-- \+goose Up\s*", ""
        $upSQL | docker compose -p $projectName exec -T postgres psql -U telegram_bot -d $testDatabase -v ON_ERROR_STOP=1
        if ($LASTEXITCODE -ne 0) {
            throw "Failed to apply migration $($migrationFile.Name)"
        }
        $applied++
    }
    if ($applied -lt 1) {
        throw "No migration files found under $migrationsDir"
    }

    # Run the Go test inside a short-lived Go container on the Compose network;
    # PostgreSQL is intentionally not published to the host in deployment.
    $repoPath = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
    $testDSN = "postgres://telegram_bot:$postgresPassword@postgres:5432/${testDatabase}?sslmode=disable"
    docker run --rm --network "${projectName}_default" -e "TEST_DATABASE_URL=$testDSN" -v "${repoPath}:/src" -w /src golang:1.26-alpine sh -c "go test ./internal/store -run TestRuleRepositoryIntegration -count=1"
    if ($LASTEXITCODE -ne 0) {
        throw "Go PostgreSQL integration tests failed"
    }
}
finally {
    docker compose -p $projectName down -v --remove-orphans
    Remove-Item Env:POSTGRES_PASSWORD -ErrorAction SilentlyContinue
    Remove-Item Env:BOT_TOKEN -ErrorAction SilentlyContinue
}
