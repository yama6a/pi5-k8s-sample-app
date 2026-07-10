-- +migrate Up
CREATE TABLE IF NOT EXISTS users
(
    -- id + created_at are supplied by the create-user-command (the signup service generates them),
    -- so no server-side defaults: the manager inserts exactly what it received.
    id         UUID        NOT NULL PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL
);

-- +migrate Down
DROP TABLE IF EXISTS users;
