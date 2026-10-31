-- +goose Up
CREATE TABLE IF NOT EXISTS users (
    id           bigint PRIMARY KEY,
    first_name   text NOT NULL,
    last_name    text NOT NULL,
    email        text NOT NULL,
    phone        text NOT NULL,
    city         text NOT NULL,
    language     text NOT NULL,
    gender       text NOT NULL CHECK (gender IN ('male', 'female')),
    birth_year   int  NOT NULL,
    consent_sms   boolean NOT NULL,
    consent_email boolean NOT NULL,
    created_at   timestamptz NOT NULL
);

CREATE TABLE IF NOT EXISTS events (
    id      bigserial PRIMARY KEY,
    user_id bigint NOT NULL REFERENCES users(id),
    type    text NOT NULL,
    at      timestamptz NOT NULL,
    amount  numeric(10,2)
);

CREATE INDEX IF NOT EXISTS events_user_type_at ON events (user_id, type, at);
