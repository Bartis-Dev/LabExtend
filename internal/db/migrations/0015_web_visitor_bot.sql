-- +goose Up
-- A visitor found to be a bot later in the day (daily cap, machine rhythm)
-- keeps its hash row with bot = 1: it no longer counts as a visitor, and a
-- leader restart on the same day still knows it is a bot.
ALTER TABLE web_visitor_hashes ADD COLUMN bot INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE web_visitor_hashes DROP COLUMN bot;
