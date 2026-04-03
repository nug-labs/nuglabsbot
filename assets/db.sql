CREATE TABLE IF NOT EXISTS users (
  id BIGSERIAL PRIMARY KEY,
  telegram_id BIGINT NOT NULL UNIQUE,
  username TEXT,
  first_name TEXT,
  last_name TEXT,
  total_requests INTEGER NOT NULL DEFAULT 0,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  last_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS responses (
  id BIGSERIAL PRIMARY KEY,
  key TEXT NOT NULL UNIQUE,
  message TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS whitelist (
  id BIGSERIAL PRIMARY KEY,
  domain TEXT NOT NULL UNIQUE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS subscriptions (
  id BIGSERIAL PRIMARY KEY,
  telegram_id BIGINT NOT NULL UNIQUE,
  enabled BOOLEAN NOT NULL DEFAULT TRUE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS broadcasts (
  id TEXT PRIMARY KEY,
  type TEXT NOT NULL CHECK (type IN ('message', 'quiz')),
  payload JSONB NOT NULL,
  created_at TIMESTAMPTZ NOT NULL,
  created_db_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  -- updated_at: last time the row was upserted by convert-broadcasts (or other writers that set it).
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  -- frequency: NULL or 0 = one-time broadcast; integer >= 1 = recurring, value is interval in minutes.
  frequency INTEGER CHECK (frequency IS NULL OR frequency >= 0),
  -- sent_at: last completed wave (all outgoing rows sent); updated by handle-broadcast.
  sent_at TIMESTAMPTZ,
  -- audience: same semantics as convert-broadcasts YAML (all, active_users); NULL = all users.
  audience TEXT
);

CREATE TABLE IF NOT EXISTS broadcast_outgoing (
  id BIGSERIAL PRIMARY KEY,
  broadcast_id TEXT NOT NULL REFERENCES broadcasts(id) ON DELETE CASCADE,
  user_id BIGINT NOT NULL,
  scheduled_at TIMESTAMPTZ,
  sent_time TIMESTAMPTZ,
  telegram_message_id BIGINT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (broadcast_id, user_id)
);

-- Existing databases: add column if missing (safe to run repeatedly on PG 11+).
ALTER TABLE broadcast_outgoing ADD COLUMN IF NOT EXISTS telegram_message_id BIGINT;

ALTER TABLE broadcasts ADD COLUMN IF NOT EXISTS frequency INTEGER CHECK (frequency IS NULL OR frequency >= 0);
ALTER TABLE broadcasts ADD COLUMN IF NOT EXISTS sent_at TIMESTAMPTZ;
ALTER TABLE broadcasts ADD COLUMN IF NOT EXISTS audience TEXT;
ALTER TABLE broadcasts ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW();

CREATE TABLE IF NOT EXISTS app_analytics (
  id BIGSERIAL PRIMARY KEY,
  event_name TEXT NOT NULL,
  user_id BIGINT,
  entity_id TEXT,
  event_status TEXT,
  event_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  meta JSONB NOT NULL DEFAULT '{}'::jsonb
);

CREATE INDEX IF NOT EXISTS idx_users_last_seen_at ON users(last_seen_at);
CREATE INDEX IF NOT EXISTS idx_subscriptions_enabled ON subscriptions(enabled, telegram_id);
CREATE INDEX IF NOT EXISTS idx_broadcast_outgoing_pending ON broadcast_outgoing(sent_time, scheduled_at, broadcast_id);
CREATE INDEX IF NOT EXISTS idx_app_analytics_event_at ON app_analytics(event_at);
