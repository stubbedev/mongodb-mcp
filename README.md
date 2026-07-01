# mongodb-mcp

An [MCP](https://modelcontextprotocol.io) server that exposes MongoDB to MCP
clients. It runs on the **same machine** as its consumer and speaks both
**stdio** and **streamable HTTP**, including cleanly behind an MCP/reverse proxy.

- Multiple named **sources** — direct or tunnelled over **SSH**.
- Per-source **read-only** mode that refuses every write/admin tool.
- **XDG-compliant** JSON config with an auto-generated **JSON Schema**.
- Built with maintained SDKs: the official [Go MCP SDK](https://github.com/modelcontextprotocol/go-sdk),
  the official [MongoDB Go driver v2](https://go.mongodb.org/mongo-driver),
  `golang.org/x/crypto/ssh`, `adrg/xdg`, and `invopop/jsonschema`.
- Auto-generated repo docs (`docs/`) and a CI-maintained `flake.nix`.

## Install

### Go

```sh
go install github.com/stubbedev/mongodb-mcp/cmd/mongodb-mcp@latest
```

### Nix

```sh
nix run github:stubbedev/mongodb-mcp -- --version
# or add the flake's packages.default to your system/devShell
```

## Configuration

Configuration is a single JSON file located via the XDG spec at
`$XDG_CONFIG_HOME/mongodb-mcp/config.json` (override with `--config`). Its schema
lives in [`config.schema.json`](config.schema.json); reference it with `$schema`
for editor completion. See [`config.example.json`](config.example.json).

```json
{
  "$schema": "./config.schema.json",
  "server": { "name": "mongodb-mcp", "version": "0.1.0" },
  "http": { "addr": "127.0.0.1:8080", "path": "/mcp" },
  "sources": {
    "local": {
      "uri": "mongodb://localhost:27017",
      "description": "Local dev database; safe to read and write.",
      "default_database": "test"
    },
    "prod": {
      "uri": "mongodb://app:${PROD_MONGO_PW}@db.internal:27017/?authSource=admin",
      "description": "Production replica for read-only analytics.",
      "readonly": true,
      "default_database": "app",
      "operation_timeout": "20s",
      "ssh": {
        "host": "bastion.example.com",
        "user": "deploy",
        "identity_file": "~/.ssh/id_ed25519",
        "use_agent": true,
        "known_hosts_file": "~/.ssh/known_hosts"
      }
    }
  }
}
```

### Sources

Each entry under `sources` is a named MongoDB connection. Tools select one by
name via their `source` argument.

- **Direct**: just set `uri` (local or remote, with credentials in the URI).
- **SSH tunnel**: add an `ssh` block. The MongoDB connection — handshake,
  monitoring and queries — is dialled through the SSH server, so the `uri` hosts
  are resolved from the SSH server's network. Authentication mirrors an
  interactive `ssh` client and tries, in order: agent (`use_agent`), identity
  file (`identity_file`/`passphrase`), then `password`. Host keys are verified
  against `known_hosts_file` (default `~/.ssh/known_hosts`) unless
  `insecure_ignore_host_key` is set.
- **Read-only**: `"readonly": true` makes every write and admin tool refuse the
  source while keeping read tools available.
- **Description**: `"description"` is a free-text summary surfaced by the
  `listSources` tool so a model can pick the right source for a task on its own.
- **Secrets**: `uri` and the ssh `password`/`passphrase` support `${ENV_VAR}`
  expansion, so credentials need not live in the file (important for a
  per-workspace `.mongodb-mcp.json` that may be committed).
- **Timeouts**: `connect_timeout` bounds the initial connect/ping (default 10s);
  `operation_timeout` caps every individual operation (default 30s, `"0s"`
  disables).

### Per-workspace config (MCP roots)

One server can serve several clients, each with its own sources — no need to
tell the model which connection to use. On every tool call the server resolves
the config from the calling client's **MCP workspace root**:

1. If a client root contains a `.mongodb-mcp.json`, that file's `sources` are
   used (same schema as the global config; loaded per client, reloaded when the
   file changes).
2. Otherwise the server falls back to its global `--config` / XDG config.
3. If neither exists, the call returns an error asking for a workspace config.

This works over **both stdio and HTTP** — the root comes from the MCP `roots`
capability the client advertises (e.g. Claude Code exposes the open workspace).
A proxy that cannot use the roots protocol may inject roots via a request
header: `X-Mcp-Roots: file:///path/to/repo` (comma-separated for several;
`X-Mcp-Root` / `Mcp-Roots` / `Mcp-Root` also accepted).

Header roots are **request-scoped**: they apply only to the request that carries
them and are never cached on the session, so a proxy multiplexing several
clients over one MCP session can vary them per request without one client seeing
another's sources — but it must send the header on *every* request. The
`roots/list` protocol result is cached per session (one client per session, as
for stdio and stateful HTTP).

Because the config can come entirely from clients, the global config is
**optional**: start `mongodb-mcp` with no `--config` and it runs in roots-only
mode, serving each client from its own `.mongodb-mcp.json`.

```json
// <repo>/.mongodb-mcp.json — per project (gitignore it if it holds secrets)
{
  "sources": {
    "app": { "uri": "mongodb://localhost:27017", "default_database": "app", "readonly": true }
  }
}
```

## Running

### stdio (default)

```sh
mongodb-mcp --config ./config.json
```

Example MCP client config:

```json
{
  "mcpServers": {
    "mongodb": {
      "command": "mongodb-mcp",
      "args": ["--config", "/home/me/.config/mongodb-mcp/config.json"]
    }
  }
}
```

### Streamable HTTP

```sh
mongodb-mcp --transport http --http-addr 127.0.0.1:8080 --http-path /mcp
```

The endpoint is served at `http://127.0.0.1:8080/mcp`; `GET /healthz` returns
`ok`.

### Behind an MCP / reverse proxy

The HTTP transport is proxy-aware via the `http` config block:

| Field | Purpose |
|---|---|
| `path` | Match the path your proxy forwards. |
| `stateless` | Drop server-side session state for load-balanced proxies that can't pin `Mcp-Session-Id`. |
| `json_response` | Return `application/json` instead of an SSE stream. |
| `trust_proxy` | Disable Host-header DNS-rebinding protection when a proxy forwards a non-localhost `Host` to the loopback listener. |
| `allowed_origins` | Trusted browser `Origin`s. Empty = default cross-origin protection; `["*"]` = disable it (trust the proxy). |

## Tools

All tools take a `source` argument naming the configured connection, plus
`database` (falls back to the source's `default_database`) and `collection`
where relevant. Document/filter/pipeline arguments are JSON / MongoDB Extended
JSON strings.

| Tool | Kind | Description |
|---|---|---|
| `listSources` | read | List configured sources with their description, read-only flag and default database. |
| `find` | read | Query documents (filter, projection, sort, limit, skip). |
| `aggregate` | read | Run an aggregation pipeline. |
| `count` | read | Count documents matching a filter. |
| `distinct` | read | Distinct values for a field. |
| `listDatabases` | read | List database names. |
| `listCollections` | read | List collection names. |
| `indexes` | read | List a collection's indexes. |
| `insertOne` / `insertMany` | write | Insert document(s). |
| `updateOne` / `updateMany` | write | Update document(s), optional upsert. |
| `deleteOne` / `deleteMany` | write | Delete document(s). |
| `createIndex` / `dropIndex` | admin | Manage indexes. |
| `createCollection` / `dropCollection` | admin | Manage collections. |

Write and admin tools are refused on read-only sources.

## Development

This repo uses [`just`](https://github.com/casey/just). Run `just` to list
recipes:

```sh
nix develop            # go, golangci-lint, gomarkdoc, mongosh
just build             # -> ./bin/mongodb-mcp
just test              # unit tests
just test-integration  # real MongoDB via testcontainers (needs Docker)
just lint              # fmt + vet + golangci-lint
just check             # lint + test + sync-schema + sync-docs + sync-flake
just install-hooks     # enable the pre-commit format gate
```

Generated artifacts are regenerated by their own recipes and kept current by CI:

- `just sync-schema` — `config.schema.json` from the Go config types.
- `just sync-docs` — gomarkdoc API docs under [`docs/`](docs/).
- `just sync-flake` — re-aligns the flake `vendorHash` with `go.sum` (cached on a
  `# go-sum:` digest line; pass `--force` to rebuild regardless).

CI (`.github/workflows/generate.yml`) regenerates and commits these on push, and
`ci.yml` gates PRs on no drift. Release: `just release-preview`, then
`just release-{patch,minor,major}` (bumps `VERSION`, syncs the flake, tags, pushes).

## License

MIT
