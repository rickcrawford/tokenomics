```
  _____     _                            _
 |_   _|__ | | _____ _ __   ___  _ __ __(_) ___ ___
   | |/ _ \| |/ / _ \ '_ \ / _ \| '_ ` _ \| |/ __/ __|
   | | (_) |   <  __/ | | | (_) | | | | | | | (__\__ \
   |_|\___/|_|\_\___|_| |_|\___/|_| |_| |_|_|\___|___/
```

> *Because sometimes the most important tokens aren't on the blockchain. They're on your OpenAI invoice.*

Tokenomics is an OpenAI-compatible reverse proxy that gives you monetary policy over your AI spend. Issue scoped wrapper tokens instead of sharing raw API keys. Each token is bound to a policy that controls models, budgets, rate limits, content rules, and more.

Think of it as a central bank for your API tokens. You set the policy. You control the supply.

## Why

You gave an intern an API key. They discovered `gpt-4o`. Your CFO discovered the bill.

Tokenomics sits between your agents and your providers so you can cap spend, pick models, rate limit, block bad content, mask PII, inject prompts, retry with fallbacks, and route across 16+ providers. All without changing a single line in your agent code.

## Quick Start

```bash
make build

export TOKENOMICS_HASH_KEY="my-secret-hash-key"
export OPENAI_API_KEY="sk-your-real-key"

# Create a token with a budget, model allowlist, and PII masking
./bin/tokenomics token create --policy '{
  "base_key_env": "OPENAI_API_KEY",
  "max_tokens": 100000,
  "model_regex": "^gpt-4.*",
  "rules": [
    {"type": "pii", "detect": ["ssn", "credit_card"], "action": "mask"},
    {"type": "regex", "pattern": "(?i)ignore.*instructions", "action": "fail"}
  ]
}'

# Start the proxy
./bin/tokenomics serve

# Point your agent at it
eval $(./bin/tokenomics init --token tkn_<your-token> --port 8443 --insecure)
```

See [examples/](examples/) for provider configs, sample policies, env setup, and an end-to-end walkthrough.

## Features

| Feature | What it does |
|---------|-------------|
| **Token budgets** | Per-token max_tokens caps so nobody surprise-bankrupts the department |
| **Model allowlists** | Exact match or regex. Not every task needs the flagship |
| **Rate limiting** | Requests/min, tokens/hour, max parallel; sliding or fixed window |
| **Content rules** | Regex, keyword, and PII rules that fail, warn, log, or mask |
| **PII masking** | Auto-redact SSNs, credit cards, emails, API keys, and 7 more types |
| **System prompts** | Prepend instructions to every request server-side |
| **Retry & fallback** | Auto-retry with model fallback chains on 429/5xx |
| **Multi-provider** | One token routes to OpenAI, Anthropic, Gemini, Groq, and 13 more |
| **Encryption** | AES-256-GCM at-rest encryption for stored policies |
| **Webhooks** | Events for token CRUD, rule violations, budget alerts, request completion |
| **Token expiration** | Temporary access with durations (24h, 7d, 30d) or RFC3339 timestamps |
| **Structured logging** | JSON logs with rule matches, upstream IDs, and cost metadata |

## Documentation

| Topic | Description |
|-------|-------------|
| [Examples](examples/) | Provider configs, sample policies, webhook collector, env template |
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
