# TraceHub

[English](README.md) | [简体中文](README.zh-CN.md)

Current version: `v0.1.0-alpha.3`.

TraceHub is a private, single-user session hub for AI agents across multiple
devices. The first release reads local Codex JSONL files, incrementally uploads
gzip-compressed and age-encrypted records, stores immutable ciphertext and a
derived search index in SQLite, and exposes authorized retrieval through a local
stdio MCP server.

## Data flow

```text
~/.codex/{sessions,archived_sessions}
  -> tracehub sync
  -> complete JSONL lines / gzip / age X25519
  -> HTTPS / Ed25519 request signature
  -> tracehub serve / SQLite
  -> tracehub mcp / Codex
```

TraceHub does not run background synchronization. Encrypted `chunks` are the
source of truth. The searchable index excludes reasoning, and complete tool
output is decrypted only when explicitly requested.

## Build

Go 1.26 is required:

```bash
make test
make build
make dist
```

`make dist` builds macOS and Linux binaries for amd64 and arm64.

## Keys

Generate an age X25519 server key:

```bash
tracehub keygen server \
  --private /etc/tracehub/keys/server.key \
  --public ./server.pub
```

Generate a separate Ed25519 key for each device:

```bash
tracehub keygen device \
  --private ./device.key \
  --public ./desktop.pub
```

Private keys are created with mode `0600` and existing files are never
overwritten. Server public keys are configured explicitly on clients; device
public keys are registered manually in the server configuration.

## Configuration

Configuration uses strict JSON and rejects unknown fields. Start from
`config/server.example.json` and `config/client.example.json`.

The server `server_private_keys` field is a keyring capable of decrypting old
archives. Clients encrypt new chunks only with the public key selected by
`server_key_id`.

`codex_dir` points to the Codex root, normally `~/.codex`. TraceHub scans
`sessions` and `archived_sessions`, rejecting symlinks, duplicate sessions,
file/session UUID mismatches, and truncated source histories.

## Server

```bash
tracehub serve --config /etc/tracehub/server.json
```

The default listener is `127.0.0.1:8080`. Put an existing HTTPS reverse proxy
in front of it, allow request bodies of at least 66 MiB, and do not log request
or response bodies. `GET /healthz` is the only unsigned endpoint.

The systemd unit is `deploy/systemd/tracehub.service`.

### Single Docker container

Pull the published Linux amd64/arm64 image:

```bash
docker pull ghcr.io/streamsc/tracehub:v0.1.0-alpha.3
```

Or build it from source:

```bash
docker build \
  -f deploy/docker/Dockerfile \
  -t tracehub:0.1.0-alpha.3 \
  .

cd deploy/docker
cp server.example.json server.json
mkdir keys
# Place server.key and device public keys in keys/.
sudo chown -R 10001:10001 keys
chmod 600 keys/server.key
docker volume create tracehub-data

docker run -d \
  --name tracehub \
  --restart no \
  -p 127.0.0.1:8080:8080 \
  -v "$PWD/server.json:/etc/tracehub/server.json:ro" \
  -v "$PWD/keys:/etc/tracehub/keys:ro" \
  -v tracehub-data:/var/lib/tracehub \
  --health-cmd 'wget -q -O - http://127.0.0.1:8080/healthz' \
  --health-interval 30s \
  --health-timeout 5s \
  --health-retries 3 \
  ghcr.io/streamsc/tracehub:v0.1.0-alpha.3
```

### Docker Compose

```bash
cd deploy/docker
cp server.example.json server.json
mkdir keys
# Place server.key and device public keys in keys/.
sudo chown -R 10001:10001 keys
chmod 600 keys/server.key
docker compose up --build
```

Both deployment modes run `tracehub serve` as UID/GID `10001` and persist
SQLite at `/var/lib/tracehub/tracehub.db`.

## Sync

```bash
tracehub sync --config ./client.json
```

The server reports the authoritative byte offset for each session. The client
uploads only newly appended, newline-terminated JSONL records. Normal chunks
target 4 MiB, oversized records use a dedicated chunk, and a record larger than
64 MiB is rejected. Repeating a sync is idempotent.

## Codex MCP

```toml
[mcp_servers.tracehub]
command = "/usr/local/bin/tracehub"
args = ["mcp", "--config", "/Users/you/.config/tracehub/client.json"]
```

The MCP server provides `list_devices`, `search_sessions`, `get_session_info`,
`read_session`, and `read_tool_output`. Archived content is explicitly marked as
untrusted. Reasoning is never returned through MCP.

## Administration

```bash
tracehub admin export-session \
  --config /etc/tracehub/server.json \
  --device desktop \
  --session 019ffdf2-452e-7c60-bd5d-4d88b56ef31b \
  --output ./session.jsonl

tracehub admin delete-session \
  --config /etc/tracehub/server.json \
  --device desktop \
  --session 019ffdf2-452e-7c60-bd5d-4d88b56ef31b
```

Deletion removes archive chunks, indexes, and FTS records and truncates the
SQLite WAL. Retention is otherwise permanent; MCP and HTTP do not expose remote
deletion.

## Security boundary

- The server and TLS reverse proxy are trusted and can decrypt full sessions.
- Raw sessions remain age-encrypted in SQLite; search indexes and conversation
  text are plaintext and require restricted filesystem permissions and disk
  encryption.
- Request signatures do not replace HTTPS.
- Codex JSONL is isolated behind `internal/codex` and is not a public TraceHub
  protocol.

## Project governance

- [Contributing](CONTRIBUTING.md)
- [Requirements](docs/requirements/README.md)
- [Release management](docs/releasing.md)
- [Changelog](CHANGELOG.md)

Licensed under the [Apache License 2.0](LICENSE).
