// Package store holds the pgx queries.
package store

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct{ pool *pgxpool.Pool }

func New(ctx context.Context, url string) (*Store, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, err
	}
	return &Store{pool}, nil
}

// CountAudience runs a WHERE clause produced by segment.Build over `users u`.
func (s *Store) CountAudience(ctx context.Context, where string, args []any) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, "SELECT count(*) FROM users u WHERE "+where, args...).Scan(&n)
	return n, err
}
