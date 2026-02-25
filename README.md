```
  _____     _                            _
 |_   _|__ | | _____ _ __   ___  _ __ __(_) ___ ___
   | |/ _ \| |/ / _ \ '_ \ / _ \| '_ ` _ \| |/ __/ __|
   | | (_) |   <  __/ | | | (_) | | | | | | | (__\__ \
   |_|\___/|_|\_\___|_| |_|\___/|_| |_| |_|_|\___|___/
```

> *Because sometimes the most important tokens aren't on the blockchain -- they're on your OpenAI invoice.*

Tokenomics is an OpenAI-compatible reverse proxy that gives you monetary policy over your AI spend. Instead of handing out raw API keys like candy at a parade, you issue scoped wrapper tokens -- each one bound to a policy that controls models, budgets, rate limits, content rules, and more.

Think of it as a central bank for your API tokens. You set the policy. You control the supply. You decide who gets to spend what, where, and how fast.

## Why

You gave an intern an API key. They discovered `gpt-4o`. Your CFO discovered the bill.

Tokenomics sits between your agents and your providers so you can:

- **Cap the spend** -- per-token budgets so nobody surprise-bankrupts the department
- **Pick the models** -- regex-based model allowlists per token, because not every task needs the flagship
- **Rate limit everything** -- requests per minute, tokens per hour, max parallel calls
- **Block bad content** -- regex, keyword, and PII detection rules that can fail, warn, log, or mask
- **Mask sensitive data** -- automatically redact SSNs, credit cards, emails, and API keys before they hit the wire
- **Inject system prompts** -- prepend instructions to every request without trusting the client to do it
- **Retry and fallback** -- automatic retries with model fallback chains so you're not paging at 3am
- **Route to any provider** -- one token can hit OpenAI, Anthropic, Gemini, Mistral, and 13 more
- **Encrypt at rest** -- policies are AES-256-GCM encrypted in the database
- **Log everything** -- structured JSON logs with rule matches, upstream request IDs, and cost metadata

All without changing a single line in your agent code.

## Quick Start

### 1. Build

```bash
make build
```

### 2. Create a token

```bash
export TOKENOMICS_HASH_KEY="my-secret-hash-key"

./bin/tokenomics token create --policy '{
  "base_key_env": "OPENAI_API_KEY",
  "max_tokens": 100000,
  "model_regex": "^gpt-4.*",
  "rules": [
    {"type": "pii", "detect": ["ssn", "credit_card"], "action": "mask"},
    {"type": "regex", "pattern": "(?i)ignore.*instructions", "action": "fail"}
  ]
}'
```

Save the printed `tkn_<uuid>` token -- it is only shown once.

### 3. Start the proxy

```bash
export OPENAI_API_KEY="sk-your-real-key"
export TOKENOMICS_HASH_KEY="my-secret-hash-key"

./bin/tokenomics serve
```

The proxy listens on `:8443` (HTTPS) and `:8080` (HTTP) by default.

### 4. Connect your agent

```bash
eval $(./bin/tokenomics init --token tkn_<your-token> --port 8443 --insecure)
```

Your agent now routes all API calls through Tokenomics. Run it as usual.

## Token Management

Full CRUD from the CLI. Tokens can have expiration dates for temporary access.

```bash
# Create with a 30-day expiration
tokenomics token create --policy '{"base_key_env":"OPENAI_API_KEY"}' --expires 30d

# List all tokens
tokenomics token list

# Inspect a token
tokenomics token get --hash <token-hash>

# Update the policy or extend expiration
tokenomics token update --hash <token-hash> --policy '{"max_tokens":50000}' --expires 1y

# Revoke
tokenomics token delete --hash <token-hash>
```

Expiration supports durations (`24h`, `7d`, `30d`, `1y`), RFC3339 timestamps, or `clear` to remove.

## Multi-Provider Support

One token, many providers. Tokenomics routes to the right backend based on the model name in the request.

```json
{
  "providers": {
    "openai": [{ "base_key_env": "OPENAI_API_KEY", "model_regex": "^gpt" }],
    "anthropic": [{ "base_key_env": "ANTHROPIC_API_KEY", "model_regex": "^claude" }],
    "google": [{ "base_key_env": "GEMINI_API_KEY", "model_regex": "^gemini" }]
  }
}
```

The proxy automatically handles provider-specific authentication (Bearer, header-based, query string), custom headers, and chat endpoint paths. 16+ providers are pre-configured out of the box.

**Supported providers:** OpenAI, Anthropic, Azure OpenAI, Google Gemini, Vertex AI, Mistral, Cohere, Groq, Together AI, Fireworks AI, Perplexity, DeepSeek, xAI (Grok), OpenRouter, Ollama, vLLM, LiteLLM.

## Content Rules

Rules are objects with a type, action, and scope. Three rule types cover most content policies:

| Type | Matches on | Example |
|------|-----------|---------|
| `regex` | Go regular expression | `{"type":"regex","pattern":"(?i)drop\\s+table","action":"fail"}` |
| `keyword` | Case-insensitive word boundary | `{"type":"keyword","keywords":["bomb","exploit"],"action":"warn"}` |
| `pii` | Built-in PII patterns | `{"type":"pii","detect":["ssn","credit_card","email"],"action":"mask"}` |

Four actions control what happens on match:

| Action | Behavior |
|--------|----------|
| `fail` | Block the request with 403 |
| `warn` | Allow but log a warning |
| `log` | Silently record the match |
| `mask` | Redact matched content with `[REDACTED]` before forwarding |

Rules can target `input` (user messages, default), `output` (responses), or `both`.

**Built-in PII detectors:** SSN, credit card, email, phone, IP address, AWS key, API key, JWT, private key, connection string, GitHub token.

```json
{
  "rules": [
    {"type": "pii", "detect": ["ssn", "credit_card", "email"], "action": "mask", "scope": "both"},
    {"type": "regex", "pattern": "(?i)ignore.*instructions", "action": "fail"},
    {"type": "keyword", "keywords": ["jailbreak", "bypass"], "action": "warn", "name": "prompt-injection"}
  ]
}
```

Backward compatible -- old `["regex_string"]` format still works and auto-converts to `fail` rules.

## Documentation

| Topic | Description |
|-------|-------------|
| [Configuration](docs/CONFIGURATION.md) | config.yaml fields, environment variables, CLI flags |
| [Policies](docs/POLICIES.md) | Policy JSON schema, model filtering, rules, prompts |
| [Token Management](docs/TOKEN_MANAGEMENT.md) | Creating, listing, and deleting tokens |
| [Agent Integration](docs/AGENT_INTEGRATION.md) | Connecting OpenAI, Anthropic, Azure, Gemini, and custom agents |
| [TLS](docs/TLS.md) | Auto-generated certificates, CA trust, custom certs |
| [Stats & Logging](docs/STATS_AND_LOGGING.md) | Request logging, /stats endpoint, usage tracking |
| [Events & Webhooks](docs/EVENTS.md) | Webhook events for token CRUD, rule violations, budget alerts |

## Author

**Rick Crawford** - [LinkedIn](https://www.linkedin.com/in/rickcrawford/) | [GitHub](https://github.com/rickcrawford)

## License

[MIT](LICENSE)
