package migrations

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/praganter/insider-case/backend/internal/testdb"
)

func TestApplyRecordsVersionsAndIsRepeatable(t *testing.T) {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, testdb.New(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	for range 2 {
		if err := Apply(ctx, pool); err != nil {
			t.Fatal(err)
		}
	}
	var applied, latest int
	if err := pool.QueryRow(ctx, "SELECT count(*), max(version_id) FROM goose_db_version WHERE version_id > 0").Scan(&applied, &latest); err != nil {
		t.Fatal(err)
	}
	if applied != 5 || latest != 5 {
		t.Fatalf("applied=%d latest=%d, want 5 and 5", applied, latest)
	}
}
