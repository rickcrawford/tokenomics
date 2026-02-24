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
    "(?i)ignore.*instructions"
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
    "file_path": "/var/log/tokenomics/sessions.md"
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

Regex patterns that block matching user message content.

```json
{ "rules": ["(?i)ignore.*instructions"] }
```

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

## Memory (Session Logging)

The `memory` field enables conversation logging for the token.

### File-based memory

Append markdown-formatted entries to a file during the session:

```json
{
  "memory": {
    "enabled": true,
    "file_path": "/var/log/tokenomics/sessions.md"
  }
}
```

Each entry is formatted as:

```
## <timestamp> | <session_id> | <role> | <model>

<content>

---
```

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
    "(?i)ignore.*instructions",
    "(?i)system.*prompt",
    "(?i)jailbreak"
  ],
  "memory": {
    "enabled": true,
    "file_path": "/var/log/tokenomics/audit.md"
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
  "rules": ["(?i)ignore.*instructions", "(?i)jailbreak"],
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
    "file_path": "/var/log/tokenomics/support.md"
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
