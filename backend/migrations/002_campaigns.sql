CREATE TABLE IF NOT EXISTS campaigns (
    id           bigserial PRIMARY KEY,
    brief        text NOT NULL,
    channels     text[] NOT NULL,
    status       text NOT NULL DEFAULT 'draft'
        CHECK (status IN ('draft', 'audience_ready', 'content_ready', 'approved', 'scheduled', 'sending', 'completed')),
    segment_rule jsonb,
    scheduled_at timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now(),
    approved_at  timestamptz
);

CREATE TABLE IF NOT EXISTS campaign_contents (
    campaign_id bigint NOT NULL REFERENCES campaigns(id),
    channel     text NOT NULL,
    variant     text NOT NULL DEFAULT 'A',
    language    text NOT NULL DEFAULT 'tr',
    subject     text NOT NULL DEFAULT '',
    body        text NOT NULL,
    PRIMARY KEY (campaign_id, channel, variant, language)
);
