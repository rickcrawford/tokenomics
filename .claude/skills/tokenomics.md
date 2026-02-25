# Tokenomics Operator

Manage tokens, policies, and the proxy server for the Tokenomics reverse proxy.

## When to Use

Use this skill when the user wants to:
- Create, list, inspect, update, or delete wrapper tokens
- Build policy JSON interactively (budgets, models, rules, PII, prompts)
- Start or configure the proxy server
- Generate agent init commands for OpenAI/Anthropic/Azure/Gemini SDKs
- Set up webhooks or provider configs
- Configure logging or remote token sync
- Start the remote config server

## Environment Check

Before any operation, verify:
1. Binary exists at `./bin/tokenomics` (run `make build` if not)
2. `TOKENOMICS_HASH_KEY` env var is set (warn if using default)
3. For serve: check if `config.yaml` exists

## Commands Reference

### Token CRUD

```bash
# Create (policy JSON required, expires optional)
./bin/tokenomics token create --policy '<json>' --expires 30d

# List all
./bin/tokenomics token list

# Inspect
./bin/tokenomics token get --hash <hash>

# Update policy and/or expiration
./bin/tokenomics token update --hash <hash> --policy '<json>' --expires 7d

# Delete
./bin/tokenomics token delete --hash <hash>
```

Expiration formats: `24h`, `7d`, `30d`, `1y`, RFC3339, or `clear`.

### Server

```bash
./bin/tokenomics serve                    # uses config.yaml
./bin/tokenomics serve --config path.yaml # custom config
```

Default ports: 8080 (HTTP), 8443 (HTTPS with auto-generated TLS).

### Remote Config Server

```bash
# Start a central config server (other proxies sync from this)
./bin/tokenomics remote --addr :9090 --api-key my-secret

# Configure a proxy to sync from it (in config.yaml)
# remote:
#   url: http://config-server:9090
#   api_key: my-secret
#   sync: 60       # re-sync every 60 seconds
```

### Agent Init

```bash
# OpenAI (default)
eval $(./bin/tokenomics init --token tkn_xxx --port 8443 --insecure)

# Anthropic
eval $(./bin/tokenomics init --token tkn_xxx --cli anthropic --port 8443 --insecure)

# Azure
eval $(./bin/tokenomics init --token tkn_xxx --cli azure --port 8443 --insecure)

# Write to .env file
./bin/tokenomics init --token tkn_xxx --output dotenv --dotenv .env
```

## Policy Schema

When helping users build policies, use this structure:

```json
{
  "base_key_env": "OPENAI_API_KEY",
  "max_tokens": 100000,
  "model": "gpt-4o",
  "model_regex": "^gpt-4.*",
  "prompts": [
    {"role": "system", "content": "You are a helpful assistant."}
  ],
  "rules": [
    {"type": "regex", "pattern": "(?i)drop\\s+table", "action": "fail"},
    {"type": "keyword", "keywords": ["jailbreak", "bypass"], "action": "warn"},
    {"type": "pii", "detect": ["ssn", "credit_card", "email"], "action": "mask", "scope": "both"}
  ],
  "rate_limit": {
    "rules": [
      {"requests": 10, "window": "1m"},
      {"tokens": 50000, "window": "1h"}
    ],
    "max_parallel": 3
  },
  "retry": {
    "max_retries": 2,
    "fallbacks": ["gpt-4o-mini"],
    "retry_on": [429, 500, 502, 503]
  },
  "timeout": 30,
  "metadata": {"team": "engineering", "env": "staging"}
}
```

### Multi-Provider Policy

```json
{
  "max_tokens": 500000,
  "providers": {
    "openai": [{"base_key_env": "OPENAI_API_KEY", "model_regex": "^gpt"}],
    "anthropic": [{"base_key_env": "ANTHROPIC_API_KEY", "model_regex": "^claude"}],
    "google": [{"base_key_env": "GEMINI_API_KEY", "model_regex": "^gemini"}]
  },
  "rules": [
    {"type": "pii", "detect": ["ssn", "credit_card"], "action": "mask"}
  ]
}
```

### Rule Types

| Type | Required fields | Example |
|------|----------------|---------|
| `regex` | `pattern` (Go regex) | `{"type":"regex","pattern":"(?i)secret","action":"fail"}` |
| `keyword` | `keywords` (string array) | `{"type":"keyword","keywords":["bomb"],"action":"warn"}` |
| `pii` | `detect` (type array) | `{"type":"pii","detect":["ssn","email"],"action":"mask"}` |

Actions: `fail` (block 403), `warn` (allow + log), `log` (silent), `mask` (redact with [REDACTED])
Scopes: `input` (default), `output`, `both`
PII types: `ssn`, `credit_card`, `email`, `phone`, `ip_address`, `aws_key`, `api_key`, `jwt`, `private_key`, `connection_string`, `github_token`

### Policy Templates

Point users to examples for starting points:
- `examples/policies/minimal.json` - bare minimum
- `examples/policies/budget-limited.json` - budgets + rate limits
- `examples/policies/pii-protection.json` - PII detection and masking
- `examples/policies/prompt-injection-guard.json` - content safety
- `examples/policies/multi-provider-routing.json` - multi-provider with retry
- `examples/policies/intern-sandbox.json` - locked-down sandbox

## Interactive Token Creation

When the user says "create a token" without providing a full policy, ask about:
1. Provider(s) and API key env var(s)
2. Model restrictions (exact or regex)
3. Token budget (max_tokens)
4. Rate limits (requests/min, tokens/hour, parallel)
5. Content rules (PII masking, blocked patterns, keywords)
6. System prompts to inject
7. Expiration duration

Then assemble the policy JSON, show it for confirmation, and run the create command.

## Supported Providers

16 pre-configured: OpenAI, Anthropic, Azure OpenAI, Google Gemini, Vertex AI, Mistral, Cohere, Groq, Together AI, Fireworks AI, Perplexity, DeepSeek, xAI (Grok), OpenRouter, Ollama, vLLM.

Provider YAML examples in `examples/providers/`.

## Logging Configuration

```yaml
logging:
  level: info              # debug, info, warn, error
  format: json             # json or text
  request_body: false      # log full request bodies
  response_body: false     # log full response bodies
  hide_token_hash: false   # mask hashes in logs with ****
  disable_request: false   # suppress per-request structured logs
```

## Remote Token Sync

```yaml
remote:
  url: http://config-server:9090   # central server URL
  api_key: my-secret               # shared API key
  sync: 60                         # re-sync every N seconds (0 = startup only)
  insecure: false                  # skip TLS verification
```

The remote server runs via `./bin/tokenomics remote --addr :9090 --api-key <key>`.
It serves `GET /api/v1/tokens` and `GET /api/v1/tokens/<hash>` with Bearer auth.
Sync is additive. Local-only tokens are preserved.
