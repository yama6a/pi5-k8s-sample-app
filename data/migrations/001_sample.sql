-- +migrate Up
CREATE TABLE IF NOT EXISTS sample
(
    id         UUID        NOT NULL DEFAULT gen_random_uuid() PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO sample DEFAULT VALUES;

-- +migrate Down
DROP TABLE IF EXISTS sample;
