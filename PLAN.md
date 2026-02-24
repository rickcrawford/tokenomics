# Tokenomics — OpenAI Reverse Proxy Implementation Plan

## Overview

A Go-based reverse proxy that sits in front of OpenAI (and compatible) APIs. It issues "wrapper tokens" that map to policies and real provider credentials (referenced by env var name, never stored as secrets). The proxy intercepts requests, enforces policies (model restrictions, token budgets, prompt rules), injects system prompts, and forwards requests using the real provider key.

---

## Architecture

```
                         tokenomics init
                         ┌─────────────────────────┐
                         │ Sets env vars / config:  │
                         │  OPENAI_API_KEY=tkn_xxx  │
                         │  OPENAI_BASE_URL=proxy   │
                         └────────────┬────────────┘
                                      │
                                      ▼
                         Agent CLI (aider, claude, etc.)
                         Uses wrapper-token as Bearer
                                      │
                                      ▼
┌──────────────────────────────────────────────────────┐
│                 Tokenomics Proxy                      │
├──────────────────────────────────────────────────────┤
│   TLS Termination        (auto-gen or provided)      │
├──────────────────────────────────────────────────────┤
│   Reverse Proxy Handler                              │
│   1. Extract bearer (wrapper-token)                  │
│   2. Hash token (HMAC-SHA256)                        │
│   3. Lookup policy (in-memory from SQLite)            │
│   4. Enforce model (exact match / regex)             │
│   5. Check regex rules against user prompt           │
│   6. Inject policy prompts before user messages      │
│   7. Count input tokens (tiktoken)                   │
│   8. Check session budget                            │
│   9. Resolve base key from env var                   │
│   10. Forward to upstream provider                   │
│   11. Count response tokens, update session          │
├──────────────────────────────────────────────────────┤
│   Session Manager        (in-memory / Redis)         │
│   Token Counter by hash                              │
├──────────────────────────────────────────────────────┤
│   Token Store (SQLite → memory, file-watch reload)   │
│   hash → policy JSON                                 │
└──────────────────────────────────────────────────────┘
                                      │
                                      ▼
                         Upstream Provider (OpenAI, etc.)
                         (Bearer: real API key from env)
```

---

## Components

### 1. CLI (cobra + viper)

Commands:
- `tokenomics serve` — Start the reverse proxy
- `tokenomics token create --policy '<json>'` — Create a wrapper token, print it once
- `tokenomics token delete --token <token>` — Delete a wrapper token
- `tokenomics token list` — List all wrapper tokens (hashed) and their policies
- `tokenomics init --token <wrapper-token> [--port <port>] [--host <host>] [--tls] [--cli <name>]` — Configure an agent CLI to use the proxy

Global flags: `--config`, `--db`

### 1b. Init Command (Agent Framework Injection)

The `init` command acts as the glue between the proxy and any downstream agent CLI
(e.g., `aider`, `claude`, `openai`, `continue`, or a custom tool). It rewrites the
agent framework's environment / config so that all API calls route through the proxy.

**What it does:**
1. Accepts a wrapper token and optional proxy host/port/tls settings
2. Detects or is told (`--cli`) which agent framework is in use
3. Writes/updates the relevant environment variables or config files so the
   framework's API base URL points at the proxy and the bearer token is the
   wrapper token

**Supported configuration targets (extensible):**

| Target | Env Vars / Config Set |
|--------|-----------------------|
| Generic / OpenAI SDK | `OPENAI_API_KEY=<wrapper-token>`, `OPENAI_BASE_URL=https://<host>:<port>/v1` |
| Anthropic SDK | `ANTHROPIC_API_KEY=<wrapper-token>`, `ANTHROPIC_BASE_URL=https://<host>:<port>` |
| Azure OpenAI | `AZURE_OPENAI_API_KEY=<wrapper-token>`, `AZURE_OPENAI_ENDPOINT=https://<host>:<port>` |
| Custom | `--env-key <KEY_NAME>`, `--env-base-url <URL_VAR>` for arbitrary env var names |

**Output modes:**
- `--shell` (default): Prints `export` statements to stdout for `eval $(tokenomics init ...)`
- `--dotenv`: Appends/updates a `.env` file at a given path
- `--json`: Outputs JSON for programmatic consumption

**Example usage:**
```bash
# Quick: eval into current shell
eval $(tokenomics init --token tkn_abc123 --port 8443 --tls)

# Write to a .env file for a project
tokenomics init --token tkn_abc123 --port 8443 --tls --dotenv .env

# Specify a particular CLI/SDK target
tokenomics init --token tkn_abc123 --cli anthropic --port 8443 --tls --shell

# Custom env var names
tokenomics init --token tkn_abc123 --env-key MY_API_KEY --env-base-url MY_BASE_URL --port 8443
```

**Flags:**
- `--token` (required): The wrapper token to inject
- `--host` (default: `localhost`): Proxy hostname
- `--port` (default: from config or `8443`): Proxy port
- `--tls` (default: `true`): Use https scheme
- `--insecure` (default: `false`): Skip TLS verification (for self-signed certs)
- `--cli` (default: `generic`): Target CLI/SDK (`generic`, `anthropic`, `azure`, `custom`)
- `--env-key`: Custom env var name for the API key
- `--env-base-url`: Custom env var name for the base URL
- `--shell` / `--dotenv <path>` / `--json`: Output mode

### 2. Configuration (viper)

File: `config.yaml` (or env vars / CLI flags)

```yaml
server:
  http_port: 8080
  https_port: 8443
  tls:
    enabled: true
    cert_file: ""      # blank = auto-generate
    key_file: ""
    auto_gen: true     # generate self-signed if cert/key missing
  upstream_url: "https://api.openai.com"

storage:
  db_path: "./tokenomics.db"

session:
  backend: "memory"    # "memory" or "redis"
  redis:
    addr: "localhost:6379"
    password: ""
    db: 0

security:
  hash_key_env: "TOKENOMICS_HASH_KEY"  # env var holding HMAC key for hashing tokens
```

Env vars loaded from `.env` file via viper.

### 3. Policy Schema (JSON)

```json
{
  "base_key_env": "OPENAI_API_KEY",
  "max_tokens": 100000,
  "model": "gpt-4o",
  "model_regex": "^gpt-4.*",
  "prompts": [
    {"role": "system", "content": "You are a helpful assistant for Acme Corp."},
    {"role": "system", "content": "Never reveal internal details."}
  ],
  "rules": [
    "(?i)ignore.*instructions",
    "(?i)system.*prompt"
  ]
}
```

- `base_key_env` (required): Name of env var holding the real API key
- `max_tokens` (optional, 0 = unlimited): Session token budget per wrapper token
- `model` (optional, blank = any): Exact model name allowed
- `model_regex` (optional, blank = any): Regex pattern for allowed models
- `prompts` (optional): List of messages injected before user messages
- `rules` (optional): List of regex patterns — if any match user prompt text, block the request

### 4. SQLite Token Store

Table: `tokens`
```sql
CREATE TABLE tokens (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    token_hash TEXT UNIQUE NOT NULL,
    policy TEXT NOT NULL,  -- JSON blob
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

- On startup: load entire table into `sync.RWMutex`-protected map
- Background goroutine: stat the DB file every N seconds, reload if mtime changes
- CLI commands write directly to SQLite; the running server picks up changes via reload

### 5. Proxy Handler Flow

1. Parse `Authorization: Bearer <wrapper-token>` header
2. HMAC-hash the wrapper token using key from `TOKENOMICS_HASH_KEY` env
3. Lookup hashed token in in-memory store → get policy
4. Parse request body (OpenAI chat completion format)
5. **Model check**: if policy has `model` or `model_regex`, validate `request.model`
6. **Rules check**: iterate `rules` regexes against all user message content; block if match
7. **Prompt injection**: prepend `prompts` list to `messages` array
8. **Token count** (input): count tokens using tiktoken, check session budget
9. Replace `Authorization` header with `Bearer <resolved base_key>`
10. Forward request to upstream
11. Read response, **count output tokens**, update session counter
12. Return response to client

Streaming: For SSE streaming responses, count tokens from each chunk incrementally.

### 6. Session / Token Counter

Interface:
```go
type SessionStore interface {
    GetUsage(tokenHash string) (int64, error)
    AddUsage(tokenHash string, count int64) (int64, error)
    Reset(tokenHash string) error
}
```

Implementations:
- `MemorySessionStore` — `sync.RWMutex` + `map[string]int64`
- `RedisSessionStore` — Redis INCRBY on hashed key

### 7. TLS / Certificate Manager

- If `tls.cert_file` and `tls.key_file` are provided and exist, use them
- Otherwise, auto-generate a self-signed CA + server cert using `crypto/x509`
- Store generated certs in a configurable directory (default: `./certs/`)
- Log a warning that auto-generated certs are self-signed

### 8. Token Hashing

- Use HMAC-SHA256 with key from env var (`TOKENOMICS_HASH_KEY`)
- All token lookups, storage, and session tracking use the hash — never the raw token
- CLI `token create` generates a random token (e.g., `tkn_<32-byte-hex>`), stores the HMAC hash, and prints the raw token once

---

## Directory Structure

```
tokenomics/
├── cmd/
│   └── tokenomics/
│       └── main.go
├── internal/
│   ├── cli/
│   │   ├── root.go          # cobra root command + viper setup
│   │   ├── serve.go         # serve command
│   │   ├── token.go         # token create/delete/list commands
│   │   └── init.go          # init command — configure agent CLI to use proxy
│   ├── config/
│   │   └── config.go        # viper config struct + loading
│   ├── proxy/
│   │   ├── handler.go       # main HTTP handler
│   │   ├── middleware.go     # auth extraction, policy enforcement
│   │   └── rewrite.go       # request rewriting (prompt injection, header swap)
│   ├── policy/
│   │   ├── policy.go        # policy struct + validation
│   │   └── engine.go        # model check, rules check, prompt injection
│   ├── store/
│   │   ├── sqlite.go        # SQLite operations + in-memory cache
│   │   └── store.go         # store interface
│   ├── session/
│   │   ├── memory.go        # in-memory session store
│   │   ├── redis.go         # redis session store
│   │   └── session.go       # session store interface
│   ├── tls/
│   │   └── certgen.go       # auto-generate self-signed certs
│   └── tokencount/
│       └── counter.go       # tiktoken wrapper
├── config.yaml               # default config
├── .env.example              # example env vars
├── go.mod
├── go.sum
└── Makefile
```

---

## Dependencies

| Package | Purpose |
|---------|---------|
| `github.com/spf13/cobra` | CLI framework |
| `github.com/spf13/viper` | Configuration |
| `github.com/pkoukk/tiktoken-go` | Token counting (OpenAI's tiktoken port) |
| `github.com/mattn/go-sqlite3` | SQLite driver (CGO) |
| `github.com/redis/go-redis/v9` | Redis client (optional) |
| `github.com/joho/godotenv` | .env file loading |
| `net/http/httputil` | Reverse proxy (stdlib) |
| `crypto/x509`, `crypto/ecdsa` | Cert generation (stdlib) |

Alternative: `modernc.org/sqlite` (pure Go, no CGO) — avoids needing a C compiler.

---

## Implementation Order

1. Project scaffolding (go.mod, directory structure, Makefile)
2. Config loading (viper + .env)
3. Policy struct + validation
4. SQLite store with in-memory cache + file-watch reload
5. Token hashing (HMAC-SHA256)
6. CLI: token create / delete / list
7. CLI: init command (agent framework injection)
8. TLS cert auto-generation
9. Proxy handler (request parsing, forwarding)
10. Policy engine (model check, rules check, prompt injection)
11. Token counting (tiktoken integration)
12. Session store (memory + redis)
13. Streaming support
14. Serve command (wire everything together)
15. Integration testing

---

## Open Questions

See below — these will be asked interactively.
