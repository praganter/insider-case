package store

import (
	"context"
	"errors"
	"strconv"

	"github.com/jackc/pgx/v5"

	"github.com/praganter/insider-case/backend/internal/content"
)

// FanOut is one channel's slice of the audience: a WHERE clause from segment.Build.
type FanOut struct {
	Channel string
	Where   string
	Args    []any
}

// StartSending moves an approved campaign to sending and creates one message
// per audience member and channel. ON CONFLICT DO NOTHING plus the table's
// UNIQUE(campaign_id, user_id, channel) means a repeated call cannot duplicate.
func (s *Store) StartSending(ctx context.Context, id int64, parts []FanOut) error {
	return s.inTx(ctx, func(tx pgx.Tx) error {
		if err := moveTx(ctx, tx, id, "approved", "sending", ""); err != nil {
			return err
		}
		for _, p := range parts {
			// Builder args are $1..$n; ours follow them.
			n := len(p.Args)
			q := `INSERT INTO messages (campaign_id, user_id, channel, language)
				SELECT $` + strconv.Itoa(n+1) + `, u.id, $` + strconv.Itoa(n+2) + `, u.language
				FROM users u WHERE ` + p.Where + ` ON CONFLICT DO NOTHING`
			if _, err := tx.Exec(ctx, q, append(append([]any{}, p.Args...), id, p.Channel)...); err != nil {
				return err
			}
		}
		return nil
	})
}

// PendingMessageIDs are the messages not yet handed to the queue.
func (s *Store) PendingMessageIDs(ctx context.Context, campaignID int64) ([]int64, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id FROM messages WHERE campaign_id = $1 AND status = 'pending' ORDER BY id`, campaignID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// MessageCounts is the number of messages per status.
func (s *Store) MessageCounts(ctx context.Context, campaignID int64) (map[string]int, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT status, count(*) FROM messages WHERE campaign_id = $1 GROUP BY status`, campaignID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var st string
		var n int
		if err := rows.Scan(&st, &n); err != nil {
			return nil, err
		}
		out[st] = n
	}
	return out, rows.Err()
}

// Outgoing is everything a worker needs to deliver one message.
type Outgoing struct {
	ID         int64
	CampaignID int64
	Status     string
	Channel    string
	To         string
	Template   content.Message
	Vars       map[string]string
}

func (s *Store) GetOutgoing(ctx context.Context, id int64) (Outgoing, error) {
	var o Outgoing
	var first, last, city, email, phone string
	err := s.pool.QueryRow(ctx, `
		SELECT m.id, m.campaign_id, m.status, m.channel, u.first_name, u.last_name, u.city, u.email, u.phone,
		       c.subject, c.body
		FROM messages m
		JOIN users u ON u.id = m.user_id
		JOIN campaign_contents c ON c.campaign_id = m.campaign_id AND c.channel = m.channel AND c.variant = m.variant
		WHERE m.id = $1`, id).
		Scan(&o.ID, &o.CampaignID, &o.Status, &o.Channel, &first, &last, &city, &email, &phone,
			&o.Template.Subject, &o.Template.Body)
	if errors.Is(err, pgx.ErrNoRows) {
		return o, ErrNotFound
	}
	o.Template.Channel = o.Channel
	o.Vars = map[string]string{"first_name": first, "last_name": last, "city": city}
	o.To = phone
	if o.Channel == "email" {
		o.To = email
	}
	return o, err
}

// StartAttempt counts an attempt before the provider is called.
func (s *Store) StartAttempt(ctx context.Context, id int64) error {
	_, err := s.pool.Exec(ctx, `UPDATE messages SET attempts = attempts + 1, updated_at = now() WHERE id = $1`, id)
	return err
}

func (s *Store) MarkSent(ctx context.Context, id int64, providerID string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE messages SET status = 'sent', provider_msg_id = $2, last_error = NULL, updated_at = now() WHERE id = $1`, id, providerID)
	return err
}

func (s *Store) MarkRetrying(ctx context.Context, id int64, reason string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE messages SET status = 'retrying', last_error = $2, updated_at = now() WHERE id = $1`, id, reason)
	return err
}

func (s *Store) MarkFailed(ctx context.Context, id int64, reason string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE messages SET status = 'failed', last_error = $2, updated_at = now() WHERE id = $1`, id, reason)
	return err
}

// CompleteIfDone marks the campaign completed once no message is waiting.
func (s *Store) CompleteIfDone(ctx context.Context, campaignID int64) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE campaigns SET status = 'completed'
		WHERE id = $1 AND status = 'sending'
		  AND NOT EXISTS (SELECT 1 FROM messages WHERE campaign_id = $1 AND status IN ('pending', 'retrying'))`, campaignID)
	return err
}
