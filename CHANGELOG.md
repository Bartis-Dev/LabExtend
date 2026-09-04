# Changelog

## Unreleased

### Fixed

- A file that changes size while it is being archived no longer aborts the
  whole node's backup. The tar header takes its size from the directory walk's
  stat, but the copy was unbounded, so a file that grew in between produced
  `archive/tar: write too long` - and because that error travelled out of the
  walk, the node uploaded nothing at all. supaserver's running Postgres hit it
  on five consecutive nights while the six nodes without busy writers
  succeeded. Grown files are now truncated to the stat'd size and shrunk ones
  zero-padded, so a torn read costs that one file instead of all of them. A
  file that disappears between the walk and the open is skipped for the same
  reason.

- Backup failures that happen before any node is contacted now reach the
  configured Discord webhook. A run that could not resolve its S3 endpoint (or
  matched no agents) was written to `backup_runs` as failed and announced
  nowhere, because the webhook only fired at the bottom of the run function,
  past those early returns. Six consecutive nightly runs were lost that way in
  August 2026 under a plan explicitly set to `webhook_mode=on-error`.
- Failure notifications now carry the reason. Early failures have no per-node
  results to derive one from, so the embed previously said "failed" and gave the
  reader nothing to act on; it now includes an `Error` field.
