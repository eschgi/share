-- 0003: people with an account, their phones, and invites.

-- A person. The username and password are optional: most people come in through an invite
-- and never need either.
CREATE TABLE users (
  id            TEXT PRIMARY KEY,
  name          TEXT NOT NULL,
  username      TEXT COLLATE NOCASE UNIQUE,
  password_hash TEXT,
  role          TEXT NOT NULL CHECK (role IN ('admin', 'member')),
  created_at    INTEGER NOT NULL,
  created_by    TEXT NOT NULL -- a user id, 'cli' or 'first-start'
) STRICT;

-- A signed-in phone, with its own long-lived key.
CREATE TABLE devices (
  id           TEXT PRIMARY KEY,
  user_id      TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  token_hash   BLOB NOT NULL UNIQUE,
  name         TEXT NOT NULL,
  created_at   INTEGER NOT NULL,
  last_seen_at INTEGER NOT NULL,
  revoked_at   INTEGER
) STRICT;

CREATE INDEX devices_by_user ON devices (user_id);

-- An invite works once, until it expires. With a user_id it adds a phone for that person;
-- otherwise accepting it creates the person.
CREATE TABLE invites (
  id         TEXT PRIMARY KEY,
  token_hash BLOB NOT NULL UNIQUE,
  name       TEXT NOT NULL,
  role       TEXT NOT NULL CHECK (role IN ('admin', 'member')),
  user_id    TEXT REFERENCES users (id) ON DELETE CASCADE,
  created_by TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  expires_at INTEGER NOT NULL,
  used_at    INTEGER,
  device_id  TEXT,
  revoked_at INTEGER
) STRICT;
