-- +goose Up
-- +goose StatementBegin

-- Read state per user. With one read_at per notification, any account (also
-- a non-admin) could mark everything read and hide a failed backup from the
-- admins before they saw it.
CREATE TABLE notification_reads (
  notification_id INTEGER NOT NULL REFERENCES notifications(id) ON DELETE CASCADE,
  user_id         INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  read_at         INTEGER NOT NULL,
  PRIMARY KEY (notification_id, user_id)
);
CREATE INDEX idx_notification_reads_user ON notification_reads(user_id);

-- Keep what was already read as read for everyone, so nobody starts with a
-- wall of old unread notifications.
INSERT INTO notification_reads (notification_id, user_id, read_at)
  SELECT n.id, u.id, n.read_at FROM notifications n, users u WHERE n.read_at IS NOT NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS notification_reads;
-- +goose StatementEnd
