// Command seed applies the migrations and fills an empty database with
// deterministic demo data. Running it again changes nothing.
package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/praganter/insider-case/backend/migrations"
)

func main() {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()
	if err := run(ctx, pool, time.Now()); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, pool *pgxpool.Pool, now time.Time) error {
	if err := migrations.Apply(ctx, pool); err != nil {
		return err
	}
	var n int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM users").Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		log.Printf("seed: %d users already present, skipping", n)
		return nil
	}
	users, events := generate(now)
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	_, err = tx.CopyFrom(ctx, pgx.Identifier{"users"},
		[]string{"id", "first_name", "last_name", "email", "phone", "city", "language", "gender", "birth_year", "consent_sms", "consent_email", "created_at"},
		pgx.CopyFromSlice(len(users), func(i int) ([]any, error) {
			u := users[i]
			return []any{u.ID, u.FirstName, u.LastName, u.Email, u.Phone, u.City, u.Language, u.Gender, u.BirthYear, u.ConsentSMS, u.ConsentEmail, u.CreatedAt}, nil
		}))
	if err != nil {
		return err
	}
	_, err = tx.CopyFrom(ctx, pgx.Identifier{"events"},
		[]string{"user_id", "type", "at", "amount"},
		pgx.CopyFromSlice(len(events), func(i int) ([]any, error) {
			e := events[i]
			return []any{e.UserID, e.Type, e.At, e.Amount}, nil
		}))
	if err != nil {
		return err
	}
	log.Printf("seed: inserted %d users, %d events", len(users), len(events))
	return tx.Commit(ctx)
}
