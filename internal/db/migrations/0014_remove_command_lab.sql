-- Command Lab module removed: the in-app builder/docs experience wasn't
-- earning its weight in the navbar. Reference docs are better served by
-- the user's own bookmarks. Drop the seeded row so the navbar stops
-- showing it; deleted regardless of enabled state.
DELETE FROM modules WHERE builtin_key = 'command_lab';
