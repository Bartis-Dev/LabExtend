-- Refresh the default theme palette to a less-neon, forest-green accent.
-- Only the original-seeded "Default Dark" theme is touched; user-created
-- themes are left alone.
UPDATE themes
SET palette_json = '{"--bg":"#0a0a0a","--bg-card":"#141414","--bg-elevated":"#1c1c1c","--fg":"#e5e5e5","--fg-muted":"#9ca3af","--accent":"#15803d","--accent-hover":"#16a34a","--border":"#262626","--border-strong":"#3f3f46","--danger":"#b91c1c","--success":"#15803d","--warning":"#b45309"}',
    updated_at = strftime('%s','now')
WHERE is_default = 1
  AND palette_json LIKE '%6366f1%';
