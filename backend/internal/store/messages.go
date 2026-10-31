package store

import (
	"context"
	"errors"
	"fmt"
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

// variantSQL splits the audience in two by a stable hash of the user id (the
// same person always lands in the same group); without an A/B test everyone gets A.
// hashtext can be negative, and & 1 is safe where abs() could overflow.
const variantSQL = `CASE WHEN %s THEN chr(65 + (hashtext(u.id::text) & 1)) ELSE 'A' END`

// StartSending moves an approved (or due scheduled) campaign to sending and creates one message
// per audience member and channel. ON CONFLICT DO NOTHING plus the table's
// UNIQUE(campaign_id, user_id, channel) means a repeated call cannot duplicate.
func (s *Store) StartSending(ctx context.Context, id int64, from string, abTest bool, parts []FanOut) error {
	return s.inTx(ctx, func(tx pgx.Tx) error {
		if err := moveTx(ctx, tx, id, from, "sending", ""); err != nil {
			return err
		}
		for _, p := range parts {
			// Builder args are $1..$n; ours follow them.
			n := len(p.Args)
			q := `INSERT INTO messages (campaign_id, user_id, channel, language, variant)
				SELECT $` + strconv.Itoa(n+1) + `, u.id, $` + strconv.Itoa(n+2) + `, u.language, ` +
				fmt.Sprintf(variantSQL, "$"+strconv.Itoa(n+3)+"::boolean") + `
				FROM users u WHERE ` + p.Where + ` ON CONFLICT DO NOTHING`
			if _, err := tx.Exec(ctx, q, append(append([]any{}, p.Args...), id, p.Channel, abTest)...); err != nil {
				return err
			}
		}
		return nil
	})
}

// Pending is a message not yet handed to the queue; the channel picks its queue.
type Pending struct {
	ID      int64
	Channel string
}

func (s *Store) PendingMessages(ctx context.Context, campaignID int64) ([]Pending, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, channel FROM messages WHERE campaign_id = $1 AND status = 'pending' ORDER BY id`, campaignID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Pending
	for rows.Next() {
		var p Pending
		if err := rows.Scan(&p.ID, &p.Channel); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
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

// VariantCounts is the number of messages per variant and status, for A/B results.
func (s *Store) VariantCounts(ctx context.Context, campaignID int64) (map[string]map[string]int, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT variant, status, count(*) FROM messages WHERE campaign_id = $1 GROUP BY variant, status`, campaignID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]map[string]int{}
	for rows.Next() {
		var v, st string
		var n int
		if err := rows.Scan(&v, &st, &n); err != nil {
			return nil, err
		}
		if out[v] == nil {
			out[v] = map[string]int{}
		}
		out[v][st] = n
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
	Consent    bool // the person's consent for this channel right now
	Template   content.Message
	Vars       map[string]string
}

func (s *Store) GetOutgoing(ctx context.Context, id int64) (Outgoing, error) {
	var o Outgoing
	var first, last, city, email, phone string
	err := s.pool.QueryRow(ctx, `
		SELECT m.id, m.campaign_id, m.status, m.channel, u.first_name, u.last_name, u.city, u.email, u.phone,
		       CASE m.channel WHEN 'sms' THEN u.consent_sms ELSE u.consent_email END,
		       c.subject, c.body
		FROM messages m
		JOIN users u ON u.id = m.user_id
		JOIN campaign_contents c ON c.campaign_id = m.campaign_id AND c.channel = m.channel AND c.variant = m.variant AND c.language = m.language
		WHERE m.id = $1`, id).
		Scan(&o.ID, &o.CampaignID, &o.Status, &o.Channel, &first, &last, &city, &email, &phone, &o.Consent,
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

// MessageStat is the number of messages in one channel and status, over all campaigns.
type MessageStat struct {
	Channel, Status string
	N               int
	Attempts        int // provider calls made for these messages, retries included
}

func (s *Store) MessageStats(ctx context.Context) ([]MessageStat, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT channel, status, count(*), coalesce(sum(attempts), 0) FROM messages GROUP BY channel, status ORDER BY channel, status`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MessageStat
	for rows.Next() {
		var m MessageStat
		if err := rows.Scan(&m.Channel, &m.Status, &m.N, &m.Attempts); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// FailStalled marks messages failed when Asynq archived their task before the
// worker could record a result, and completes the campaigns that were waiting on them.
func (s *Store) FailStalled(ctx context.Context, ids []int64, reason string) error {
	rows, err := s.pool.Query(ctx, `
		UPDATE messages SET status = 'failed', last_error = $2, updated_at = now()
		WHERE id = ANY($1) AND status IN ('pending', 'retrying') RETURNING campaign_id`, ids, reason)
	if err != nil {
		return err
	}
	campaigns := map[int64]bool{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return err
		}
		campaigns[id] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for id := range campaigns {
		if err := s.CompleteIfDone(ctx, id); err != nil {
			return err
		}
	}
	return nil
}
