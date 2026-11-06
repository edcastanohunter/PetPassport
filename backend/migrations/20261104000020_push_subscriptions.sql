-- +goose Up
-- Web Push y preferencias (PP-F3-DAT-006, PP-F3-DOM-011, PP-F3-ARQ-015, PP-F3-PRD-011)
CREATE TABLE notifications.push_subscriptions (
  id              uuid PRIMARY KEY,
  user_id         uuid NOT NULL,
  endpoint_hash   bytea NOT NULL UNIQUE,
  endpoint        text NOT NULL,
  p256dh          text NOT NULL,
  auth            text NOT NULL,
  user_agent_hash bytea,
  status          text NOT NULL CHECK (status IN ('active','expired')),
  created_at      timestamptz NOT NULL DEFAULT now(),
  last_seen_at    timestamptz
);
CREATE INDEX push_subscriptions_user_idx ON notifications.push_subscriptions (user_id, status);

ALTER TABLE notifications.preferences
  ADD COLUMN push jsonb NOT NULL DEFAULT '{"appointments":true,"preventive_care":true,"clinical":false,"account":false,"security":true}';

ALTER TABLE notifications.deliveries
  DROP CONSTRAINT IF EXISTS deliveries_channel_check;
ALTER TABLE notifications.deliveries
  ADD CONSTRAINT deliveries_channel_check CHECK (channel IN ('email','push'));

GRANT SELECT, INSERT, UPDATE, DELETE ON notifications.push_subscriptions TO pp_app_writer;
GRANT SELECT ON notifications.push_subscriptions TO pp_app_reader;

-- +goose Down
ALTER TABLE notifications.deliveries
  DROP CONSTRAINT IF EXISTS deliveries_channel_check;
ALTER TABLE notifications.deliveries
  ADD CONSTRAINT deliveries_channel_check CHECK (channel IN ('email'));

ALTER TABLE notifications.preferences
  DROP COLUMN IF EXISTS push;

DROP TABLE IF EXISTS notifications.push_subscriptions;
