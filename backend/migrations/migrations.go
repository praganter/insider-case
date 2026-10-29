// Package migrations embeds the SQL files and applies them.
package migrations

import (
	"context"
	"embed"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed *.sql
var files embed.FS

// Apply runs every file in name order; each statement is idempotent.
func Apply(ctx context.Context, pool *pgxpool.Pool) error {
	entries, err := files.ReadDir(".")
	if err != nil {
		return err
	}
	for _, e := range entries {
		sql, err := files.ReadFile(e.Name())
		if err != nil {
			return err
		}
		if _, err := pool.Exec(ctx, string(sql)); err != nil {
			return fmt.Errorf("migration %s: %w", e.Name(), err)
		}
	}
	return nil
}
