-- Checks before keys are passed on (docs/e2ee-plan.md, contract/crypto/check.json): a device
-- with keys asks the device or person that gets them to show the same code. The server only
-- relays the commitment and the two nonces; a check is good for 15 minutes.
CREATE TABLE key_checks (
  id          UUID PRIMARY KEY,
  asker       UUID NOT NULL REFERENCES devices (id) ON DELETE CASCADE,
  device_id   UUID REFERENCES devices (id) ON DELETE CASCADE, -- a device of the asker's person, waiting for the person's key
  user_id     UUID REFERENCES users (id) ON DELETE CASCADE,   -- another person, waiting for folder keys
  commitment  BYTEA NOT NULL,
  answer      BYTEA,                                          -- the waiting side's nonce
  answered_by UUID REFERENCES devices (id) ON DELETE CASCADE,
  reveal      BYTEA,                                          -- the asker's nonce, after the answer
  created_at  TIMESTAMPTZ(3) NOT NULL,
  CHECK ((device_id IS NULL) <> (user_id IS NULL))
);
CREATE UNIQUE INDEX key_checks_device ON key_checks (asker, device_id) WHERE device_id IS NOT NULL;
CREATE UNIQUE INDEX key_checks_user ON key_checks (asker, user_id) WHERE user_id IS NOT NULL;
CREATE INDEX key_checks_for_device ON key_checks (device_id);
CREATE INDEX key_checks_for_user ON key_checks (user_id);
