# Changelog

## Unreleased

### Fixed

- Backup failures that happen before any node is contacted now reach the
  configured Discord webhook. A run that could not resolve its S3 endpoint (or
  matched no agents) was written to `backup_runs` as failed and announced
  nowhere, because the webhook only fired at the bottom of the run function,
  past those early returns. Six consecutive nightly runs were lost that way in
  August 2026 under a plan explicitly set to `webhook_mode=on-error`.
- Failure notifications now carry the reason. Early failures have no per-node
  results to derive one from, so the embed previously said "failed" and gave the
  reader nothing to act on; it now includes an `Error` field.
