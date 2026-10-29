package store

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/praganter/insider-case/backend/internal/content"
)

var ErrNotFound = errors.New("not found")

// ErrStateChanged means the campaign was not in the status the caller expected,
// i.e. someone else moved it in the meantime.
var ErrStateChanged = errors.New("campaign status changed")

type Campaign struct {
	ID         int64           `json:"id"`
	Brief      string          `json:"brief"`
	Channels   []string        `json:"channels"`
	Status     string          `json:"status"`
	Rule       json.RawMessage `json:"segment_rule"`
	CreatedAt  time.Time       `json:"created_at"`
	ApprovedAt *time.Time      `json:"approved_at"`
}

func (s *Store) CreateCampaign(ctx context.Context, brief string, channels []string) (Campaign, error) {
	c := Campaign{Brief: brief, Channels: channels, Status: "draft"}
	err := s.pool.QueryRow(ctx,
		`INSERT INTO campaigns (brief, channels) VALUES ($1, $2) RETURNING id, created_at`,
		brief, channels).Scan(&c.ID, &c.CreatedAt)
	return c, err
}

func (s *Store) GetCampaign(ctx context.Context, id int64) (Campaign, error) {
	var c Campaign
	err := s.pool.QueryRow(ctx,
		`SELECT id, brief, channels, status, segment_rule, created_at, approved_at FROM campaigns WHERE id = $1`, id).
		Scan(&c.ID, &c.Brief, &c.Channels, &c.Status, &c.Rule, &c.CreatedAt, &c.ApprovedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, ErrNotFound
	}
	return c, err
}

func (s *Store) GetContents(ctx context.Context, id int64) ([]content.Message, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT channel, subject, body FROM campaign_contents WHERE campaign_id = $1 ORDER BY channel`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []content.Message
	for rows.Next() {
		var m content.Message
		if err := rows.Scan(&m.Channel, &m.Subject, &m.Body); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// SetAudience stores the rule and moves the campaign to audience_ready. Content
// written for an older rule is dropped.
func (s *Store) SetAudience(ctx context.Context, id int64, from string, rule json.RawMessage) error {
	return s.inTx(ctx, func(tx pgx.Tx) error {
		if err := moveTx(ctx, tx, id, from, "audience_ready", `segment_rule = $3`, rule); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `DELETE FROM campaign_contents WHERE campaign_id = $1`, id)
		return err
	})
}

// SaveContents replaces the campaign's copy and moves it to content_ready.
func (s *Store) SaveContents(ctx context.Context, id int64, from string, msgs []content.Message) error {
	return s.inTx(ctx, func(tx pgx.Tx) error {
		if err := moveTx(ctx, tx, id, from, "content_ready", ""); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM campaign_contents WHERE campaign_id = $1`, id); err != nil {
			return err
		}
		for _, m := range msgs {
			if _, err := tx.Exec(ctx,
				`INSERT INTO campaign_contents (campaign_id, channel, subject, body) VALUES ($1, $2, $3, $4)`,
				id, m.Channel, m.Subject, m.Body); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Store) Approve(ctx context.Context, id int64) error {
	return s.inTx(ctx, func(tx pgx.Tx) error {
		return moveTx(ctx, tx, id, "content_ready", "approved", "approved_at = now()")
	})
}

// moveTx changes the status only if it still equals from. set is an optional
// extra assignment; its parameter, if any, is $3.
func moveTx(ctx context.Context, tx pgx.Tx, id int64, from, to, set string, args ...any) error {
	q := `UPDATE campaigns SET status = $2`
	if set != "" {
		q += ", " + set
	}
	q += ` WHERE id = $1`
	// $2 is the new status; the status guard goes last.
	all := append([]any{id, to}, args...)
	q += ` AND status = $` + strconv.Itoa(len(all)+1)
	tag, err := tx.Exec(ctx, q, append(all, from)...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrStateChanged
	}
	return nil
}

func (s *Store) inTx(ctx context.Context, fn func(pgx.Tx) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Audience is the size of a segment and, per template variable, its longest
// value, so content limits can be checked against the worst case.
type Audience struct {
	Count   int
	Longest map[string]string
}

// AudienceStats takes a WHERE clause produced by segment.Build.
func (s *Store) AudienceStats(ctx context.Context, where string, args []any) (Audience, error) {
	// Column names come from the fixed content.Vars list, never from input.
	q := "SELECT count(*)"
	for _, v := range content.Vars {
		q += ", coalesce((array_agg(" + v + " ORDER BY char_length(" + v + ") DESC))[1], '')"
	}
	q += " FROM users u WHERE " + where
	a := Audience{Longest: map[string]string{}}
	vals := make([]string, len(content.Vars))
	dest := []any{&a.Count}
	for i := range vals {
		dest = append(dest, &vals[i])
	}
	if err := s.pool.QueryRow(ctx, q, args...).Scan(dest...); err != nil {
		return a, err
	}
	for i, v := range content.Vars {
		a.Longest[v] = vals[i]
	}
	return a, nil
}
