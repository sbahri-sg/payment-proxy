package database

import (
	"context"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/emisell/api-payment-proxy/migrations"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	if _, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version text PRIMARY KEY,
			applied_at timestamptz NOT NULL DEFAULT now()
		)
	`); err != nil {
		return fmt.Errorf("create schema migrations: %w", err)
	}
	entries, err := fs.Glob(migrations.Files, "*.sql")
	if err != nil {
		return err
	}
	sort.Strings(entries)
	for _, name := range entries {
		version := strings.TrimSuffix(name, ".sql")
		var applied bool
		if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version=$1)`, version).Scan(&applied); err != nil {
			return err
		}
		if applied {
			continue
		}
		payload, err := migrations.Files.ReadFile(name)
		if err != nil {
			return err
		}
		tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, string(payload)); err == nil {
			_, err = tx.Exec(ctx, `INSERT INTO schema_migrations (version) VALUES ($1)`, version)
		}
		if err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("apply migration %s: %w", version, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit migration %s: %w", version, err)
		}
	}
	return nil
}
