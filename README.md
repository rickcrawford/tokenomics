```
  _____     _                            _
 |_   _|__ | | _____ _ __   ___  _ __ __(_) ___ ___
   | |/ _ \| |/ / _ \ '_ \ / _ \| '_ ` _ \| |/ __/ __|
   | | (_) |   <  __/ | | | (_) | | | | | | | (__\__ \
   |_|\___/|_|\_\___|_| |_|\___/|_| |_| |_|_|\___|___/
```

> **Personal Guardrails for Token Usage** - Control costs, enforce safety, and track consumption across your AI agents.

Tokenomics is an OpenAI-compatible reverse proxy that acts as a guardrail system for LLM usage. Instead of giving agents direct access to your API keys, issue scoped wrapper tokens bound to guardrail policies. Each token controls what models can be used, how much can be spent, what content is allowed, and what gets recorded.

One binary. Zero code changes. Drop it in front of any agent that speaks the OpenAI protocol.

## What It Does

### 🛑 Cost Guardrails

Prevent overspending before it happens. Every wrapper token has a budget, rate limits, and model restrictions. When limits are reached, the proxy blocks requests—no surprises on the invoice.

- **Token budgets** with daily, hourly, and monthly caps
- **Rate limiting** on requests/min, tokens/hour, and max parallel requests
- **Model allowlists** so not every task burns through your most expensive models
- **Token expiration** with durations (24h, 7d) or exact timestamps for temporary access
- **Multi-provider routing** to send requests to the cheapest provider that fits your constraints

### 🔒 Safety Guardrails

Content inspection runs on every request before it reaches the provider. Detect and block prompt injections, redact PII, enforce output policies, or log everything for audit.

- **Jailbreak detection** blocks prompt injection attempts that try to override instructions
- **PII masking** auto-redacts SSNs, credit cards, emails, API keys, private keys, and 6 more types
- **Content rules** using regex, keyword, and pattern matching with fail/warn/log/mask actions
- **System prompts** injected server-side so agents always operate under the right instructions
- **Retry and fallback** chains automatically recover from provider failures with cheaper models

### 📋 Usage Tracking

Every conversation that flows through the proxy is optionally recorded. Session logs capture the full request/response exchange with cost details. Store on disk, in Redis, or both for team access.

- **Per-token conversation logs** in markdown, grouped by date and session
- **File or Redis backends** for local development or shared team deployments
- **Configurable per policy**, so sensitive tokens skip recording while others capture everything
- **Pattern-based file naming** with `{token_hash}`, `{date}`, and `{token_hash}` placeholders

### 📊 Cost Attribution

The ledger writes a JSON summary for every proxy session into a `.tokenomics/` directory. Commit it alongside your code. Over time, you get a complete record of token consumption per feature, per branch, per agent, per team.

- **Per-session JSON** with request-level detail and rollups by model, provider, and token
- **Git context** captures branch, start commit, and end commit for cost-per-feature analysis
- **Provider metadata** normalizes cached tokens, reasoning tokens, actual model served, and rate limits
- **CLI commands** (`ledger summary`, `ledger sessions`, `ledger show`) for usage analysis
- **Cost-per-feature** attribution by committing `.tokenomics/` and querying by branch

### 🔄 Multi-Provider Support

One wrapper token can route to any provider. The policy decides which API key to use based on model and constraints. Switch providers without changing your agent code.

Supported providers include OpenAI, Anthropic, Azure OpenAI, Google Gemini, Groq, Mistral, Cohere, Perplexity, DeepSeek, Together AI, Fireworks AI, Replicate, AWS Bedrock, and any OpenAI-compatible endpoint.

### 🔍 Observability

Every request produces structured JSON logs with token counts, latency, upstream IDs, rule matches, retry counts, and provider details. Webhooks fire on token events, violations, budget alerts, rate limit hits, and completions.

## Installation

```bash
curl -fsSL https://github.com/rickcrawford/tokenomics/releases/latest/download/install.sh | bash
```

Or build from source:

```bash
git clone https://github.com/rickcrawford/tokenomics.git
cd tokenomics && make build
sudo cp bin/tokenomics /usr/local/bin/
```

Verify: `tokenomics --help`

## Quick Start

Full guide: [Quick Start](docs/QUICK_START.md).

**1. Set environment variables**

```bash
export OPENAI_API_KEY="<your-openai-api-key>"
export TOKENOMICS_HASH_KEY="<any-random-secret-string>"
```

**2. Create a wrapper token**

```bash
tokenomics token create --policy '{"base_key_env":"OPENAI_API_KEY"}'
```

**3. Run**

```bash
export TOKENOMICS_KEY="tkn_<paste-your-token-here>"
tokenomics run python my_script.py
```

The `run` command starts the proxy, configures environment variables, runs your command, and cleans up. No separate server setup needed.

**Default directory:** Tokenomics stores data (tokens, ledger, certs) in `~/.tokenomics/` by default. Use `--dir .tokenomics` to use the current directory, or `--dir /path` for a custom location.

See [examples/](examples/) for provider configs, sample policies, and an end-to-end walkthrough.

## Features

| Guardrail Type | Feature | Description |
|---|---|---|
| **Cost Control** | Token budgets | Per-token daily/monthly spending caps |
| **Cost Control** | Rate limiting | Requests/min, tokens/hour, max parallel; sliding or fixed window |
| **Cost Control** | Model allowlists | Exact match or regex-based model filtering |
| **Cost Control** | Token expiration | Temporary access with durations (24h, 7d) or timestamps |
| **Safety** | Jailbreak detection | Detect prompt injection attempts that override instructions |
| **Safety** | Content rules | Regex, keyword, and PII rules with fail/warn/log/mask actions |
| **Safety** | PII masking | Auto-redact SSNs, credit cards, emails, API keys, and 7 more types |
| **Safety** | System prompts | Server-side instruction injection on every request |
| **Safety** | Retry and fallback | Auto-recover from failures with model fallback chains |
| **Tracking** | Conversation logs | Per-token markdown logs of user/assistant exchanges |
| **Tracking** | Redis backend | Shared memory across distributed agents |
| **Tracking** | Session JSON | Per-session logs with request-level detail and rollups |
| **Tracking** | Git context | Branch, commit start/end for cost-per-feature analysis |
| **Tracking** | Provider metadata | Cached tokens, reasoning tokens, actual model, rate limits |
| **Tracking** | CLI commands | `ledger summary`, `ledger sessions`, `ledger show` |
| **Routing** | Multi-provider | Route to 17+ providers with model-based selection |
| **Routing** | Remote sync | Load tokens from a central config server via webhooks |
| **Observability** | Structured logging | JSON logs with rule matches, upstream IDs, and costs |
| **Observability** | Webhooks | Events for violations, budget alerts, rate limits |
| **Security** | Encryption | AES-256-GCM at-rest encryption for policies |
| **Security** | Token isolation | Scoped wrapper tokens instead of raw API keys |

## Documentation

| Topic | Description |
|-------|-------------|
| [Features](docs/FEATURES.md) | Complete feature reference organized by category |
| [Quick Start](docs/QUICK_START.md) | Fast setup and first request in minutes |
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
| [OpenClaw Integration](docs/OPENCLAW.md) | Connect OpenClaw agents to Tokenomics guardrails |

## OpenClaw Integration

Tokenomics provides personal guardrails for OpenClaw autonomous agents. Set budgets, enforce safety policies, and track costs across distributed agent fleets—all without modifying agent code.

**Example:** Run a Slack bot with:
- Daily budget: 1M tokens
- Safety rules: Block jailbreaks, mask PII, detect injection attempts
- Fallback providers: Try Anthropic if OpenAI is over capacity
- Usage tracking: Record conversations and cost attribution

See [examples/openclaw](examples/openclaw/) for complete examples (Slack, Discord, personal assistant) and [docs/OPENCLAW.md](docs/OPENCLAW.md) for the integration guide.

## Author

**Rick Crawford** - [LinkedIn](https://www.linkedin.com/in/rickcrawford/) | [GitHub](https://github.com/rickcrawford)

## License

[MIT](LICENSE)
