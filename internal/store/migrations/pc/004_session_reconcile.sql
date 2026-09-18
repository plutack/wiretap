-- Mark interception sessions that ended without a clean shutdown.
--
-- ended_at used to carry that meaning by staying NULL forever, but app startup
-- now backfills ended_at for sessions left open by a crash so history always has
-- a usable close time. interrupted preserves the signal that NULL used to
-- encode, so the UI can still tell these runs apart from clean ones.
--
-- Older databases may already have this column before the migration ledger is
-- seeded, so the runner tolerates the duplicate-column error once.
ALTER TABLE intercept_sessions ADD COLUMN interrupted INTEGER NOT NULL DEFAULT 0;
