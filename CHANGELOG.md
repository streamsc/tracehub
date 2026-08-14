# Changelog

All notable user-visible changes to TraceHub are recorded in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project uses [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## Unreleased

### Added

### Changed

### Fixed

### Removed

## [0.1.0-alpha.3] - 2026-08-14

### Added

- Tag-triggered GitHub Actions builds for verified release binaries, checksums,
  workflow artifacts, and Draft GitHub Releases.
- Public multi-platform container images for Linux amd64 and arm64 on GitHub
  Container Registry, published under immutable version tags.

## [0.1.0-alpha.2] - 2026-08-14

### Changed

- Docker builds now use the official Docker Hub base image names directly and
  no longer reference a maintainer-owned registry proxy.

## [0.1.0-alpha.1] - 2026-08-14

### Added

- Single `tracehub` binary with `serve`, `sync`, `mcp`, key generation, export,
  and local deletion commands.
- Incremental Codex JSONL synchronization with authoritative server offsets,
  gzip and age X25519 encryption, and Ed25519 request signatures.
- SQLite ciphertext archive, derived conversation index, FTS5 search, bounded
  session pagination, and on-demand tool-output decryption.
- systemd, single-container Docker, Docker Compose, and macOS/Linux amd64/arm64
  release artifacts.
- Repository governance, contribution, requirement, and release processes.
