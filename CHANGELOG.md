# Changelog

All notable changes to raftman are documented here, from the point of view of
someone running it. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/)
and versions follow [Semantic Versioning](https://semver.org/).

The GitHub release notes for a version are taken verbatim from its section here.

## [Unreleased]

## [1.1.0] - 2026-09-20

Behavior changes, all in the direction users would expect. Databases written
by earlier versions keep working; existing rows are not modified.

### Added

- Syslog frontends accept `format=RFC6587` (octet-counted framing over TCP)
  and `format=automatic`, which detects RFC3164, RFC5424 or RFC6587 per
  message, so one listener can serve mixed senders.
- `GET <api path>/healthz` answers `{"Status":"ok"}` after checking the
  database, or 503 with an `Error`. Available on both the `api+http` and
  `ui+http` frontends (`/api/healthz` with the defaults).

### Changed

- Timestamps are stored in UTC. Entries from senders in different timezones
  used to interleave wrongly in `list` and in date range filters, because the
  stored value kept the sender's offset and was compared as text. Rows written
  by earlier versions are left as they are, so ordering between old and new
  rows around the upgrade can still be off by the sender's offset.
- The `Application` filter of the API now applies on its own. It used to be
  silently ignored unless `Hostname` was also given.

### Fixed

- An RFC5424 packet with `-` as timestamp was stored at year 0001. It now
  gets the time raftman received it.

## [1.0.2] - 2026-09-20

No change to the syslog inputs, the JSON API, the command line or the database
file. A database written by any earlier version keeps working as is.

### Added

- `raftman -version` prints the version.
- Prebuilt static binaries for Linux amd64, arm64 and armv7 are attached to
  each GitHub release. They have no runtime dependency and work in
  `FROM scratch` images.
- The Docker image is published for amd64, arm64 and armv7.
- JSON responses carry `Content-Type: application/json`.

### Changed

- The web UI no longer loads anything from the network. It was built on a
  library fetched from a CDN at page load, which broke offline use and
  depended on an unpinned third party. It is now plain HTML, CSS and
  JavaScript with the same features. A failed query, such as a malformed
  search expression, is now shown in the toolbar instead of being ignored.
- The database is opened in SQLite WAL mode, so queries no longer wait for
  writes. Two side files, `logs.db-wal` and `logs.db-shm`, appear next to the
  database while raftman runs. The file stays readable by older versions of
  raftman and by any SQLite tool.
- Retention purges old entries with two statements instead of one per row,
  which is much faster on large databases.
- Built with Go 1.27, SQLite 3.4x and go-syslog 2.3.0. Docker image based on
  current Alpine.

### Fixed

- `docker stop` sends SIGTERM, which raftman did not handle: it was killed
  after the grace period and entries still in memory were lost. SIGTERM is
  now handled like SIGINT.
- Entries received but not yet written were dropped on shutdown. They are now
  written before the process exits.
- Shutdown could hang forever because of a lost wakeup in the stop sequence.
- In-flight HTTP requests are allowed to finish on shutdown.
- Building from a fresh clone failed: the embedded UI files were produced by
  a code generator that is no longer needed.

## [1.0.1] - 2019-11-21

### Fixed

- Docker image based on Alpine instead of `scratch`, with `/var/lib/raftman`
  created, so the default database path works out of the box.

## [1.0.0] - 2019-09-27

First tagged release. Syslog server (RFC5424 and RFC3164, UDP and TCP) storing
entries in an SQLite database with full text search, a JSON API (`stat`,
`list`) and a web UI. Retention by age. Built with Go 1.13 and Go modules.

[Unreleased]: https://github.com/pierredavidbelanger/raftman/compare/1.1.0...HEAD
[1.1.0]: https://github.com/pierredavidbelanger/raftman/compare/1.0.2...1.1.0
[1.0.2]: https://github.com/pierredavidbelanger/raftman/compare/1.0.1...1.0.2
[1.0.1]: https://github.com/pierredavidbelanger/raftman/compare/1.0.0...1.0.1
[1.0.0]: https://github.com/pierredavidbelanger/raftman/releases/tag/1.0.0
