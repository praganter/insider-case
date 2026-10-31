-- +goose Up
CREATE TABLE IF NOT EXISTS messages (
    id              bigserial PRIMARY KEY,
    campaign_id     bigint NOT NULL REFERENCES campaigns(id),
    user_id         bigint NOT NULL REFERENCES users(id),
    channel         text NOT NULL,
    variant         text NOT NULL DEFAULT 'A',
    language        text NOT NULL,
    status          text NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'retrying', 'sent', 'failed')),
    attempts        int NOT NULL DEFAULT 0,
    last_error      text,
    provider_msg_id text,
    updated_at      timestamptz NOT NULL DEFAULT now(),
    -- The database, not the code, guarantees nobody is messaged twice.
    UNIQUE (campaign_id, user_id, channel)
);

CREATE INDEX IF NOT EXISTS messages_campaign_status ON messages (campaign_id, status);
