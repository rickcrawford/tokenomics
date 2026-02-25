# Examples

Everything you need to go from `git clone` to proxying requests in under 5 minutes.

## Quick Start

```bash
# 1. Copy example configs
cp examples/config.yaml config.yaml
cp examples/.env.example .env

# 2. Edit .env with your real API keys
vim .env

# 3. Source the env (or use direnv, dotenv, etc.)
export $(grep -v '^#' .env | xargs)

# 4. Build and run
make build
./bin/tokenomics serve
```

## What's Inside

```
examples/
├── config.yaml                  # Full annotated config with webhook setup
├── .env.example                 # Every env var Tokenomics might need
├── webhook-collector/           # Sample server to receive and debug events
│   └── main.go
├── providers/                   # Per-provider YAML configs
│   ├── openai.yaml
│   ├── anthropic.yaml
│   ├── azure-openai.yaml
│   ├── google-gemini.yaml
│   ├── groq.yaml
│   ├── mistral.yaml
│   ├── deepseek.yaml
│   ├── ollama.yaml
│   └── multi-provider.yaml      # All providers in one file
└── policies/                    # Sample policy JSON files
    ├── minimal.json             # Simplest possible policy
    ├── budget-limited.json      # Token budgets and rate limits
    ├── pii-protection.json      # PII detection and masking
    ├── prompt-injection-guard.json  # Content rules for prompt safety
    ├── multi-provider-routing.json  # Multi-provider with retry and fallback
    └── intern-sandbox.json      # Locked-down sandbox for untrusted users
```

## Webhook Collector

A debugging tool that receives Tokenomics events, verifies signatures, and logs them to stdout with color-coded icons.

### Run it

```bash
# No auth (development)
go run examples/webhook-collector/main.go

# With shared secret
go run examples/webhook-collector/main.go -secret my-webhook-secret

# With HMAC signature verification
go run examples/webhook-collector/main.go -signing-key my-signing-key

# Both, custom port
go run examples/webhook-collector/main.go -addr :7777 -secret my-webhook-secret -signing-key my-signing-key
```

### Configure Tokenomics to send events

In your `config.yaml`:

```yaml
events:
  webhooks:
    - url: http://localhost:9090/webhook
      secret: my-webhook-secret
      signing_key: my-signing-key
```

### Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/webhook` | POST | Receives events |
| `/stats` | GET | JSON event counts by type |
| `/health` | GET | Returns `ok` |

### Sample output

```
╔══════════════════════════════════════════════════════╗
║       Tokenomics Webhook Collector                  ║
╚══════════════════════════════════════════════════════╝
  Listen:       :9090
  Secret:       my-***
  Signing Key:  my-***
  Endpoints:    POST /webhook  GET /stats  GET /health

Listening on :9090 ...

─── 14:32:01.234 [S] server.start ───────────────────────────
  ID:   evt_a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4
  Time: 2025-01-15T14:32:01.234Z
  Data:
  {
    "http_port": 8080,
    "https_port": 8443,
    "tls": true,
    "upstream": "https://api.openai.com"
  }

─── 14:32:05.678 [+] token.created ───────────────────────────
  ID:   evt_b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5
  Time: 2025-01-15T14:32:05.678Z
  Data:
  {
    "token_hash": "abc12345",
    "expires_at": "2025-02-15T14:32:05Z"
  }

─── 14:32:10.456 [X] rule.violation ───────────────────────────
  ID:   evt_c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6
  Time: 2025-01-15T14:32:10.456Z
  Data:
  {
    "model": "gpt-4o",
    "rule_name": "prompt-injection",
    "token_hash": "abc12345def67890"
  }
```

## Provider Configs

Each file in `providers/` is a standalone `providers.yaml` you can copy to your project root. Use one provider or combine several.

### Single provider

```bash
# Use just OpenAI
cp examples/providers/openai.yaml providers.yaml
```

### Multiple providers

```bash
# Use the multi-provider example (OpenAI + Anthropic + Groq + Mistral + DeepSeek)
cp examples/providers/multi-provider.yaml providers.yaml
```

### Provider auth schemes

| Provider | Auth | Notes |
|----------|------|-------|
| OpenAI | Bearer (default) | `Authorization: Bearer sk-...` |
| Anthropic | Header | `x-api-key: sk-ant-...` + `anthropic-version` header |
| Azure OpenAI | Header | `api-key: ...` + `api-version` header |
| Google Gemini | Query | `?key=AIza...` appended to URL |
| Groq | Bearer | Same as OpenAI |
| Mistral | Bearer | Same as OpenAI |
| DeepSeek | Bearer | Same as OpenAI |
| Ollama | None | Local, no auth needed |

## Policies

Copy a policy JSON and use it with `token create`:

```bash
# Simple: just an API key
./bin/tokenomics token create --policy "$(cat examples/policies/minimal.json)"

# Budget-limited with rate limits
./bin/tokenomics token create --policy "$(cat examples/policies/budget-limited.json)" --expires 30d

# PII protection
./bin/tokenomics token create --policy "$(cat examples/policies/pii-protection.json)"

# Prompt injection guard
./bin/tokenomics token create --policy "$(cat examples/policies/prompt-injection-guard.json)"

# Multi-provider routing with retry
./bin/tokenomics token create --policy "$(cat examples/policies/multi-provider-routing.json)" --expires 1y

# Locked-down intern sandbox with 10k token budget
./bin/tokenomics token create --policy "$(cat examples/policies/intern-sandbox.json)" --expires 7d
```

### Policy comparison

| Policy | Models | Budget | Rate Limit | Rules | Retry |
|--------|--------|--------|------------|-------|-------|
| `minimal` | Any | None | None | None | No |
| `budget-limited` | gpt-4o* | 100k | 30/min, 50k tok/hr | None | No |
| `pii-protection` | gpt-4o | 200k | None | PII mask (6 types) | No |
| `prompt-injection-guard` | gpt-* | None | None | 6 rules (fail+warn) | No |
| `multi-provider-routing` | All | 500k | 60/min, 200k tok/hr | PII mask | 2 retries + fallback |
| `intern-sandbox` | gpt-4o-mini only | 10k | 10/min, 100/day | 3 rules + PII mask | No |

## Environment Variables

The `.env.example` file lists every variable Tokenomics can use. The two required ones:

| Variable | Purpose |
|----------|---------|
| `TOKENOMICS_HASH_KEY` | HMAC key for hashing tokens (required) |
| `TOKENOMICS_ENCRYPTION_KEY` | AES-256-GCM key for at-rest encryption (optional) |

All provider API keys (`OPENAI_API_KEY`, `ANTHROPIC_API_KEY`, etc.) are read at request time based on the `base_key_env` in your policy or `api_key_env` in your providers config.

## End-to-End Walkthrough

```bash
# Terminal 1: Start the webhook collector
go run examples/webhook-collector/main.go -secret my-webhook-secret -signing-key my-signing-key

# Terminal 2: Start Tokenomics with the example config
cp examples/config.yaml config.yaml
cp examples/providers/openai.yaml providers.yaml
export TOKENOMICS_HASH_KEY=my-secret-hash-key
export OPENAI_API_KEY=sk-your-real-key
make build && ./bin/tokenomics serve

# Terminal 3: Create a token and make a request
export TOKENOMICS_HASH_KEY=my-secret-hash-key
./bin/tokenomics token create --policy "$(cat examples/policies/budget-limited.json)" --expires 7d

# Use the printed token
eval $(./bin/tokenomics init --token tkn_<your-token> --port 8443 --insecure)

# Make a request through the proxy
curl -s $OPENAI_BASE_URL/v1/chat/completions \
  -H "Authorization: Bearer $OPENAI_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4o-mini",
    "messages": [{"role": "user", "content": "Hello!"}]
  }'
```

Watch Terminal 1 -- you'll see `server.start`, `token.created`, `budget.update`, and `request.completed` events flow in.
