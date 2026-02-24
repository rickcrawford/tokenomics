```
  _____     _                            _
 |_   _|__ | | _____ _ __   ___  _ __ __(_) ___ ___
   | |/ _ \| |/ / _ \ '_ \ / _ \| '_ ` _ \| |/ __/ __|
   | | (_) |   <  __/ | | | (_) | | | | | | | (__\__ \
   |_|\___/|_|\_\___|_| |_|\___/|_| |_| |_|_|\___|___/
```

**OpenAI-compatible reverse proxy with token management and policy enforcement.**

Tokenomics sits between your AI agent tools and upstream LLM providers. It issues wrapper tokens mapped to policies that control model access, token budgets, prompt injection, and content rules — without exposing your real API keys.

---

## Features

- **Full `/v1/*` passthrough** — proxies all OpenAI-compatible endpoints; applies policy enforcement on chat completions
- **Wrapper tokens** — issue `tkn_<uuid>` tokens mapped to policies; real keys stay in env vars, never exposed
- **Policy engine** — model allowlists (exact + regex), token budgets, content rules, system prompt injection
- **Per-policy upstream** — route different tokens to different providers (OpenAI, Anthropic, Azure, Gemini, Ollama, etc.)
- **SSE streaming** — first-class streaming support with incremental token counting
- **Structured JSON logging** — every request logged with model, tokens, latency, status
- **Usage stats** — `/stats` endpoint with aggregation by model/key and per-token session tracking
- **Auto-TLS** — generates a CA + server cert on first run; install the CA for trusted local HTTPS
- **Agent CLI injection** — `tokenomics init` configures any agent framework to route through the proxy
- **Pure Go** — no CGO, no C compiler needed; BoltDB for storage, cross-compiles everywhere
- **Chi router** — RequestID, RealIP, Recoverer, Timeout middleware out of the box

---

## Quick Start

### Build

```bash
make build
```

### Create a token

```bash
export TOKENOMICS_HASH_KEY="my-secret-hash-key"

./bin/tokenomics token create --policy '{
  "base_key_env": "OPENAI_API_KEY",
  "max_tokens": 100000,
  "model_regex": "^gpt-4.*",
  "prompts": [{"role": "system", "content": "You are a helpful assistant."}]
}'
```

This prints a `tkn_<uuid>` token once — store it securely.

### Start the proxy

```bash
export OPENAI_API_KEY="sk-your-real-key"
export TOKENOMICS_HASH_KEY="my-secret-hash-key"

./bin/tokenomics serve
```

The proxy starts on `:8443` (HTTPS) and `:8080` (HTTP) with auto-generated certificates.

### Configure your agent

```bash
# Generic OpenAI SDK (aider, continue, openai-python, etc.)
eval $(./bin/tokenomics init --token tkn_your-token-here --port 8443 --insecure)

# Anthropic SDK
eval $(./bin/tokenomics init --token tkn_your-token-here --cli anthropic --port 8443 --insecure)

# Gemini
eval $(./bin/tokenomics init --token tkn_your-token-here --cli gemini --port 8443 --insecure)

# Azure OpenAI
eval $(./bin/tokenomics init --token tkn_your-token-here --cli azure --port 8443 --insecure)

# Write to .env file
./bin/tokenomics init --token tkn_your-token-here --output dotenv --dotenv .env
```

Now run your agent tool as usual — all API calls route through the proxy.

---

## Endpoints

| Endpoint | Description |
|----------|-------------|
| `GET /health` | Health check — returns `{"status":"ok"}` |
| `GET /ping` | Heartbeat (chi middleware) |
| `GET /stats` | Usage stats by model/key + per-token sessions |
| `POST /v1/chat/completions` | Proxied with full policy enforcement |
| `* /v1/*` | Proxied with key swap (passthrough) |

---

## Policy Schema

```json
{
  "base_key_env": "OPENAI_API_KEY",
  "upstream_url": "https://api.openai.com",
  "max_tokens": 100000,
  "model": "gpt-4o",
  "model_regex": "^gpt-4.*",
  "prompts": [
    {"role": "system", "content": "You are a helpful assistant for Acme Corp."}
  ],
  "rules": [
    "(?i)ignore.*instructions",
    "(?i)system.*prompt"
  ]
}
```

| Field | Required | Description |
|-------|----------|-------------|
| `base_key_env` | Yes | Env var name holding the real API key |
| `upstream_url` | No | Override global upstream for this token |
| `max_tokens` | No | Session token budget (0 = unlimited) |
| `model` | No | Exact model name allowed |
| `model_regex` | No | Regex for allowed models |
| `prompts` | No | Messages injected before user messages |
| `rules` | No | Regex patterns — block request if any match user content |

---

## Configuration

`config.yaml`:

```yaml
server:
  http_port: 8080
  https_port: 8443
  tls:
    enabled: true
    auto_gen: true
    cert_dir: "./certs"
  upstream_url: "https://api.openai.com"

storage:
  db_path: "./tokenomics.db"

session:
  backend: "memory"  # or "redis"
  redis:
    addr: "localhost:6379"

security:
  hash_key_env: "TOKENOMICS_HASH_KEY"
```

All config values can be overridden with `TOKENOMICS_` prefixed env vars (e.g., `TOKENOMICS_SERVER_HTTPS_PORT=9443`).

---

## Token Management

```bash
# Create
./bin/tokenomics token create --policy '{"base_key_env":"OPENAI_API_KEY"}'

# List (shows hashes, policies, creation dates)
./bin/tokenomics token list

# Delete by hash
./bin/tokenomics token delete --hash <hash>
```

---

## Stats Endpoint

`GET /stats` returns:

```json
{
  "totals": {
    "request_count": 42,
    "input_tokens": 15000,
    "output_tokens": 8000,
    "total_tokens": 23000,
    "error_count": 2
  },
  "by_model_and_key": [
    {
      "model": "gpt-4o",
      "base_key_env": "OPENAI_API_KEY",
      "request_count": 40,
      "input_tokens": 14000,
      "output_tokens": 7500,
      "total_tokens": 21500,
      "error_count": 1
    }
  ],
  "by_token": [
    {
      "token_hash": "a1b2c3d4e5f6...",
      "request_count": 20,
      "input_tokens": 7000,
      "output_tokens": 3500,
      "total_tokens": 10500,
      "error_count": 0,
      "last_model": "gpt-4o",
      "base_key_env": "OPENAI_API_KEY",
      "first_seen": "2026-02-24T10:00:00Z",
      "last_seen": "2026-02-24T11:30:00Z"
    }
  ]
}
```

---

## TLS / Certificates

On first run with `tls.auto_gen: true`, the proxy generates:

- `certs/ca.crt` + `certs/ca.key` — Root CA
- `certs/server.crt` + `certs/server.key` — Server cert signed by the CA

To trust the proxy locally:

```bash
# macOS
sudo security add-trusted-cert -d -r trustRoot -k /Library/Keychains/System.keychain certs/ca.crt

# Linux (Debian/Ubuntu)
sudo cp certs/ca.crt /usr/local/share/ca-certificates/tokenomics-ca.crt
sudo update-ca-certificates

# Or skip verification
export NODE_TLS_REJECT_UNAUTHORIZED=0
```

---

## License

MIT
