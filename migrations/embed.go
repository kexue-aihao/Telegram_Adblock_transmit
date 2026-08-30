// Package migrations exposes the SQL migrations to the Go startup code.
// Keeping the files in an embed.FS makes the binary self-contained and avoids
// depending on a host filesystem path in Docker or a service manager.
package migrations

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// FS contains all *.sql migration files in this directory.
//
//go:embed *.sql
var FS embed.FS

// Apply runs all pending embedded migrations in filename/version order. It is
// deliberately small and dependency-free: Goose markers delimit the Up SQL,
// while this table tracks applied versions across restarts. Every migration
// should still be written to be idempotent where PostgreSQL permits it.
func Apply(ctx context.Context, pool *pgxpool.Pool) error {
	if pool == nil {
		return fmt.Errorf("migration database pool is nil")
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin migrations: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// Serialize migrations across bot instances. The transaction-scoped lock
	// is released automatically on commit or rollback.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(746179205617)`); err != nil {
		return fmt.Errorf("lock migrations: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS telegram_schema_migrations (
			version BIGINT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`); err != nil {
		return fmt.Errorf("create migration table: %w", err)
	}

	rows, err := tx.Query(ctx, `SELECT version FROM telegram_schema_migrations`)
	if err != nil {
		return fmt.Errorf("read migration versions: %w", err)
	}
	versions := make(map[int64]struct{})
	for rows.Next() {
		var version int64
		if err := rows.Scan(&version); err != nil {
			rows.Close()
			return fmt.Errorf("scan migration version: %w", err)
		}
		versions[version] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("iterate migration versions: %w", err)
	}
	rows.Close()

	files, err := fs.Glob(FS, "*.sql")
	if err != nil {
		return fmt.Errorf("list embedded migrations: %w", err)
	}
	sort.Strings(files)
	for _, filename := range files {
		version, err := migrationVersion(filename)
		if err != nil {
			return err
		}
		if _, applied := versions[version]; applied {
			continue
		}
		contents, err := fs.ReadFile(FS, filename)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", filename, err)
		}
		upSQL, err := upSQL(string(contents))
		if err != nil {
			return fmt.Errorf("parse migration %s: %w", filename, err)
		}
		if strings.TrimSpace(upSQL) == "" {
			return fmt.Errorf("migration %s has no Up SQL", filename)
		}
		if _, err := tx.Exec(ctx, upSQL); err != nil {
			return fmt.Errorf("apply migration %s: %w", filename, err)
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO telegram_schema_migrations (version) VALUES ($1)`, version); err != nil {
			return fmt.Errorf("record migration %s: %w", filename, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit migrations: %w", err)
	}
	return nil
}

func migrationVersion(filename string) (int64, error) {
	base := path.Base(filename)
	dot := strings.IndexByte(base, '_')
	if dot <= 0 {
		return 0, fmt.Errorf("migration filename %q must start with a numeric version", filename)
	}
	version, err := strconv.ParseInt(base[:dot], 10, 64)
	if err != nil || version < 1 {
		return 0, fmt.Errorf("migration filename %q has invalid version", filename)
	}
	return version, nil
}

func upSQL(contents string) (string, error) {
	const upMarker = "-- +goose Up"
	const downMarker = "-- +goose Down"
	start := strings.Index(contents, upMarker)
	if start < 0 {
		return "", fmt.Errorf("missing %q marker", upMarker)
	}
	start += len(upMarker)
	end := strings.Index(contents[start:], downMarker)
	if end < 0 {
		return strings.TrimSpace(contents[start:]), nil
	}
	return strings.TrimSpace(contents[start : start+end]), nil
}
