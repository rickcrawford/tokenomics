# Configuration

Tokenomics loads configuration from a YAML file, environment variables, and CLI flags. Values are resolved in this order (highest priority first):

1. CLI flags
2. Environment variables
3. Config file
4. Defaults

## Config File

By default, Tokenomics looks for `config.yaml` in:

1. The current working directory
2. `$HOME/.tokenomics/`

You can specify a custom path with `--config`:

```bash
./bin/tokenomics serve --config /etc/tokenomics/config.yaml
```

### Full Reference

```yaml
server:
  http_port: 8080          # HTTP listener port (set to 0 to disable)
  https_port: 8443         # HTTPS listener port
  upstream_url: "https://api.openai.com"  # Default upstream API
  tls:
    enabled: true          # Enable the HTTPS listener
    auto_gen: true         # Auto-generate CA + server certificates on first run
    cert_dir: "./certs"    # Directory for auto-generated certificates
    cert_file: ""          # Path to a custom TLS certificate (disables auto_gen)
    key_file: ""           # Path to a custom TLS private key (disables auto_gen)

storage:
  db_path: "./tokenomics.db"  # BoltDB file for token storage

session:
  backend: "memory"        # Session backend: "memory" or "redis"
  redis:
    addr: "localhost:6379" # Redis address (only used when backend is "redis")
    password: ""           # Redis password
    db: 0                  # Redis database number

security:
  hash_key_env: "TOKENOMICS_HASH_KEY"          # Env var name that holds the HMAC key
  encryption_key_env: "TOKENOMICS_ENCRYPTION_KEY"  # Env var for AES-256 at-rest encryption

logging:
  level: "info"              # "debug", "info", "warn", "error"
  format: "json"             # "json" or "text"
  request_body: false        # Log full request bodies
  response_body: false       # Log full response bodies
  hide_token_hash: false     # Mask token hashes in logs
  disable_request: false     # Suppress per-request structured logs

events:
  webhooks:
    - url: "http://localhost:9999/webhook"
      secret: ""             # Shared secret (X-Webhook-Secret header)
      signing_key: ""        # HMAC-SHA256 key (X-Webhook-Signature header)
      events: []             # Filter by event type (empty = all). Supports trailing wildcard (token.*)
      timeout: 10            # HTTP timeout in seconds

remote:
  url: ""                    # Central config server URL (e.g. http://config-server:9090)
  api_key: ""                # Shared API key for authentication
  sync: 0                    # Sync interval in seconds (0 = startup only)
  insecure: false            # Skip TLS verification
  webhook:
    enabled: false           # Enable inbound webhook endpoint for push-based sync
    path: "/v1/webhook"      # URL path for the receiver endpoint
    secret: ""               # Expected X-Webhook-Secret header value
    signing_key: ""          # HMAC-SHA256 key for X-Webhook-Signature verification

cli_maps:                    # Map CLI names to providers for `tokenomics run`
  claude: anthropic          # `tokenomics run claude ...` uses anthropic provider
  anthropic: anthropic
  python: generic
  node: generic
  curl: generic

# ── Session Ledger ───────────────────────────────────────────────────
ledger:
  enabled: false             # Enable per-session token tracking to .tokenomics/
  dir: ".tokenomics"         # Directory for session files
  memory: true               # Record conversation content in memory/ subdirectory
```

### Field Descriptions

| Field | Default | Description |
|-------|---------|-------------|
| `server.http_port` | `8080` | Port for the HTTP listener. Set to `0` to disable. |
| `server.https_port` | `8443` | Port for the HTTPS listener. |
| `server.upstream_url` | `https://api.openai.com` | Default upstream URL for proxied requests. Per-policy overrides take precedence. |
| `server.tls.enabled` | `true` | Whether to start the HTTPS listener. |
| `server.tls.auto_gen` | `true` | Auto-generate a root CA and server certificate on first run. |
| `server.tls.cert_dir` | `./certs` | Directory where auto-generated certs are stored. |
| `server.tls.cert_file` | (empty) | Path to a custom server certificate. When set, `auto_gen` is bypassed. |
| `server.tls.key_file` | (empty) | Path to a custom server private key. When set, `auto_gen` is bypassed. |
| `storage.db_path` | `./tokenomics.db` | Path to the BoltDB database file. |
| `session.backend` | `memory` | Session tracking backend. Use `"redis"` for persistence across restarts. |
| `session.redis.addr` | `localhost:6379` | Redis server address. |
| `session.redis.password` | (empty) | Redis password. |
| `session.redis.db` | `0` | Redis database number. |
| `security.hash_key_env` | `TOKENOMICS_HASH_KEY` | Name of the environment variable that contains the HMAC secret used for token hashing. |
| `security.encryption_key_env` | `TOKENOMICS_ENCRYPTION_KEY` | Name of the env var for AES-256-GCM at-rest encryption of policies in BoltDB. |
| `logging.level` | `info` | Log level: `debug`, `info`, `warn`, `error`. |
| `logging.format` | `json` | Log format: `json` (structured) or `text`. |
| `logging.request_body` | `false` | Include full request body in logs. |
| `logging.response_body` | `false` | Include full response body in logs. |
| `logging.hide_token_hash` | `false` | Replace token hashes with `****` in logs. |
| `logging.disable_request` | `false` | Suppress per-request structured log entries entirely. |
| `events.webhooks[].url` | (required) | Webhook endpoint URL. |
| `events.webhooks[].secret` | (empty) | Shared secret sent as `X-Webhook-Secret`. |
| `events.webhooks[].signing_key` | (empty) | HMAC-SHA256 signing key for `X-Webhook-Signature`. |
| `events.webhooks[].events` | `[]` (all) | Event type filter. Supports trailing wildcard (`token.*`). |
| `events.webhooks[].timeout` | `10` | HTTP timeout in seconds for webhook delivery. |
| `remote.url` | (empty) | URL of a remote config server to sync tokens from. |
| `remote.api_key` | (empty) | API key for authenticating with the remote server. |
| `remote.sync` | `0` | Sync interval in seconds. `0` means sync at startup only. |
| `remote.insecure` | `false` | Skip TLS certificate verification for the remote server. |
| `remote.webhook.enabled` | `false` | Enable a webhook receiver endpoint on the proxy for push-based token sync. |
| `remote.webhook.path` | `/v1/webhook` | URL path for the inbound webhook endpoint. |
| `remote.webhook.secret` | (empty) | Expected value of the `X-Webhook-Secret` header on inbound webhooks. |
| `remote.webhook.signing_key` | (empty) | HMAC-SHA256 key for verifying `X-Webhook-Signature` on inbound webhooks. |
| `cli_maps` | (see below) | Maps CLI names to providers for auto-detection in `tokenomics run`. Example: `claude: anthropic` means `tokenomics run claude` uses the anthropic provider. |
| `ledger.enabled` | `false` | Enable per-session token tracking to the `.tokenomics/` directory. |
| `ledger.dir` | `.tokenomics` | Directory for session files and memory logs. |
| `ledger.memory` | `true` | Record conversation content (user/assistant messages) in memory markdown files. |

## Logging

### File Output

By default, logs are written to stdout. To write logs to a file, set the `TOKENOMICS_LOG_FILE` environment variable:

```bash
export TOKENOMICS_LOG_FILE="~/.tokenomics/tokenomics.log"
./bin/tokenomics serve
```

The log file will be created if it doesn't exist, and logs will be appended on subsequent runs.

## Environment Variables

Every config field can be overridden with a `TOKENOMICS_` prefixed environment variable. Dots and nesting are replaced with underscores:

| Config Field | Environment Variable |
|-------------|---------------------|
| `server.http_port` | `TOKENOMICS_SERVER_HTTP_PORT` |
| `server.https_port` | `TOKENOMICS_SERVER_HTTPS_PORT` |
| `server.upstream_url` | `TOKENOMICS_SERVER_UPSTREAM_URL` |
| `server.tls.enabled` | `TOKENOMICS_SERVER_TLS_ENABLED` |
| `server.tls.auto_gen` | `TOKENOMICS_SERVER_TLS_AUTO_GEN` |
| `server.tls.cert_dir` | `TOKENOMICS_SERVER_TLS_CERT_DIR` |
| `server.tls.cert_file` | `TOKENOMICS_SERVER_TLS_CERT_FILE` |
| `server.tls.key_file` | `TOKENOMICS_SERVER_TLS_KEY_FILE` |
| `storage.db_path` | `TOKENOMICS_STORAGE_DB_PATH` |
| `session.backend` | `TOKENOMICS_SESSION_BACKEND` |
| `session.redis.addr` | `TOKENOMICS_SESSION_REDIS_ADDR` |
| `session.redis.password` | `TOKENOMICS_SESSION_REDIS_PASSWORD` |
| `session.redis.db` | `TOKENOMICS_SESSION_REDIS_DB` |
| `security.hash_key_env` | `TOKENOMICS_SECURITY_HASH_KEY_ENV` |
| `security.encryption_key_env` | `TOKENOMICS_SECURITY_ENCRYPTION_KEY_ENV` |
| (logging output) | `TOKENOMICS_LOG_FILE` |
| `logging.level` | `TOKENOMICS_LOGGING_LEVEL` |
| `logging.format` | `TOKENOMICS_LOGGING_FORMAT` |
| `logging.disable_request` | `TOKENOMICS_LOGGING_DISABLE_REQUEST` |
| `remote.url` | `TOKENOMICS_REMOTE_URL` |
| `remote.api_key` | `TOKENOMICS_REMOTE_API_KEY` |
| `remote.sync` | `TOKENOMICS_REMOTE_SYNC` |
| `ledger.enabled` | `TOKENOMICS_LEDGER_ENABLED` |
| `ledger.dir` | `TOKENOMICS_LEDGER_DIR` |
| `ledger.memory` | `TOKENOMICS_LEDGER_MEMORY` |

Example:

```bash
TOKENOMICS_SERVER_HTTPS_PORT=9443 ./bin/tokenomics serve
```

### Special Environment Variables

These are not config fields but are read directly at runtime:

| Variable | Purpose |
|---|---|
| `TOKENOMICS_HASH_KEY` | The HMAC secret key used to hash wrapper tokens. Required for `token create` and `serve`. |
| Whatever `base_key_env` points to (e.g. `OPENAI_API_KEY`) | The real upstream API key. Resolved per-policy at request time. |

## CLI Flags

### Global Flags

These flags are available on all subcommands:

| Flag | Description |
|------|-------------|
| `--config <path>` | Path to a config file. Overrides the default search paths. |
| `--db <path>` | Path to the BoltDB database. Overrides `storage.db_path` from config. |

### `token` Subcommands

| Command | Flag | Description |
|---|---|---|
| `token create` | `--policy <json>` | Policy JSON string or `@file.json` (required) |
| `token create` | `--expires <value>` | Expiration: duration (`24h`, `7d`, `1y`), RFC3339, or omit for no expiry |
| `token get` | `--hash <hash>` | Token hash to retrieve (required) |
| `token update` | `--hash <hash>` | Token hash to update (required) |
| `token update` | `--policy <json>` | New policy JSON (optional, at least one of --policy or --expires required) |
| `token update` | `--expires <value>` | New expiration, or `clear` to remove (optional) |
| `token delete` | `--hash <hash>` | Token hash to delete (required) |
| `token list` | (none) | Lists all tokens and their policies |

### `start` / `stop` Commands

| Command | Flag | Default | Description |
|---|---|---|---|
| `start` | `--host` | `localhost` | Proxy hostname |
| `start` | `--port` | `8443` | Proxy port |
| `start` | `--tls` | `true` | Use HTTPS |
| `start` | `--pid-file` | `~/.tokenomics/tokenomics.pid` | PID file path |
| `start` | `--log-file` | `~/.tokenomics/tokenomics.log` | Log file path |
| `stop` | `--pid-file` | `~/.tokenomics/tokenomics.pid` | PID file path |

### `provider` Subcommands

| Command | Flag | Default | Description |
|---|---|---|---|
| `provider list` | `--output` | `table` | Output format: `table` or `json` |
| `provider test` | `--insecure` | `false` | Skip TLS verification for connectivity tests |
| `provider test` | `[provider...]` | (all) | Test specific providers or all if none given |
| `provider models` | `[provider]` | (all) | Show models for a specific provider or all |

### `ledger` Subcommands

| Command | Flag | Default | Description |
|---|---|---|---|
| `ledger summary` | `--dir` | from config or `.tokenomics` | Ledger directory |
| `ledger summary` | `--json` | `false` | Output as JSON |
| `ledger sessions` | `--dir` | from config or `.tokenomics` | Ledger directory |
| `ledger sessions` | `--json` | `false` | Output as JSON |
| `ledger show` | `--dir` | from config or `.tokenomics` | Ledger directory |
| `ledger show` | `--json` | `false` | Output as JSON |

### `doctor` / `status` / `version` Commands

These commands take no additional flags beyond the global `--config` and `--db` flags.

| Command | Description |
|---|---|
| `doctor` | Check config, database, security keys, provider keys, TLS, proxy state, remote config |
| `status` | Show proxy state, environment variables, provider key status, token count |
| `version` | Print version, commit, build date, Go version, OS/arch |

### `remote` Command

| Flag | Default | Description |
|---|---|---|
| `--addr` | `:9090` | Listen address (host:port) |
| `--api-key` | (empty) | API key for authenticating client requests |

### `init` / `run` Commands

See [Agent Integration](AGENT_INTEGRATION.md) for full details on `init` and `run` flags.

### `completion` Command

```bash
tokenomics completion [bash|zsh|fish|powershell]
```

Generates shell completion scripts. See `tokenomics completion --help` for per-shell installation instructions.
