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
  hash_key_env: "TOKENOMICS_HASH_KEY"  # Env var name that holds the HMAC key
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
| `token create` | `--policy <json>` | Policy JSON string (required) |
| `token delete` | `--hash <hash>` | Token hash to delete (required) |
| `token list` | (none) | Lists all tokens and their policies |

### `init` Command

See [Agent Integration](AGENT_INTEGRATION.md) for full details on `init` flags.
