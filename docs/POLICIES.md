# Policies

Every wrapper token in Tokenomics is bound to a policy. The policy defines what the token is allowed to do: which models it can access, how many tokens it can consume, what content is blocked, and what system prompts are injected.

A single token can work with **multiple providers**. Each provider can have its own set of policies scoped by model. Global settings apply to all requests, while provider-specific settings override them when the requested model matches.

## Policy JSON Schema

A policy is a JSON object passed to `token create --policy`:

```json
{
  "base_key_env": "OPENAI_API_KEY",
  "upstream_url": "https://api.openai.com",
  "max_tokens": 100000,
  "model_regex": "^gpt-4.*",
  "timeout": 60,
  "prompts": [
    { "role": "system", "content": "You are a helpful assistant." }
  ],
  "rules": [
    {"type": "regex", "pattern": "(?i)ignore.*instructions", "action": "fail"},
    {"type": "pii", "detect": ["ssn", "credit_card"], "action": "mask"},
    {"type": "keyword", "keywords": ["jailbreak"], "action": "warn"}
  ],
  "providers": {
    "openai": [
      {
        "base_key_env": "OPENAI_API_KEY",
        "model": "gpt-4o",
        "max_tokens": 50000,
        "timeout": 120
      },
      {
        "base_key_env": "OPENAI_API_KEY",
        "model_regex": "^gpt-3\\.5",
        "max_tokens": 200000
      }
    ],
    "anthropic": [
      {
        "base_key_env": "ANTHROPIC_API_KEY",
        "upstream_url": "https://api.anthropic.com",
        "model_regex": "^claude",
        "prompts": [{ "role": "system", "content": "Be concise." }]
      }
    ]
  },
  "rate_limit": {
    "rules": [
      { "requests": 60, "window": "1m" },
      { "tokens": 100000, "window": "1h", "strategy": "fixed" }
    ],
    "max_parallel": 5
  },
  "retry": {
    "max_retries": 2,
    "fallbacks": ["gpt-4o-mini"],
    "retry_on": [429, 500, 502, 503]
  },
  "metadata": {
    "team": "engineering",
    "project": "chatbot"
  },
  "memory": {
    "enabled": true,
    "file_path": "/var/log/tokenomics/memory",
    "file_name": "{token_hash}.md"
  }
}
```

## Resolution Order

When a chat completion request arrives, Tokenomics resolves the effective policy:

1. **Start with global** fields (`base_key_env`, `upstream_url`, `max_tokens`, `model`, `model_regex`, `prompts`, `rules`, `timeout`, `rate_limit`, `retry`, `metadata`)
2. **Check rate limits** — if the token has exceeded request or token limits, reject with 429.
3. **Search providers** for a matching policy. Each provider has an array of policies; each policy specifies a `model` (exact) or `model_regex` (pattern). The first match wins.
4. **Merge** the matching provider policy on top of the global fields:
   - `base_key_env`, `upstream_url`, `max_tokens`, `model`, `model_regex`, `timeout` — provider overrides global
   - `prompts` — provider prompts **prepend** before global prompts
   - `rules` — provider rules **append** after global rules
   - `rate_limit`, `retry`, `metadata` — inherited from global (not overridden per provider)
5. **Send to upstream** with the configured `timeout`. On failure, **retry** according to `retry` config, trying fallback models if configured.

If no provider policy matches the requested model, the global policy is used as-is.

## Global Fields

### `base_key_env` (required if no providers)

The name of the environment variable holding the real API key.

```json
{ "base_key_env": "OPENAI_API_KEY" }
```

Either this or at least one provider with `base_key_env` must be set.

### `upstream_url` (optional)

Override the global upstream URL from config for this token.

```json
{ "upstream_url": "https://api.anthropic.com" }
```

### `max_tokens` (optional)

Total token budget for the session. Set to `0` or omit for unlimited.

```json
{ "max_tokens": 50000 }
```

### `model` / `model_regex` (optional)

Restrict allowed models globally. Both `model` (exact) and `model_regex` (Go regex) are checked.

### `prompts` (optional)

Messages prepended to every chat completion request.

```json
{
  "prompts": [
    { "role": "system", "content": "You are a helpful assistant." }
  ]
}
```

### `rules` (optional)

Content inspection rules that can block, warn, log, or mask matching content. Rules are objects with a type, action, and scope.

**Rule types:**

| Type | Description | Required Field |
|------|-------------|----------------|
| `regex` | Match a Go regular expression | `pattern` |
| `keyword` | Match case-insensitive keywords with word boundaries | `keywords` (array) |
| `pii` | Detect personally identifiable information | `detect` (array of PII types) |

**Actions:**

| Action | Behavior |
|--------|----------|
| `fail` | Block the request with 403 Forbidden (default) |
| `warn` | Allow the request but log a warning |
| `log` | Silently record the match in structured logs |
| `mask` | Redact matched content with `[REDACTED]` before forwarding |

**Scope:** `input` (user messages, default), `output` (response content), or `both`.

**Built-in PII types:** `ssn`, `credit_card`, `email`, `phone`, `ip_address`, `aws_key`, `api_key`, `jwt`, `private_key`, `connection_string`, `github_token`.

```json
{
  "rules": [
    {"type": "regex", "pattern": "(?i)ignore.*instructions", "action": "fail"},
    {"type": "pii", "detect": ["ssn", "credit_card", "email"], "action": "mask", "scope": "both"},
    {"type": "keyword", "keywords": ["jailbreak", "bypass"], "action": "warn", "name": "prompt-injection"}
  ]
}
```

Rules support an optional `name` field for identifying matches in logs. All rule matches (violations, warnings, and logs) are recorded in structured request logs.

**Backward compatible:** The old string-array format `["regex1", "regex2"]` still works and auto-converts each entry to a regex rule with `fail` action.

## Providers

The `providers` field is a map of provider names to **arrays** of provider policies. Each policy targets specific models.

```json
{
  "providers": {
    "openai": [
      { "base_key_env": "OPENAI_KEY", "model": "gpt-4o" },
      { "base_key_env": "OPENAI_KEY", "model_regex": "^gpt-3" }
    ]
  }
}
```

### Provider Policy Fields

Each provider policy supports the same fields as the global policy:

| Field | Override Behavior |
|-------|-------------------|
| `base_key_env` | Replaces global |
| `upstream_url` | Replaces global |
| `max_tokens` | Replaces global |
| `model` | Replaces global |
| `model_regex` | Replaces global |
| `timeout` | Replaces global |
| `prompts` | Prepends before global prompts |
| `rules` | Appends after global rules |

### Model Matching

A provider policy matches when:
- `model` is set and equals the requested model exactly, OR
- `model_regex` is set and the requested model matches the pattern, OR
- Neither `model` nor `model_regex` is set (catches all models)

### Provider Configuration File

Provider connection details (upstream URLs, authentication, headers) are defined in a separate `providers.yaml` file. Policies reference providers by name — the proxy automatically resolves auth scheme, custom headers, and chat path from the provider config.

```yaml
# providers.yaml
providers:
  openai:
    upstream_url: https://api.openai.com
    api_key_env: OPENAI_API_KEY
    models:
      - gpt-4o
      - gpt-4o-mini

  anthropic:
    upstream_url: https://api.anthropic.com
    api_key_env: ANTHROPIC_API_KEY
    auth_scheme: header
    auth_header: x-api-key
    headers:
      anthropic-version: "2023-06-01"
    chat_path: /v1/messages
    models:
      - claude-3-opus
```

When a policy references `"anthropic"` as a provider name, the proxy:
1. Routes to `https://api.anthropic.com`
2. Sends the API key via `x-api-key` header (not `Authorization: Bearer`)
3. Adds the `anthropic-version` header to every request
4. Uses `/v1/messages` instead of `/v1/chat/completions`

#### Provider Config Fields

| Field | Description |
|-------|-------------|
| `upstream_url` | Base URL for the provider's API |
| `api_key_env` | Environment variable holding the API key |
| `auth_scheme` | How to send the key: `"bearer"` (default), `"header"` (raw), `"query"` (?key=) |
| `auth_header` | Custom header name (default: `"Authorization"`) |
| `headers` | Extra headers sent with every request |
| `models` | Known model prefixes (informational) |
| `chat_path` | Override path for chat completions endpoint |

#### Client Authentication Formats

The Tokenomics proxy accepts wrapper tokens from clients in multiple formats to support different SDKs and CLI tools:

- **`x-api-key: {token}`** — Anthropic SDK, Google Gemini SDK, Azure style
- **`Authorization: Bearer {token}`** — OpenAI SDK, curl, generic clients
- **`Authorization: {token}`** — Raw token format for backward compatibility

All formats are equivalent. Use whichever matches your client library. Environment variables (e.g., `ANTHROPIC_API_KEY`) are automatically converted to the appropriate header format by the SDK.

#### Upstream Auth Schemes

The following schemes control how the proxy authenticates with upstream providers (these are configured in `providers.yaml` or policy, not client auth):

- **`bearer`** (default): Sends `Authorization: Bearer <key>`
- **`header`**: Sends `<auth_header>: <key>` (e.g., `x-api-key: sk-ant-...`)
- **`query`**: Appends `?key=<key>` to the URL (e.g., Google Gemini)

#### Resolution Priority

Connection details are resolved in this order (first wins):

1. **Policy** — `upstream_url` or `base_key_env` set directly in the policy/provider policy
2. **Provider config** — `upstream_url` or `api_key_env` from `providers.yaml`
3. **Global config** — `server.upstream_url` from `config.yaml`

The `providers.yaml` ships with 16 pre-configured providers including OpenAI, Anthropic, Azure OpenAI, Google Gemini, Vertex AI, Mistral, Cohere, Groq, Together, Fireworks, Perplexity, DeepSeek, xAI, OpenRouter, Ollama, vLLM, and LiteLLM.

## Rate Limiting

The `rate_limit` field controls request and token throughput per wrapper token. Supports multiple rules with different time windows and strategies.

```json
{
  "rate_limit": {
    "rules": [
      { "requests": 60, "window": "1m" },
      { "requests": 1000, "window": "1h", "strategy": "fixed" },
      { "tokens": 100000, "window": "1h" }
    ],
    "max_parallel": 5
  }
}
```

### Rate Limit Rules

Each rule defines limits within a time window:

| Field | Description |
|-------|-------------|
| `requests` | Max requests per window (0 = unlimited) |
| `tokens` | Max tokens per window (0 = unlimited) |
| `window` | Duration: `"1s"`, `"1m"` (default), `"1h"`, `"24h"`, or any Go duration |
| `strategy` | `"sliding"` (default) or `"fixed"` |

**Sliding window** tracks individual request timestamps and counts within a rolling window. **Fixed window** resets the counter at the start of each window period.

Multiple rules are evaluated independently — the first rule that is exceeded blocks the request.

### Max Parallel

`max_parallel` limits concurrent in-flight requests per token. Useful for preventing abuse from parallel request flooding.

## Retry and Fallback

The `retry` field configures automatic retry on upstream failures and model fallback chains.

```json
{
  "retry": {
    "max_retries": 3,
    "fallbacks": ["gpt-4o-mini", "gpt-3.5-turbo"],
    "retry_on": [429, 500, 502, 503]
  }
}
```

| Field | Description |
|-------|-------------|
| `max_retries` | Max retry attempts per model (default 0 = no retries) |
| `fallbacks` | Ordered list of fallback model names to try after the primary model exhausts retries |
| `retry_on` | HTTP status codes that trigger retry (default: `[429, 500, 502, 503]`) |

The proxy first retries the primary model up to `max_retries` times. If all retries fail, it moves to the next model in `fallbacks` and retries again. The process continues until a successful response or all models are exhausted.

## Timeout

The `timeout` field sets the per-request upstream timeout in seconds. Default is 30 seconds if not specified.

```json
{ "timeout": 120 }
```

Provider policies can override the global timeout:

```json
{
  "timeout": 30,
  "providers": {
    "openai": [{
      "base_key_env": "OPENAI_KEY",
      "model_regex": "^o1",
      "timeout": 300
    }]
  }
}
```

## Metadata

The `metadata` field attaches key-value tags to every request for analytics and cost attribution. Tags are included in structured JSON logs.

```json
{
  "metadata": {
    "team": "engineering",
    "project": "chatbot",
    "env": "production",
    "cost_center": "CC-1234"
  }
}
```

Tags can be used downstream for filtering logs, computing per-team costs, or routing to analytics pipelines.

## Upstream Request Tracking

Tokenomics automatically tracks upstream provider request and response IDs for debugging and log correlation. This requires no configuration — it works out of the box.

### What gets captured

Every proxied request logs three tracking identifiers:

| Log Field | Description |
|---|---|
| `client_request_id` | A unique `tkn_<hex>` ID generated by Tokenomics and sent upstream via `X-Client-Request-Id` header |
| `upstream_request_id` | The provider's server-generated request ID extracted from response headers |
| `upstream_id` | The provider's completion/message ID from the response body (e.g., `chatcmpl-...`, `msg_...`) |

### Provider header mapping

| Provider | Response Header | Body ID Field | Client ID Supported |
|---|---|---|---|
| OpenAI | `x-request-id` | `id` (`chatcmpl-...`) | Yes (`X-Client-Request-Id`) |
| Anthropic | `request-id` | `id` (`msg_...`) | No |
| Azure OpenAI | `x-request-id`, `apim-request-id` | `id` (`chatcmpl-...`) | Yes (`x-ms-client-request-id`) |
| Google Gemini | — | `responseId` | No |
| Mistral | `Mistral-Correlation-Id` | `id` (`cmpl-...`) | No |
| Cohere | — | `id` | No |

### Example log entry

```json
{
  "timestamp": "2025-01-15T10:30:00.000Z",
  "model": "gpt-4o",
  "status_code": 200,
  "client_request_id": "tkn_a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4",
  "upstream_request_id": "req_abc123def456",
  "upstream_id": "chatcmpl-B9MHDbslfkBeAs8l4bebGdFOJ6PeG"
}
```

Use `client_request_id` to correlate Tokenomics logs with the provider's logs when debugging issues or contacting provider support.

## Memory (Session Logging)

The `memory` field enables conversation logging for the token.

### Per-session files (recommended)

Write each session to its own file using `file_path` (directory) and `file_name` (pattern):

```json
{
  "memory": {
    "enabled": true,
    "file_path": "/var/log/tokenomics/memory",
    "file_name": "{token_hash}.md"
  }
}
```

This creates one file per token, e.g. `/var/log/tokenomics/memory/a1b2c3d4e5f6a1b2.md`.

| Placeholder | Replaced with |
|-------------|---------------|
| `{token_hash}` | First 16 characters of the token's HMAC hash |
| `{date}` | Current UTC date as `YYYY-MM-DD` |

Patterns can include subdirectories. For example, `{date}/{token_hash}.md` creates daily directories with per-token files.

### Single-file memory (legacy)

Append all sessions to one file by setting `file_path` without `file_name`:

```json
{
  "memory": {
    "enabled": true,
    "file_path": "/var/log/tokenomics/sessions.md"
  }
}
```

### Entry format

Each entry is formatted as:

```
## <timestamp> | <token_hash_prefix> | <role> | <model>

<content>

---
```

See `examples/memory-sample.md` for a full sample output.

### Redis-based memory

Push entries to a Redis list keyed by session:

```json
{
  "memory": {
    "enabled": true,
    "redis": true
  }
}
```

Entries are pushed to `tokenomics:memory:<session_id>` using RPUSH.

## Examples

### Single provider, minimal

```json
{ "base_key_env": "OPENAI_API_KEY" }
```

### Multi-provider with model routing

One token works with both OpenAI and Anthropic. The proxy routes based on model name:

```json
{
  "base_key_env": "OPENAI_API_KEY",
  "max_tokens": 100000,
  "prompts": [{ "role": "system", "content": "Be helpful." }],
  "providers": {
    "openai": [
      {
        "base_key_env": "OPENAI_API_KEY",
        "model_regex": "^gpt-4",
        "max_tokens": 50000
      },
      {
        "base_key_env": "OPENAI_API_KEY",
        "model_regex": "^gpt-3",
        "max_tokens": 200000
      }
    ],
    "anthropic": [
      {
        "base_key_env": "ANTHROPIC_API_KEY",
        "upstream_url": "https://api.anthropic.com",
        "model_regex": "^claude"
      }
    ]
  }
}
```

When a request comes in for `claude-3-opus`, the proxy matches the Anthropic provider policy, uses `ANTHROPIC_API_KEY`, routes to `https://api.anthropic.com`, and prepends the global "Be helpful." prompt.

### Locked-down policy with content rules and memory

```json
{
  "base_key_env": "OPENAI_API_KEY",
  "model": "gpt-4o-mini",
  "max_tokens": 10000,
  "prompts": [
    { "role": "system", "content": "Answer only questions about our product." }
  ],
  "rules": [
    {"type": "regex", "pattern": "(?i)ignore.*instructions", "action": "fail"},
    {"type": "regex", "pattern": "(?i)system.*prompt", "action": "fail"},
    {"type": "keyword", "keywords": ["jailbreak"], "action": "fail"},
    {"type": "pii", "detect": ["ssn", "credit_card", "email"], "action": "mask", "scope": "both"}
  ],
  "memory": {
    "enabled": true,
    "file_path": "/var/log/tokenomics/audit",
    "file_name": "{token_hash}.md"
  }
}
```

### Per-model budget limits

Different token budgets for expensive vs cheap models:

```json
{
  "providers": {
    "openai": [
      {
        "base_key_env": "OPENAI_API_KEY",
        "model": "gpt-4o",
        "max_tokens": 10000
      },
      {
        "base_key_env": "OPENAI_API_KEY",
        "model_regex": "^gpt-4o-mini",
        "max_tokens": 500000
      }
    ]
  }
}
```

### Rate limited with retry and fallback

```json
{
  "base_key_env": "OPENAI_API_KEY",
  "timeout": 60,
  "rate_limit": {
    "rules": [
      { "requests": 10, "window": "1m" },
      { "tokens": 50000, "window": "1h" }
    ],
    "max_parallel": 3
  },
  "retry": {
    "max_retries": 2,
    "fallbacks": ["gpt-4o-mini"]
  },
  "metadata": {
    "team": "support",
    "env": "production"
  }
}
```

If a request to `gpt-4o` fails with a 429 or 5xx, the proxy retries up to 2 times, then falls back to `gpt-4o-mini`. Rate limits enforce a maximum of 10 requests per minute and 50k tokens per hour. No more than 3 requests can be in flight at once.

### Full production policy

```json
{
  "base_key_env": "OPENAI_API_KEY",
  "max_tokens": 500000,
  "timeout": 30,
  "prompts": [{ "role": "system", "content": "You are a helpful customer support agent." }],
  "rules": [
    {"type": "regex", "pattern": "(?i)ignore.*instructions", "action": "fail"},
    {"type": "keyword", "keywords": ["jailbreak"], "action": "fail"},
    {"type": "pii", "detect": ["ssn", "credit_card"], "action": "mask"}
  ],
  "rate_limit": {
    "rules": [
      { "requests": 30, "window": "1m" },
      { "requests": 500, "window": "24h", "strategy": "fixed" },
      { "tokens": 100000, "window": "1h" }
    ],
    "max_parallel": 5
  },
  "retry": {
    "max_retries": 2,
    "fallbacks": ["gpt-4o-mini"],
    "retry_on": [429, 500, 502, 503]
  },
  "metadata": {
    "team": "support",
    "project": "helpdesk",
    "env": "production"
  },
  "memory": {
    "enabled": true,
    "file_path": "/var/log/tokenomics/support",
    "file_name": "{date}/{token_hash}.md"
  },
  "providers": {
    "openai": [
      {
        "base_key_env": "OPENAI_API_KEY",
        "model": "gpt-4o",
        "timeout": 120,
        "max_tokens": 100000
      },
      {
        "base_key_env": "OPENAI_API_KEY",
        "model_regex": "^gpt-4o-mini",
        "max_tokens": 500000
      }
    ]
  }
}
```
