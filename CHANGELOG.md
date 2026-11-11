# Changelog

## Unreleased

### Changed

- **Larger UI that uses the full width.** The root font size is 120% from
  1024px up (106% on phones), so text, spacing, table rows, bars, charts,
  cards and the sidebar grow by about a fifth in one place. Data pages (nodes,
  node detail, containers, container detail, backups, analytics, audit, files,
  webhooks, cron, S3) drop their centered max-width and use the whole page;
  settings and account stay at a readable width. Grids add columns on 1536px
  and 1920px screens. Below 1024px the sidebar becomes a drawer behind a menu
  button.
- **One dialog and message system.** Every `alert()` and `confirm()` is gone.
  Confirmations use a shared dialog built on the app's modal (focus trap,
  Escape, focus returns to the opener, only the top dialog reacts when two are
  stacked); success and error messages use one toast stack. The hand-rolled
  overlays on Backups, Cron, S3 and the Portainer restart now use the shared
  modal. Forms (backup plan, cron job, S3 endpoint, webhook, path profile)
  validate on submit with inline field errors instead of browser bubbles, and
  server errors stay in the dialog instead of being lost. Saving a backup plan
  or S3 endpoint no longer fails silently.
- **No browser defaults left.** Checkboxes, radios, selects, file inputs,
  scrollbars, focus rings, placeholders, selection and disabled states follow
  the design tokens. Icon buttons and status dots get real tooltips. The
  accent is now `#2563EB` so white text on it passes WCAG AA.
- **Loading, empty and error states on every data page.** Nothing shows for the
  first 400ms, then a skeleton; empty lists explain what to do next; load
  errors show the server's reason with a Retry button.

- **Event notifications replace alert rules.** Threshold alert rules, the
  Alerts page and the `/api/alert-rules` and `/api/alert-history` routes are
  removed. The leader now emits `backup.failed` (failed or partial run, with
  failed nodes and error), `backup.retention_failed`, `node.offline` (no
  heartbeat for 60s), `node.online` (connected again for 30s) and
  `container.crashloop` (once per transition). They are kept as notifications
  (newest 500, at most 90 days) and shown under a bell in the sidebar with the
  unread count, a list with severity, time and details, mark one or all as
  read, a link to the backup run, node or container, live updates over SSE and
  a toast for each new critical one. Webhooks subscribe to event types with
  checkboxes; every event goes to every enabled webhook subscribed to it. A
  failed post is logged and shown on the notification. "Send test" now reports
  Discord's actual answer. Migration 0012 drops `alert_rules` and
  `alert_history`, adds `notifications` and `webhook_configs.event_types`, and
  subscribes every existing webhook to all five types, so the webhook that got
  backup errors and alerts before keeps getting them.
- **Backup failures no longer depend on the plan's webhook setting.** Failed and
  partial runs always raise `backup.failed`, also for plans with
  `webhook_mode=off` or no webhook. The plan's webhook only receives the summary
  of successful runs, and only with `webhook_mode=always`. A run cut off by a
  leader restart is now announced as failed too.
- **Pocket ID is the only login.** Passwords, password change, TOTP 2FA and
  recovery codes, the first-boot setup wizard, the Users page and the
  Connect/Disconnect Pocket ID flow are removed. Access and role come from
  Pocket ID groups on every login: members of `BPM_OIDC_USER_GROUP` (default
  `labextend`) sign in as users, members of `BPM_OIDC_ADMIN_GROUP` (default
  `labextend-admins`) as admins, everyone else is refused and loses any session
  they still hold. An unknown subject in one of the groups gets a user row
  (username from `preferred_username`, collisions get `-2`, `-3`, ...); an
  existing one is matched by issuer and subject only and has role, email and
  name refreshed. The `groups` scope is requested in addition. Without
  `BPM_OIDC_*` the leader logs an error, keeps running, and the login page says
  what to set. The Account page shows the profile read-only and links to Pocket
  ID for passkeys. Migration 0011 drops `totp_secrets`, `users.password_hash`,
  `users.is_active`, `sessions.is_2fa_pending` and the `setup_completed` flag;
  existing user rows, sessions and audit history stay. `BPM_TOTP_KEY`,
  `BPM_TOTP_ISSUER` and `BPM_PASSWORD_LOGIN` are no longer read (a still
  mounted value is ignored). Break-glass: a Pocket ID one-time login link, see
  README.
- **Audit log mode on a new Settings page** (admin only): `auto` (default) keeps
  the full log with two or more users and only the newest 100 entries with a
  single user, `on` keeps everything, `off` records nothing and hides the Audit
  page. A single-user instance trims its existing log to 100 rows at the first
  start. Replaces `BPM_DISABLE_AUDIT`, which is no longer read.
- **The file browser is off unless `BPM_FILEBROWSER=true`** on the leader. All
  file routes answer 403 otherwise, and the Files page explains how to enable
  it.

### Security

- **A TOTP code can no longer be replayed.** `totp.Validate` accepts one 30s
  step of skew either side, so a used code stayed valid for up to 90 seconds.
  The leader now remembers the last accepted step per user and only accepts
  later ones (migration 0009).

- **grpc-go 1.66 → 1.82.1** for GO-2026-6061 and GO-2026-4762, both reachable
  through the leader's gRPC server (`govulncheck` traced them from
  `startGRPCServer` and the `Channel` stream). grpc 1.82 needs Go 1.25, so the
  module's minimum Go version moved from 1.24 to 1.25, and the image now builds
  with Go 1.27 — Go 1.24 has been out of security support since February 2026,
  and it compiled the standard library every release shipped with.
  `govulncheck ./...` reports nothing now. The two chi advisories from the
  earlier scan no longer apply: both were in `middleware.RealIP`, which was
  replaced by the trusted-proxy handling above.

- **The agent gRPC port is no longer published on the swarm ingress.**
  `docker-stack.yml` published `:9090` on every node's public interface, though
  agents reach the leader over the overlay network and never needed it. The
  agent channel was reachable from anywhere, guarded only by the shared token.
  Existing deployments keep the port until the stack is redeployed.

- **A read-only path profile now actually is.** `read_only` was checked on write
  and delete only, so mkdir, chown and rename went through — and renaming a
  file *out* of the protected root, then deleting it at its new place, removed
  it in two steps. Both ends of a rename are checked now.

- **File, cron, swarm-service, S3 and backup endpoints now require an admin
  account.** They were behind `requireAuth` only, so any non-admin user could
  write to any path on any node (the path profiles are a convenience, not a
  boundary — an unmatched absolute path falls back to a virtual `/` root) or
  install a cron entry with `run_as: root`. With `/:/host` mounted on every
  node that is root on the whole swarm. `is_admin` previously gated nothing but
  the user list and the audit log.

- **`password_secret` in a pgdump DSN is validated as a secret name.** It was
  concatenated straight onto `/run/secrets/`, so `../../etc/shadow` combined
  with an attacker-chosen `host=` made the agent read an arbitrary host file and
  send its contents as `PGPASSWORD` to a remote server.

- **The claimed `agent-id` is now bound to the client certificate when mTLS is
  configured.** Node identity came from a metadata string the agent chooses,
  authenticated only by the cluster-wide shared token, so any agent could take
  over another node's registry slot and receive the S3 credentials the leader
  decrypts for that node's backups. `scripts/gen-mtls.sh` issues one cert per
  node now instead of one shared agent cert. Token-only deployments are
  unchanged and still cannot distinguish nodes — issue per-node certs to close
  it.

- The S3 access key is no longer logged on every backup.

- **Starting a 2FA enrollment no longer switches off working 2FA.** Enrollment
  wrote the new secret straight into `secret_enc` with `enabled=0`, so merely
  opening the setup panel and closing it again disabled the second factor and
  destroyed the recovery codes — silently, and with every later login skipping
  2FA. A pending enrollment now lives in its own columns (migration
  `0006_totp_pending.sql`) and is promoted only once a valid code confirms it.
  Enrolling and disabling both require the account password now; with only the
  session cookie required, one POST was enough to strip 2FA after a session
  hijack.

- **Recovery codes are stored hashed.** They sat in cleartext next to the
  AES-GCM-encrypted secret, which handed anyone with a copy of the database
  eight working second factors per user and defeated the point of encrypting the
  secret. Comparison is constant-time; sheets printed before this change still
  work until the user re-enrolls.

- **Login and 2FA verification are rate limited** (10 failures per IP per 5 min,
  5 per pending session per 15 min, the latter destroying the session). Neither
  endpoint had any limit or attempt counter, and a 2FA-pending session lived for
  the full session TTL — a week by default — against a 10^6 code space.

- **`X-Forwarded-For` is only believed from a configured proxy.** chi's
  `middleware.RealIP` accepts it from any peer, and the audit logger and session
  store each read the header again on their own, so every client chose the
  source IP recorded against its own actions. Set `BPM_TRUSTED_PROXIES` to your
  reverse proxy; with it empty the TCP peer address is used. Note the default
  change: behind Traefik without this set, `source_ip` becomes the proxy's
  address rather than a forged one.

- **Password changes revoke every other session** — the caller's own survives.
  Admin-side password resets revoke all of the target's sessions. Neither did,
  so the one remediation available for a stolen cookie did nothing.

- **The last admin can no longer demote or deactivate themselves.** `Delete`
  guarded against this, `Update` did not, and the guard itself ignored the
  target's own `is_active` flag (refusing to remove an already-disabled admin)
  and treated a failed count query as "zero admins". Recovery from the lockout
  required editing the SQLite file on the volume.

- **Login answers identically for an unknown, a disabled and a wrong-password
  account.** A disabled account returned `403 account disabled` before any
  password was checked, and an unknown identifier skipped Argon2 entirely while
  a wrong password spent a 64 MiB pass — two independent oracles for enumerating
  valid accounts.

- **Concurrent Argon2 verifications are capped.** The stored parameters ask for
  64 MiB per call and nothing bounded concurrency, so a few hundred parallel
  unauthenticated login attempts were enough to OOM-kill the leader.

- Admin user updates no longer discard their errors — a failed password reset
  used to answer `200` while leaving the password unchanged.

### Added

- **Backup retention is enforced.** After every run that uploaded at least one
  archive, each plan keeps the newest `retention_keep` successful archives per
  node and deletes the older objects from the bucket (batched `DeleteObjects`,
  1000 keys per call), then their item rows, and a run row once it has no items
  left. A row is only removed when the provider confirmed the delete (an
  already missing key counts as deleted), the current run is never touched, an
  object another row still references (same-day re-runs under a `{date}`
  template) is kept, and `retention_keep` 0 keeps everything. Prune errors are
  logged and noted in the run's error summary without failing the backup.
  Failed runs and items, which hold no object, are removed after 30 days.
- **`bpm retention-dry-run`** prints per plan and node what the prune would
  delete and keep (items, objects, bytes) against the real DB, without the HTTP
  server and without deleting anything. `-list-bucket` also lists each plan's
  key prefix and reports unreferenced objects (never deleted) and rows whose
  object is missing; it needs `BPM_SECRETS_KEY`. `-db` overrides
  `$DATA_DIR/labextend.db`.

- **Sign in with Pocket ID (OIDC).** Optional login through any OIDC provider:
  authorization code flow with PKCE, state and nonce. Accounts are connected
  explicitly under Account (password required), never matched by email; an
  unknown identity is refused. An OIDC login skips the TOTP step. Configure
  with `BPM_OIDC_ISSUER`, `BPM_OIDC_CLIENT_ID`, `BPM_OIDC_CLIENT_SECRET` (or
  `_FILE`) and `EXTERNAL_URL`. `BPM_PASSWORD_LOGIN=false` turns the password
  form off, but only while OIDC is enabled.
- **Recovery codes: count, regenerate, copy, download.** The account page shows
  how many recovery codes are left (warning at two or fewer), can replace them
  with a fresh set (password required, old codes stop working), and offers Copy
  and Download .txt next to the codes. Its browser alert and confirm dialogs
  became inline messages and the shared modal.

- **Web Analytics: page views for every site behind Traefik, without a script
  on the sites.** The leader reads the JSON access log of every container whose
  image starts with `traefik` (`BPM_ANALYTICS_IMAGE_PREFIX`) from the log stream
  it already receives, and counts per host: requests, page views, bot hits and
  unique visitors, with top pages, referrer domains, browsers, operating systems
  and status classes. No IP address is stored: a visitor is a SHA-256 of a
  random per-day salt, the client address and the User-Agent, and the salt and
  hashes of a day are deleted once it ends, keeping only the count. Daily totals
  stay 400 days (`BPM_ANALYTICS_RETENTION_DAYS`), hourly ones 48 hours. Counts
  are aggregated in memory and written every 30 seconds. Migration
  `0007_web_analytics.sql`.

- **A root shell on any node, in the browser.** Every online node has a
  terminal button that opens a full xterm.js session against the host itself —
  host mounts, host network, host processes — with no sshd, no key and no
  passphrase to manage. The agent has no host privileges of its own, so it uses
  the Docker socket it already holds to start a privileged helper container that
  `nsenter`s into PID 1's namespaces; that container *is* the session. The
  browser talks WebSocket to the leader, the leader turns it into commands on
  the agent's existing gRPC channel.

  This is root on the host handed out over HTTP, so: admin-only, both open and
  close are audit-logged with the acting user and the helper container's id, and
  the session dies when the shell exits, when the browser leaves, after one
  hour, or when the agent loses its stream to the leader — no orphaned
  privileged container survives any of those. Cross-site requests are blocked by
  the WebSocket origin check (a CSRF header is not possible on a WebSocket
  handshake). At most 8 sessions per node.

  `BPM_SHELL_IMAGE` overrides the helper image; by default the agent uses its
  own, which is guaranteed to be on the node already and ships `nsenter`, so
  opening a shell never waits on a registry.

- **Nodes and containers are lists, not card grids.** One row per node with
  inline CPU/RAM/disk/network bars, online first; one row per container. The
  container list groups by node by default and can group by stack or show
  everything flat, the choice is remembered, groups collapse, and a group header
  carries the count of crash-looping containers inside it. Each node or stack is
  its own bordered block with a prominent name, and the columns stay aligned
  from block to block. Stack membership
  comes from the `com.docker.stack.namespace` / `com.docker.compose.project`
  labels the containers already carried.

- **Nodes show their real Docker Swarm labels.** `Spec.Labels` is what placement
  constraints actually match, and it is not the same thing as the agent's own
  `BPM_AGENT_LABELS` — so the two are fetched separately (`GET /nodes` on the
  manager socket, cached 30s) and rendered differently, swarm labels tinted and
  agent labels outlined. Swarm role and availability come along with them. On a
  cluster whose leader is not a manager the columns are simply empty.

- **A real chown dialog.** Owner changes went through `window.prompt("uid:gid")`
  and `confirm("Recursive?")`. There is now a modal that shows the current
  owner, offers the path profile's owner and `root:root` as presets, resolves a
  typed user name against the node, makes "recursive" a deliberate checkbox with
  the warning that it cannot be undone, and reports failures inline instead of
  in an alert box. Alerts elsewhere in the file browser became toasts.

### Fixed

- **Cron commands containing `%` run as written.** crontab(5) turns every
  unescaped `%` into a newline and hands the rest to the command's stdin, so
  `pg_dump … > db-$(date +%Y%m%d).sql` ran as `pg_dump … > db-$(date +` —
  silently, with the full command still shown in the UI. `%` is escaped when the
  file is written and unescaped when it is read back.

- **An invalid cron schedule is rejected instead of freezing the node's cron.**
  The leader stored any schedule; the agent rewrites the whole cron file and
  refuses the write if any entry is invalid. One `@daily` therefore stopped
  every later change to *every* job on that node from reaching the host, while
  the API answered 200 each time. The leader now applies the agent's rules
  before storing: five fields, one line, a valid user.

- **"Saved" no longer means "running on the host" when it doesn't.** A failed
  push to the agent was discarded and the API reported success — for an
  offline node, a rejected entry, anything. Create, update and delete now answer
  with `applied` and `apply_error`, the cron page shows it, and "apply" returns
  502 on failure instead of 200. Concurrent pushes to the same node are
  serialized; each carries the full job set, so two of them landing out of order
  used to drop the newer job from the host.

- The cron file uses `SHELL=/bin/sh` on hosts without bash. `/bin/bash` failed
  every job there with "no such file or directory", visible only in the host's
  own log. Hosts that have bash keep it, so existing bash-syntax jobs are
  unaffected.

- **Large file reads no longer knock the node offline.** grpc-go's default
  receive limit is 4 MiB; the default inline file limit is 5 MiB. Reading a file
  between the two did not fail the request — it killed the node's whole stream,
  dropping its live metrics and containers until the agent reconnected. The same
  happened with a log backlog after a leader outage. Both ends now accept up to
  64 MiB, and `BPM_FS_MAX_INLINE_BYTES` is capped at 32 MiB at startup.

- **`max_bytes` on a file read is capped and computed safely.** It came straight
  from the query string: `4294967295` wrapped `maxBytes+1` to zero and returned
  the file as empty and complete; `4000000000` allocated 4 GB and got the agent
  OOM-killed. A single `Read` call also reported a short read as the whole file.
  Writes are capped to the same limit (413) instead of decoding any body size
  into memory first.

- **Reading a FIFO no longer freezes the agent.** Commands run one at a time, and
  opening a FIFO blocks until a writer appears, so a single read of something
  like `/dev/xconsole` left the node "online" while every later file, cron and
  backup command timed out. Only regular files are read now.

- **`node_offline` alerts fire.** The rule iterated connected agents, and an agent
  leaves that list the moment its stream ends, so an offline node was never
  evaluated and a dead node raised nothing. It now reads the nodes table. The
  engine does not count its own downtime against the nodes, so a leader restart
  does not page for every node at once, and label scopes work for nodes that
  are offline. This also removes a data race on the agent's `LastSeen`.

- **An agent restart no longer reports every long-lived container as crash
  looping.** Docker's restart counter covers a container's whole life, and the
  first sighting counted all of it as restarts "just now" — a false
  `container_crashed` alert for any container with three or more lifetime
  restarts, on every rollout. The first sighting only sets the baseline.

- **A broken frontend build fails the image build.** The Dockerfile fell back to
  a one-line placeholder page when `npm run build` failed, so a TypeScript error
  produced an image that built, passed CI, was pushed as `:latest` and served a
  blank page.

- **The file browser works when the leader runs on Windows.** Paths on a remote
  host are POSIX, but the leader cleaned them with `path/filepath`, so on a
  Windows leader `filepath.Clean("/")` returned a backslash, the virtual
  full-filesystem root failed its equality check, and every listing came back
  `root not in managed paths for this node: /` — for the exact root the sidebar
  offered. The leader uses `path` for remote paths now; the agent keeps
  `filepath` because it builds real local paths and only runs on Linux.

- **Root-owned files show their owner again.** proto3 JSON omits zero values, so
  a file owned by uid 0 / gid 0 — the normal case — arrived with no `uid` and no
  `gid` at all, and the owner column rendered as a bare `:`. Absent now means 0.

- **User names resolve against the host, not the agent container.** The agent
  called `user.Lookup`, which reads the container's own `/etc/passwd`, so every
  real host account came back "unknown user" — precisely the case a name lookup
  exists for. It reads `/host/etc/passwd` first and falls back to `os/user`.

- Stopped containers no longer sort above running ones.

- **SSE tails and terminal sessions are no longer cut off after two minutes.**
  The router's 120s request timeout cancels the request context, which for a
  long-lived stream means a healthy connection is torn down on a clock. Those
  endpoints are exempt from it; ordinary requests keep their deadline.

- **The leader no longer crashes when a live-log viewer disconnects.** The log
  store's fanout copies the subscriber channels out from under a read lock and
  sends after releasing it, while unsubscribing closed the channel — a send on
  a closed channel, which panics inside the gRPC handler goroutine and takes
  the whole process down with it. Subscriber channels are no longer closed; the
  reader exits on its request context.

- **A backup that fails mid-archive no longer overwrites the previous good
  object.** The tar writer closed the pipe cleanly even after the walk failed,
  so the uploader saw an EOF and *completed* the multipart upload of a
  truncated archive. Because the default key template has date granularity, a
  failed manual run overwrote that morning's successful backup. The pipe is now
  closed with the error, which aborts the upload.

- **A file that vanishes mid-walk no longer produces a corrupt archive that
  reports success.** The tar header was written before the file was opened, so
  the skip left `archive/tar` permanently short by `hdr.Size` — a sticky error
  that failed every later entry, or, when the vanished file was the last one,
  an archive without its end-of-archive marker whose run was still recorded as
  `success` with a matching checksum. The file is opened first now, and the
  errors from `tar.Writer.Close`/`gzip.Writer.Close` are no longer discarded.

- **Backup runs no longer get stuck on `running` forever.** The terminal status
  write and the failure webhook used the run's own 4h context, so a run that hit
  that deadline could not record that it had — and `DeleteRun` refuses in-flight
  runs, making such rows unremovable through the API. Both now run on a detached
  context, the failed write is logged instead of discarded, and the leader marks
  runs left in flight by a previous process as failed at startup.

- A failed S3 upload no longer leaks the tar-walker goroutine and its open file
  handles; the walk result travels over a channel rather than a variable read
  while the walker was still writing it. A failing upload also reports the S3
  error itself rather than the closed-pipe error it causes in the walker, which
  otherwise named an innocent file as the culprit.

- **A multi-node plan can no longer be saved with a key template that is the
  same for every node.** The template is rendered per node, so one without
  `{host}` or `{node_id}` had every node uploading to the same object at once —
  last writer wins, the rest gone, and the run still reporting success with a
  byte count and checksum per node. `{host}`/`{node_id}` is now required unless
  the scope names exactly one node.

- **A plan cannot run twice at once.** `RunPlan` returned as soon as it had
  spawned its goroutine, so the cron job always completed instantly and nothing
  — not even `cron.SkipIfStillRunning` — would have prevented an overlap. A
  second start is refused; "Run now" answers `409` while a run is in flight.

- **pgdump sources with the same database name no longer overwrite each other.**
  The per-source key suffix was the database name alone, so two hosts both
  dumping `postgres` produced one object while the response reported two files
  and a checksum over both. The suffix carries the source index now.

- **`log_excerpt` keeps its per-node results.** It was cut at 8192 bytes in the
  middle of the encoded JSON, so it never parsed again and the reader silently
  fell back to an empty list — losing every `s3_key` and per-node error on
  exactly the runs where several nodes failed. The individual error strings are
  truncated instead, leaving the document valid, and a parse failure is logged
  rather than discarded.

### Known gaps

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
