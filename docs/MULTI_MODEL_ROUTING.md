# Multi-Model Routing

Tokenomics routes requests to different providers based on the model name in each request. A single wrapper token can access OpenAI, Anthropic, Google Gemini, Groq, and any other provider. The proxy resolves which API key to use, which upstream to hit, and which auth scheme to apply, all from the policy and provider config.

## How It Works

```
Client request (model: "claude-3-opus")
  |
  v
Extract model from request body
  |
  v
Search policy providers for matching model
  |
  +-- openai policies:  "^gpt-"       --> no match
  +-- anthropic policies: "^claude"    --> match!
  |
  v
Merge anthropic provider policy onto global policy
  |
  v
Look up "anthropic" in providers.yaml for connection details
  |
  v
Route to https://api.anthropic.com/v1/messages
  with x-api-key header and anthropic-version header
```

## Configuration Layers

Routing depends on two configuration layers that work together.

### 1. Provider Config (providers.yaml)

Defines connection details for each provider: upstream URL, auth scheme, custom headers, and chat endpoint path. Built-in defaults include OpenAI, generic, Anthropic, Azure, Gemini, Groq, Mistral, DeepSeek, and Ollama.

```yaml
providers:
  openai:
    upstream_url: https://api.openai.com
    api_key_env: OPENAI_PAT

  anthropic:
    upstream_url: https://api.anthropic.com
    api_key_env: ANTHROPIC_PAT
    auth_scheme: header
    auth_header: x-api-key
    headers:
      anthropic-version: "2023-06-01"
    chat_path: /v1/messages

  groq:
    upstream_url: https://api.groq.com/openai
    api_key_env: GROQ_API_KEY
```

See [POLICIES.md](POLICIES.md#provider-config-fields) for the full list of provider config fields.

### 2. Policy (per token)

Defines which models are allowed, which provider handles each model, and per-model budgets/rules. The `providers` map in the policy links provider names to arrays of model-scoped policies.

```json
{
  "base_key_env": "OPENAI_PAT",
  "max_tokens": 100000,
  "prompts": [{"role": "system", "content": "Be helpful."}],
  "providers": {
    "openai": [
      {"base_key_env": "OPENAI_PAT", "model": "gpt-4o", "max_tokens": 50000},
      {"base_key_env": "OPENAI_PAT", "model_regex": "^gpt-3\\.5", "max_tokens": 200000}
    ],
    "anthropic": [
      {"base_key_env": "ANTHROPIC_PAT", "model_regex": "^claude"}
    ],
    "groq": [
      {"base_key_env": "GROQ_API_KEY", "model_regex": "^llama"}
    ]
  }
}
```

## Resolution Algorithm

When a chat completions request arrives, the proxy resolves the effective policy in this order:

| Step | Action |
|------|--------|
| 1 | Extract `model` from the request JSON body |
| 2 | Start with global policy fields (base_key_env, upstream_url, max_tokens, prompts, rules, timeout) |
| 3 | Iterate through each provider's policy array |
| 4 | For each provider policy, check if the model matches (exact, regex, or wildcard) |
| 5 | On first match, merge the provider policy onto the global fields |
| 6 | Look up the matched provider name in providers.yaml for connection details |
| 7 | Resolve upstream URL: policy > provider config > global config |
| 8 | Resolve API key from the environment variable named in `base_key_env` |
| 9 | Apply provider auth scheme, custom headers, and chat path |

If no provider policy matches, the global policy is used as-is.

## Model Matching

Each provider policy can specify a model constraint. The first matching policy wins.

| Constraint | Example | Matches |
|------------|---------|---------|
| `model` (exact) | `"model": "gpt-4o"` | Only `gpt-4o` |
| `model_regex` (pattern) | `"model_regex": "^gpt-4"` | `gpt-4o`, `gpt-4-turbo`, `gpt-4o-mini` |
| Neither (wildcard) | No model or model_regex set | Any model |

Matching stops at the first provider policy that matches. Order within a provider array matters: put more specific models first.

## Merge Behavior

When a provider policy matches, its fields are merged onto the global policy.

| Field | Merge rule |
|-------|-----------|
| `base_key_env` | Provider replaces global |
| `upstream_url` | Provider replaces global |
| `max_tokens` | Provider replaces global |
| `model` | Provider replaces global |
| `model_regex` | Provider replaces global |
| `timeout` | Provider replaces global |
| `prompts` | Provider prompts prepend before global prompts |
| `rules` | Provider rules append after global rules |
| `rate_limit` | Inherited from global (not per-provider) |
| `retry` | Inherited from global (not per-provider) |
| `metadata` | Inherited from global (not per-provider) |

## Upstream URL Priority

The upstream URL is resolved from three sources, first match wins:

1. `upstream_url` in the matched policy or provider policy
2. `upstream_url` from the provider config in providers.yaml
3. `server.upstream_url` from config.yaml (global default)

## Auth Schemes

The proxy authenticates with the upstream provider using the scheme from providers.yaml.

| Scheme | Header sent | Example |
|--------|------------|---------|
| `bearer` (default) | `Authorization: Bearer <key>` | OpenAI, Groq, Mistral |
| `header` | `<auth_header>: <key>` | Anthropic (`x-api-key`) |
| `query` | URL param `?key=<key>` | Google Gemini |

The proxy also adds any custom headers defined in the provider config (e.g., `anthropic-version`).

## Retry and Fallback

When a request fails with a retryable status code (default: 429, 500, 502, 503), the proxy retries the same model up to `max_retries` times, then moves to the next model in the `fallbacks` list.

```json
{
  "retry": {
    "max_retries": 2,
    "fallbacks": ["gpt-4o-mini"],
    "retry_on": [429, 500, 502, 503]
  }
}
```

Each fallback model goes through the same resolution process, so a fallback can route to a different provider if the policy maps it that way.

## CLI Integration

The `tokenomics run` command auto-detects providers from CLI tool names using `cli_maps` in config.yaml:

```yaml
cli_maps:
  claude: anthropic
  python: generic
  node: generic
```

```bash
tokenomics run claude "What is AI?"       # Uses anthropic provider
tokenomics run --provider groq -- python script.py  # Explicit provider
```

The `run` command starts the proxy, sets environment variables for the provider, executes the command, and cleans up.

## Examples

### Single provider

One API key, one provider. The simplest case.

```json
{"base_key_env": "OPENAI_PAT"}
```

All requests go to OpenAI using the global upstream URL.

### Two providers, model-based routing

```json
{
  "base_key_env": "OPENAI_PAT",
  "providers": {
    "openai": [
      {"base_key_env": "OPENAI_PAT", "model_regex": "^gpt-"}
    ],
    "anthropic": [
      {"base_key_env": "ANTHROPIC_PAT", "model_regex": "^claude"}
    ]
  }
}
```

Requests for `gpt-4o` go to OpenAI. Requests for `claude-3-opus` go to Anthropic. The proxy handles auth, headers, and endpoint paths automatically.

### Per-model budgets

```json
{
  "providers": {
    "openai": [
      {"base_key_env": "OPENAI_PAT", "model": "gpt-4o", "max_tokens": 10000},
      {"base_key_env": "OPENAI_PAT", "model_regex": "^gpt-4o-mini", "max_tokens": 500000}
    ]
  }
}
```

Expensive models get tight budgets. Cheap models get generous limits.

### Three providers with fallback

```json
{
  "base_key_env": "OPENAI_PAT",
  "retry": {
    "max_retries": 2,
    "fallbacks": ["gpt-4o-mini"]
  },
  "providers": {
    "openai": [
      {"base_key_env": "OPENAI_PAT", "model_regex": "^gpt-"}
    ],
    "anthropic": [
      {"base_key_env": "ANTHROPIC_PAT", "model_regex": "^claude"}
    ],
    "groq": [
      {"base_key_env": "GROQ_API_KEY", "model_regex": "^llama"}
    ]
  }
}
```

If the primary model fails, the proxy falls back to `gpt-4o-mini` via OpenAI.

### Provider-specific prompts and rules

```json
{
  "base_key_env": "OPENAI_PAT",
  "prompts": [{"role": "system", "content": "Be helpful."}],
  "rules": [{"type": "pii", "detect": ["ssn"], "action": "mask", "scope": "both"}],
  "providers": {
    "anthropic": [{
      "base_key_env": "ANTHROPIC_PAT",
      "model_regex": "^claude",
      "prompts": [{"role": "system", "content": "Be concise."}],
      "rules": [{"type": "keyword", "keywords": ["confidential"], "action": "fail"}]
    }]
  }
}
```

For Claude requests, the effective prompts are `["Be concise.", "Be helpful."]` (provider prepends). The effective rules are `[PII mask, keyword fail]` (provider appends).

## Passthrough Endpoints

Non-chat endpoints (`/v1/models`, `/v1/embeddings`, etc.) use the global policy and first provider policy for connection details. There is no model-based routing on passthrough requests because the model is not extracted from the request body.

## Key Files

| Component | File |
|-----------|------|
| Policy struct and resolution | `internal/policy/policy.go` |
| Rules engine (CheckModel, CheckRules) | `internal/policy/engine.go` |
| Chat handler with routing | `internal/proxy/handler_chat.go` |
| Passthrough handler | `internal/proxy/handler_passthrough.go` |
| Provider config loading | `internal/config/config.go` |
| Example multi-provider config | `examples/providers/multi-provider.yaml` |
