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
        "max_tokens": 50000
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
  "memory": {
    "enabled": true,
    "file_path": "/var/log/tokenomics/sessions.md"
  }
}
```

## Resolution Order

When a chat completion request arrives, Tokenomics resolves the effective policy:

1. **Start with global** fields (`base_key_env`, `upstream_url`, `max_tokens`, `model`, `model_regex`, `prompts`, `rules`)
2. **Search providers** for a matching policy. Each provider has an array of policies; each policy specifies a `model` (exact) or `model_regex` (pattern). The first match wins.
3. **Merge** the matching provider policy on top of the global fields:
   - `base_key_env`, `upstream_url`, `max_tokens`, `model`, `model_regex` — provider overrides global
   - `prompts` — provider prompts **prepend** before global prompts
   - `rules` — provider rules **append** after global rules

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
| `prompts` | Prepends before global prompts |
| `rules` | Appends after global rules |

### Model Matching

A provider policy matches when:
- `model` is set and equals the requested model exactly, OR
- `model_regex` is set and the requested model matches the pattern, OR
- Neither `model` nor `model_regex` is set (catches all models)

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
