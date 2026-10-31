-- +goose Up
ALTER TABLE campaigns ADD COLUMN IF NOT EXISTS ab_test boolean NOT NULL DEFAULT false;
