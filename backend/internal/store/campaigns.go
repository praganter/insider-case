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
	ABTest     bool            `json:"ab_test"`
	Status     string          `json:"status"`
	Rule       json.RawMessage `json:"segment_rule"`
	CreatedAt  time.Time       `json:"created_at"`
	ApprovedAt *time.Time      `json:"approved_at"`
	// ScheduledAt is set while the campaign waits in the scheduled status.
	ScheduledAt *time.Time `json:"scheduled_at"`
}

func (s *Store) CreateCampaign(ctx context.Context, brief string, channels []string, abTest bool) (Campaign, error) {
	c := Campaign{Brief: brief, Channels: channels, ABTest: abTest, Status: "draft"}
	err := s.pool.QueryRow(ctx,
		`INSERT INTO campaigns (brief, channels, ab_test) VALUES ($1, $2, $3) RETURNING id, created_at`,
		brief, channels, abTest).Scan(&c.ID, &c.CreatedAt)
	return c, err
}

func (s *Store) GetCampaign(ctx context.Context, id int64) (Campaign, error) {
	var c Campaign
	err := s.pool.QueryRow(ctx,
		`SELECT id, brief, channels, ab_test, status, segment_rule, created_at, approved_at, scheduled_at FROM campaigns WHERE id = $1`, id).
		Scan(&c.ID, &c.Brief, &c.Channels, &c.ABTest, &c.Status, &c.Rule, &c.CreatedAt, &c.ApprovedAt, &c.ScheduledAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, ErrNotFound
	}
	return c, err
}

func (s *Store) GetContents(ctx context.Context, id int64) ([]content.Message, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT channel, variant, language, subject, body FROM campaign_contents WHERE campaign_id = $1 ORDER BY channel, language, variant`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []content.Message
	for rows.Next() {
		var m content.Message
		if err := rows.Scan(&m.Channel, &m.Variant, &m.Language, &m.Subject, &m.Body); err != nil {
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
				`INSERT INTO campaign_contents (campaign_id, channel, variant, language, subject, body) VALUES ($1, $2, $3, $4, $5, $6)`,
				id, m.Channel, m.Variant, m.Language, m.Subject, m.Body); err != nil {
				return err
			}
		}
		return nil
	})
}

// Approve moves the campaign to approved, or to scheduled when at is given.
func (s *Store) Approve(ctx context.Context, id int64, at *time.Time) error {
	return s.inTx(ctx, func(tx pgx.Tx) error {
		if at != nil {
			return moveTx(ctx, tx, id, "content_ready", "scheduled", "approved_at = now(), scheduled_at = $3", *at)
		}
		return moveTx(ctx, tx, id, "content_ready", "approved", "approved_at = now()")
	})
}

// Unschedule takes a scheduled campaign back to content_ready; it must be
// approved again.
func (s *Store) Unschedule(ctx context.Context, id int64) error {
	return s.inTx(ctx, func(tx pgx.Tx) error {
		return moveTx(ctx, tx, id, "scheduled", "content_ready", "approved_at = NULL, scheduled_at = NULL")
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

// Audience is the size of a segment, split by user language. Each language
// keeps, per template variable, its longest value, so content limits can be
// checked against the worst case of the people who will read that copy.
type Audience struct {
	Count  int
	ByLang map[string]map[string]string // language -> variable -> longest value
}

// AudienceStats takes a WHERE clause produced by segment.Build.
func (s *Store) AudienceStats(ctx context.Context, where string, args []any) (Audience, error) {
	// Column names come from the fixed content.Vars list, never from input.
	q := "SELECT u.language, count(*)"
	for _, v := range content.Vars {
		q += ", coalesce((array_agg(" + v + " ORDER BY char_length(" + v + ") DESC))[1], '')"
	}
	q += " FROM users u WHERE " + where + " GROUP BY u.language"
	a := Audience{ByLang: map[string]map[string]string{}}
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return a, err
	}
	defer rows.Close()
	for rows.Next() {
		var lang string
		var n int
		vals := make([]string, len(content.Vars))
		dest := []any{&lang, &n}
		for i := range vals {
			dest = append(dest, &vals[i])
		}
		if err := rows.Scan(dest...); err != nil {
			return a, err
		}
		a.Count += n
		a.ByLang[lang] = map[string]string{}
		for i, v := range content.Vars {
			a.ByLang[lang][v] = vals[i]
		}
	}
	return a, rows.Err()
}
