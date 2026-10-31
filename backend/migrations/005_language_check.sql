-- +goose Up
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_language_check;
ALTER TABLE users ADD CONSTRAINT users_language_check CHECK (language IN ('tr', 'en'));
-- The reconciler only looks at messages that are still open.
CREATE INDEX IF NOT EXISTS messages_open ON messages (id) WHERE status IN ('pending', 'retrying');
