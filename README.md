```
  _____     _                            _
 |_   _|__ | | _____ _ __   ___  _ __ __(_) ___ ___
   | |/ _ \| |/ / _ \ '_ \ / _ \| '_ ` _ \| |/ __/ __|
   | | (_) |   <  __/ | | | (_) | | | | | | | (__\__ \
   |_|\___/|_|\_\___|_| |_|\___/|_| |_| |_|_|\___|___/
```

> *Because sometimes the most important tokens aren't on the blockchain. They're on your OpenAI invoice.*

Tokenomics is an OpenAI-compatible reverse proxy that sits between your AI agents and your providers. Issue scoped wrapper tokens instead of sharing raw API keys. Each token is bound to a policy that controls what it can do, how much it can spend, and what gets recorded.

One binary. Zero code changes. Drop it in front of any agent that speaks the OpenAI protocol.

## What It Does

### Cost Control

Cap spend before it happens. Every wrapper token has a budget, rate limits, and model restrictions. When the budget runs out, the proxy stops forwarding. No surprises on the invoice.

- **Token budgets** with per-token max_tokens caps
- **Rate limiting** on requests/min, tokens/hour, and max parallel requests
- **Model allowlists** so not every task burns through the flagship model
- **Token expiration** with durations (24h, 7d) or exact timestamps
- **Multi-provider routing** to send requests to the cheapest provider that fits

### Guardrails

Content rules run on every request before it reaches the provider. Block prompt injections, redact PII, warn on sensitive patterns, or log everything for audit.

- **Content rules** using regex, keyword, and PII detection with fail/warn/log/mask actions
- **PII masking** that auto-redacts SSNs, credit cards, emails, API keys, and 7 more types
- **System prompts** injected server-side so agents always get the right instructions
- **Retry and fallback** chains that automatically try cheaper models on 429/5xx

### Shared Memory

Every conversation that flows through the proxy can be recorded. Memory files are per-token markdown logs that capture the full user/assistant exchange. Store them on disk, in Redis, or both.

- **Per-token conversation logs** in markdown, grouped by session
- **File or Redis backends** for local development or shared team access
- **Configurable per policy**, so sensitive tokens skip memory while others record everything
- **Pattern-based file naming** with `{token_hash}` and `{date}` placeholders

### Session Ledger

The ledger writes a JSON summary for every proxy session into a `.tokenomics/` directory in your project. Commit it alongside your code. Over time, you get a complete record of token consumption per feature, per branch, per developer.

- **Per-session JSON** with request-level detail and rollups by model, provider, and token
- **Git context** captures branch, start commit, and end commit for feature attribution
- **Provider metadata** normalizes cached tokens, reasoning tokens, actual model served, and rate limits across OpenAI, Anthropic, Gemini, Azure, and Mistral
- **CLI commands** (`ledger summary`, `ledger sessions`, `ledger show`) for viewing aggregated usage
- **Cost-per-feature analysis** by committing `.tokenomics/` and querying by branch

### Multi-Provider Routing

One wrapper token can route to any provider. The policy decides which API key to use based on the requested model. Switch providers without touching your agent code.

Supported providers include OpenAI, Anthropic, Azure OpenAI, Google Gemini, Groq, Mistral, Cohere, Perplexity, DeepSeek, Together AI, Fireworks AI, Replicate, AWS Bedrock, and any OpenAI-compatible endpoint.

### Observability

Every request produces a structured JSON log with token counts, latency, upstream IDs, rule match details, retry counts, and provider metadata. Webhooks fire on token CRUD, rule violations, budget alerts, rate limit hits, and request completion.

## Installation

```bash
curl -fsSL https://raw.githubusercontent.com/rickcrawford/tokenomics/main/install.sh | bash
```

Or build from source:

```bash
git clone https://github.com/rickcrawford/tokenomics.git
cd tokenomics && make build
sudo cp bin/tokenomics /usr/local/bin/
```

Verify: `tokenomics --help`

## Quick Start

```bash
# Install CA certificate (first time only)
./bin/tokenomics serve  # Generates certs, shows install instructions

# Set your wrapper token
export TOKENOMICS_KEY="tkn_my-wrapper-token"

# Run a single command through the proxy
tokenomics run claude "What is the capital of France?"
```

The `run` command auto-detects the provider, starts the proxy, runs your command, and cleans up.

For multiple commands, start the proxy separately:

```bash
export TOKENOMICS_KEY="tkn_my-wrapper-token"
tokenomics start              # Start proxy daemon
eval $(tokenomics init)       # Set env vars for default provider

claude "prompt 1"
python script.py
node app.js

tokenomics stop               # Stop when done
```

For development without certificates:

```bash
tokenomics run --insecure claude "What is the capital of France?"
```

### Create a Token with Policies

```bash
export OPENAI_API_KEY="sk-your-real-key"

./bin/tokenomics token create --policy '{
  "base_key_env": "OPENAI_API_KEY",
  "max_tokens": 100000,
  "rules": [
    {"type": "pii", "detect": ["ssn", "credit_card"], "action": "mask"},
    {"type": "regex", "pattern": "(?i)ignore.*instructions", "action": "fail"}
  ],
  "memory": {
    "enabled": true,
    "file_path": "./memory",
    "file_name": "{date}/{token_hash}.md"
  }
}'
```

See [examples/](examples/) for provider configs, sample policies, and an end-to-end walkthrough.

## Features

| Category | Feature | Description |
|----------|---------|-------------|
| **Cost** | Token budgets | Per-token max_tokens caps |
| **Cost** | Rate limiting | Requests/min, tokens/hour, max parallel; sliding or fixed window |
| **Cost** | Model allowlists | Exact match or regex model filtering |
| **Cost** | Token expiration | Temporary access with durations (24h, 7d) or timestamps |
| **Guardrails** | Content rules | Regex, keyword, and PII rules with fail/warn/log/mask actions |
| **Guardrails** | PII masking | Auto-redact SSNs, credit cards, emails, API keys, and 7 more types |
| **Guardrails** | System prompts | Server-side prompt injection on every request |
| **Guardrails** | Retry and fallback | Auto-retry with model fallback chains on 429/5xx |
| **Memory** | Conversation logs | Per-token markdown logs of user/assistant exchanges |
| **Memory** | Redis backend | Shared memory across distributed agents |
| **Ledger** | Session tracking | Per-session JSON with request-level detail and rollups |
| **Ledger** | Git context | Branch, commit start/end for cost-per-feature analysis |
| **Ledger** | Provider metadata | Cached tokens, reasoning tokens, actual model, rate limits |
| **Ledger** | CLI commands | `ledger summary`, `ledger sessions`, `ledger show` |
| **Routing** | Multi-provider | Route to OpenAI, Anthropic, Gemini, Groq, and 13 more |
| **Routing** | Remote sync | Load tokens from a central config server via webhooks or polling |
| **Observability** | Structured logging | JSON logs with rule matches, upstream IDs, and cost metadata |
| **Observability** | Webhooks | Events for token CRUD, rule violations, budget alerts |
| **Security** | Encryption | AES-256-GCM at-rest encryption for stored policies |

## Documentation

| Topic | Description |
|-------|-------------|
| [Features](docs/FEATURES.md) | Complete feature reference organized by category |
| [Examples](examples/) | Provider configs, sample policies, webhook collector, env template |
| [Configuration](docs/CONFIGURATION.md) | config.yaml fields, environment variables, CLI flags |
| [Policies](docs/POLICIES.md) | Policy JSON schema, model filtering, rules, prompts, memory |
| [Token Management](docs/TOKEN_MANAGEMENT.md) | Creating, inspecting, updating, and deleting tokens |
| [Agent Integration](docs/AGENT_INTEGRATION.md) | Connecting agents via `run`, `init`, or manual proxy setup |
| [TLS](docs/TLS.md) | Auto-generated certificates, CA trust, custom certs |
| [Stats & Logging](docs/STATS_AND_LOGGING.md) | Request logging, /stats endpoint, usage tracking |
| [Events & Webhooks](docs/EVENTS.md) | Webhook events for token CRUD, rule violations, budget alerts |
| [Multi-Model Routing](docs/MULTI_MODEL_ROUTING.md) | Provider routing, model matching, auth schemes, fallback chains |
| [Session Ledger](docs/LEDGER.md) | Per-session token tracking, CLI commands, session JSON format |
| [Distribution](docs/DISTRIBUTION.md) | Installation methods, pre-built binaries, release process |

## Author

**Rick Crawford** - [LinkedIn](https://www.linkedin.com/in/rickcrawford/) | [GitHub](https://github.com/rickcrawford)

## License

[MIT](LICENSE)
