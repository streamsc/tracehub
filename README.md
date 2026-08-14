# TraceHub

[English](README.md) | [简体中文](README.zh-CN.md)

Private session hub for AI agents.

TraceHub collects agent session histories from multiple devices, preserves the
original records, and exposes authorized search and retrieval through MCP.

The first version supports Codex sessions. Additional agents can be added
through dedicated adapters without changing the archive and query model.

## Status

Early development.

## Planned components

- `server`: upload, archive, indexing, and query service
- `sync`: local session discovery and incremental upload
- `mcp`: authorized session search and retrieval
- `adapters/codex`: Codex JSONL parsing

## Project governance

- [Contributing](CONTRIBUTING.md)
- [Requirements](docs/requirements/README.md)
- [Release management](docs/releasing.md)
- [Changelog](CHANGELOG.md)

This project is licensed under the [Apache License 2.0](LICENSE).
