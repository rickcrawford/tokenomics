```
  _____     _                            _
 |_   _|__ | | _____ _ __   ___  _ __ __(_) ___ ___
   | |/ _ \| |/ / _ \ '_ \ / _ \| '_ ` _ \| |/ __/ __|
   | | (_) |   <  __/ | | | (_) | | | | | | | (__\__ \
   |_|\___/|_|\_\___|_| |_|\___/|_| |_| |_|_|\___|___/
```

[![GitHub Stars](https://img.shields.io/github/stars/rickcrawford/tokenomics?style=flat-square&label=Stars&color=blue)](https://github.com/rickcrawford/tokenomics)
[![MIT License](https://img.shields.io/badge/License-MIT-green?style=flat-square)](LICENSE)
[![Latest Release](https://img.shields.io/github/v/release/rickcrawford/tokenomics?style=flat-square&color=orange)](https://github.com/rickcrawford/tokenomics/releases)
[![Go Version](https://img.shields.io/github/go-mod/go-version/rickcrawford/tokenomics?style=flat-square)](go.mod)

> **Personal Guardrails for Token Usage** - Safety first (PII, prompts, rules), then scoped tokens with request and cost controls.

Tokenomics is an OpenAI-compatible reverse proxy you run yourself. It gives you the features of an AI gateway (guardrails, budgets, rate limits, multi-provider routing) but under your control from your client. No vendor lock-in, no sending traffic through a third party. Issue scoped wrapper tokens instead of raw API keys; each token enforces what models, content, and spend are allowed.

One binary. Zero code changes. Drop it in front of any agent that speaks the OpenAI protocol.

---

**Created by [Rick Crawford](https://github.com/rickcrawford)** • [LinkedIn](https://www.linkedin.com/in/rickcrawford/) • [MIT License](LICENSE)

---

## Keywords
`ai-proxy` • `llm-gateway` • `token-budgeting` • `cost-control` • `safety-guardrails` • `prompt-injection-detection` • `multi-provider-routing` • `rate-limiting` • `pii-masking` • `api-gateway` • `openai-compatible` • `agent-security`

---

## Table of Contents
- [What It Does](#what-it-does)
- [Installation](#installation)
- [Quick Start](#quick-start)
- [Features](#features)
- [Use Cases](#use-cases)
- [Documentation](#documentation)
- [OpenClaw Integration](#openclaw-integration)

---

## What It Does

### 🔒 Safety Guardrails (First)

Content inspection runs on every request before it hits the provider. PII, prompts, and rules stay under your control.

- **PII masking** auto-redacts SSNs, credit cards, emails, API keys, private keys, and 6 more types
- **Content rules** regex, keyword, and PII rules with fail/warn/log/mask on input, output, or both
- **System prompts** injected server-side so agents always run under the right instructions
- **Jailbreak detection** blocks prompt injection attempts that try to override instructions
- **Retry and fallback** chains recover from provider failures with cheaper models

### 🛑 PATs with Request and Cost Controls

Create scoped tokens (PATs) instead of handing out API keys. Each token has budgets, rate limits, and model restrictions. When limits are hit, the proxy blocks requests.

- **Token budgets** daily, hourly, and monthly caps per token
- **Rate limiting** requests/min, tokens/hour, max parallel; sliding or fixed window
- **Model allowlists** so not every task burns your most expensive models
- **Token expiration** durations (24h, 7d) or exact timestamps for temporary access
- **Multi-provider routing** send requests to the provider that fits your constraints

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
export OPENAI_PAT="<your-openai-api-key>"
export TOKENOMICS_HASH_KEY="<any-random-secret-string>"
```

**2. Create a wrapper token**

```bash
tokenomics token create --policy '{"base_key_env":"OPENAI_PAT"}'
```

**3. Run**

```bash
export TOKENOMICS_KEY="tkn_<paste-your-token-here>"
tokenomics run python my_script.py
```

The `run` command starts the proxy, configures environment variables, runs your command, and cleans up. No separate server setup needed. Admin is disabled for this ephemeral proxy unless you pass `--admin` and `admin.enabled` is true in config.

**Default directory:** Tokenomics stores data (tokens, ledger, certs) in `~/.tokenomics/` by default. Use `--dir .tokenomics` to use the current directory, or `--dir /path` for a custom location.

**Embedded admin UI:** Start the proxy and open `http://localhost:8080` or `https://localhost:8443` to view analytics, keys, sessions, and memory dashboards.

See [examples/](examples/) for provider configs, sample policies, and an end-to-end walkthrough.

## Features

| Guardrail Type | Feature | Description |
|---|---|---|
| **Safety** | PII masking | Auto-redact SSNs, credit cards, emails, API keys, and 7 more types |
| **Safety** | Content rules | Regex, keyword, and PII rules with fail/warn/log/mask actions |
| **Safety** | System prompts | Server-side instruction injection on every request |
| **Safety** | Jailbreak detection | Detect prompt injection attempts that override instructions |
| **Safety** | Retry and fallback | Auto-recover from failures with model fallback chains |
| **Cost Control** | Token budgets | Per-token daily/monthly spending caps |
| **Cost Control** | Rate limiting | Requests/min, tokens/hour, max parallel; sliding or fixed window |
| **Cost Control** | Model allowlists | Exact match or regex-based model filtering |
| **Cost Control** | Token expiration | Temporary access with durations (24h, 7d) or timestamps |
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

## Use Cases

**AI Safety & Compliance Teams**
Enforce content policies, detect prompt injection, mask PII, and maintain audit logs for regulated environments.

**Multi-Tenant SaaS Platforms**
Issue scoped tokens to customers with per-customer budgets, rate limits, and model restrictions. Track costs per tenant.

**Agent Fleet Operators**
Deploy 100+ autonomous agents with unified cost controls, safety guardrails, and usage tracking across your entire fleet.

**Cost-Conscious Development Teams**
Set monthly budgets, prevent runaway spend with fallback providers, and analyze cost-per-feature using git context.

**LLM Experimentation**
Switch between providers (OpenAI, Anthropic, Groq) without code changes. A/B test models and track costs.

**Enterprise API Management**
Replace expensive third-party gateways with a single binary you control. No vendor lock-in, no traffic routing through third parties.

## Documentation

| Topic | Description |
|-------|-------------|
| [Features](docs/FEATURES.md) | Complete feature reference organized by category |
| [Quick Start](docs/QUICK_START.md) | Fast setup and first request in minutes |
| [Examples](examples/) | Provider configs, sample policies, webhook collector, env template |
| [Configuration](docs/CONFIGURATION.md) | config.yaml fields, environment variables, CLI flags |
| [Secrets & Environment](docs/SECRETS.md) | API key management, .env file handling, secret rotation |
| [Policies](docs/POLICIES.md) | Policy JSON schema, model filtering, rules, prompts, memory |
| [Token Management](docs/TOKEN_MANAGEMENT.md) | Creating, inspecting, updating, and deleting tokens |
| [Agent Integration](docs/AGENT_INTEGRATION.md) | Connecting agents via `run`, `init`, or manual proxy setup |
| [TLS](docs/TLS.md) | Auto-generated certificates, CA trust, custom certs |
| [Stats & Logging](docs/STATS_AND_LOGGING.md) | Request logging, /stats endpoint, usage tracking |
| [Events & Webhooks](docs/EVENTS.md) | Webhook events for token CRUD, rule violations, budget alerts |
| [Multi-Model Routing](docs/MULTI_MODEL_ROUTING.md) | Provider routing, model matching, auth schemes, fallback chains |
| [Session Ledger](docs/LEDGER.md) | Per-session token tracking, CLI commands, session JSON format |
| [Web Admin](docs/WEB.md) | Embedded admin UI routes, APIs, auth, and architecture |
| [Admin UI Guide](docs/ADMIN_UI.md) | Admin tabs, policy editor workflow, embedded docs, and maintenance |
| [Distribution](docs/DISTRIBUTION.md) | Installation methods, pre-built binaries, release process |
| [OpenClaw Integration](docs/OPENCLAW_INTEGRATION.md) | Connect OpenClaw agents to Tokenomics guardrails |

## OpenClaw Integration

Tokenomics provides personal guardrails for OpenClaw autonomous agents. Set budgets, enforce safety policies, and track costs across distributed agent fleets, all without modifying agent code.

**Example:** Run a Slack bot with:
- Daily budget: 1M tokens
- Safety rules: Block jailbreaks, mask PII, detect injection attempts
- Fallback providers: Try Anthropic if OpenAI is over capacity
- Usage tracking: Record conversations and cost attribution

See [examples/openclaw](examples/openclaw/) for complete examples (Slack, Discord, personal assistant) and [docs/OPENCLAW_INTEGRATION.md](docs/OPENCLAW_INTEGRATION.md) for the integration guide.
