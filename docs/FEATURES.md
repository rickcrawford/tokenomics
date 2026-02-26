# Features

Complete feature reference for Tokenomics, organized by category. Every feature listed here is implemented, tested, and production-ready.

## Cost Control

| Feature | Description | Docs |
|---------|-------------|------|
| Token budgets | Per-token `max_tokens` caps. When the budget runs out, the proxy stops forwarding. | [Policies](POLICIES.md#max_tokens-optional) |
| Per-model budgets | Different budgets for different models within the same token. Expensive models get tight limits, cheap models get generous ones. | [Multi-Model Routing](MULTI_MODEL_ROUTING.md#per-model-budgets) |
| Rate limiting | Multiple rules per token: requests/min, tokens/hour, max parallel. Supports sliding and fixed window strategies. | [Policies](POLICIES.md#rate-limiting) |
| Model allowlists | Restrict models by exact name or regex pattern. Requests for unauthorized models are rejected. | [Policies](POLICIES.md#model--model_regex-optional) |
| Token expiration | Temporary access with durations (`24h`, `7d`, `30d`, `1y`) or exact RFC3339 timestamps. Expired tokens are rejected at lookup time. | [Token Management](TOKEN_MANAGEMENT.md) |
| Per-request timeout | Configurable upstream timeout per token or per provider policy. Prevents runaway requests. | [Policies](POLICIES.md#timeout) |

## Guardrails

| Feature | Description | Docs |
|---------|-------------|------|
| Content rules | Regex, keyword, and PII detection on request content. Actions: `fail` (block), `warn` (allow + log), `log` (silent), `mask` (redact). | [Policies](POLICIES.md#rules-optional) |
| PII masking | Auto-redact 11 PII types: SSN, credit card, email, phone, IP address, AWS key, API key, JWT, private key, connection string, GitHub token. | [Policies](POLICIES.md#rules-optional) |
| Output rules | Rules scoped to `output` or `both` inspect response content from the provider before returning to the client. | [Policies](POLICIES.md#rules-optional) |
| System prompts | Server-side prompt injection. Messages prepended to every chat completion request. Provider-specific prompts prepend before global prompts. | [Policies](POLICIES.md#prompts-optional) |
| Retry and fallback | Auto-retry on configurable status codes (default: 429, 500, 502, 503) with model fallback chains. Each fallback goes through full provider resolution. | [Policies](POLICIES.md#retry-and-fallback) |

## Multi-Provider Routing

| Feature | Description | Docs |
|---------|-------------|------|
| Model-based routing | Single wrapper token routes to any provider based on the requested model name. The proxy resolves API key, upstream URL, auth scheme, and headers automatically. | [Multi-Model Routing](MULTI_MODEL_ROUTING.md) |
| 17+ providers | Pre-configured: OpenAI, Anthropic, Azure OpenAI, Google Gemini, Vertex AI, Groq, Mistral, Cohere, Perplexity, DeepSeek, Together AI, Fireworks AI, xAI, OpenRouter, Replicate, Ollama, vLLM, LiteLLM. | [Policies](POLICIES.md#provider-configuration-file) |
| Auth schemes | Bearer token, custom header (`x-api-key`), query parameter (`?key=`). Configured per provider. | [Multi-Model Routing](MULTI_MODEL_ROUTING.md#auth-schemes) |
| Custom headers | Per-provider headers (e.g., `anthropic-version`) added automatically to upstream requests. | [Policies](POLICIES.md#provider-config-fields) |
| Custom chat paths | Per-provider endpoint overrides (e.g., `/v1/messages` for Anthropic). | [Policies](POLICIES.md#provider-config-fields) |
| Policy merge | Provider policies merge onto global: `base_key_env`, `upstream_url`, `max_tokens`, `timeout` replace. `prompts` prepend. `rules` append. | [Multi-Model Routing](MULTI_MODEL_ROUTING.md#merge-behavior) |
| Passthrough endpoints | Non-chat endpoints (`/v1/models`, `/v1/embeddings`, etc.) forwarded using global policy connection details. | [Multi-Model Routing](MULTI_MODEL_ROUTING.md#passthrough-endpoints) |
| Client auth formats | Accepts wrapper tokens via `Authorization: Bearer`, `x-api-key`, or raw `Authorization` header. All SDK formats work. | [Policies](POLICIES.md#client-authentication-formats) |

## Conversation Memory

| Feature | Description | Docs |
|---------|-------------|------|
| Per-token logs | Markdown logs of user/assistant exchanges, grouped by session. | [Policies](POLICIES.md#memory-session-logging) |
| File backend | Single-file append or per-session files with `{token_hash}` and `{date}` placeholders in file names. | [Policies](POLICIES.md#per-session-files-recommended) |
| Redis backend | Push conversation entries to a Redis list keyed by session (`tokenomics:memory:<session_id>`). | [Policies](POLICIES.md#redis-based-memory) |
| Per-policy config | Memory enabled/disabled per token. Sensitive tokens skip memory while others record everything. | [Policies](POLICIES.md#memory-session-logging) |

## Session Ledger

| Feature | Description | Docs |
|---------|-------------|------|
| Per-session JSON | Request-level detail with rollups by model, provider, and token. Written to `.tokenomics/sessions/` on proxy shutdown. | [Ledger](LEDGER.md#session-json-format) |
| Git context | Captures branch, start commit, and end commit for cost-per-feature and cost-per-branch analysis. | [Ledger](LEDGER.md#git-context) |
| Provider metadata | Normalized across providers: cached tokens, reasoning tokens, actual model served, finish reason, upstream rate limits. | [Ledger](LEDGER.md#provider-metadata) |
| Rollup dimensions | Aggregated views by model, by provider, and by wrapper token. | [Ledger](LEDGER.md#rollup-dimensions) |
| Ledger memory | Optional conversation content in `memory/` subdirectory, `.gitignore`-able separately from session data. | [Ledger](LEDGER.md#memory-files) |

## Observability

| Feature | Description | Docs |
|---------|-------------|------|
| Structured logging | JSON logs per request with token counts, latency, model, upstream IDs, rule matches, retry counts, and cost metadata. | [Stats & Logging](STATS_AND_LOGGING.md#request-logging) |
| Log controls | Configure log level, format (JSON/text), request/response body logging, token hash masking, and per-request log suppression. | [Configuration](CONFIGURATION.md#logging) |
| File logging | Write logs to a file via `TOKENOMICS_LOG_FILE` environment variable. | [Configuration](CONFIGURATION.md#file-output) |
| /stats endpoint | Aggregated usage: global totals, by model+key, and by token. Available on both HTTP and HTTPS. | [Stats & Logging](STATS_AND_LOGGING.md#stats-endpoint) |
| /health endpoint | Returns `{"status":"ok"}` for health checks. | [Stats & Logging](STATS_AND_LOGGING.md#notes) |
| /ping endpoint | Returns 200 OK heartbeat. | [Stats & Logging](STATS_AND_LOGGING.md#notes) |
| Upstream request tracking | Correlates requests across proxy and provider with `client_request_id`, `upstream_request_id`, and `upstream_id`. | [Policies](POLICIES.md#upstream-request-tracking) |
| Metadata tags | Arbitrary key-value tags (`team`, `project`, `env`, `cost_center`) attached to every request log. | [Policies](POLICIES.md#metadata) |

## Events and Webhooks

| Feature | Description | Docs |
|---------|-------------|------|
| Outbound webhooks | HTTP POST events to one or more endpoints with configurable filters, timeouts, and async delivery. | [Events](EVENTS.md#configuration) |
| Event types | 12 event types: token CRUD (4), rule actions (4), budget (2), rate limiting (1), request completion (1), server start (1). | [Events](EVENTS.md#event-types) |
| Event filtering | Per-webhook filters with exact match, trailing wildcard (`token.*`), or catch-all (`*`). | [Events](EVENTS.md#event-filtering) |
| HMAC signatures | `X-Webhook-Signature` with SHA-256 HMAC for payload integrity verification. | [Events](EVENTS.md#hmac-signature) |
| Shared secrets | `X-Webhook-Secret` header for simple authentication. Both can be used together. | [Events](EVENTS.md#shared-secret) |
| Retry delivery | Failed deliveries retried up to 3 times with exponential backoff. 4xx (except 429) not retried. | [Events](EVENTS.md#delivery) |
| Inbound webhooks | Webhook receiver for push-based token sync from a central config server. Validates secrets and HMAC, debounces rapid events. | [Events](EVENTS.md#webhook-receiver-inbound) |

## Remote Sync

| Feature | Description | Docs |
|---------|-------------|------|
| Central config server | `tokenomics remote` runs a lightweight REST API that serves tokens to proxy instances. | [Events](EVENTS.md#webhook-receiver-inbound) |
| Poll sync | Proxy fetches tokens at a configurable interval (`remote.sync` seconds). | [Configuration](CONFIGURATION.md) |
| Push sync | Inbound webhook triggers immediate sync on `token.*` events from the central server. | [Events](EVENTS.md#push-vs-poll) |
| Push + poll fallback | Recommended setup: sub-second push with long-interval poll as safety net. | [Events](EVENTS.md#push-vs-poll) |

## Security

| Feature | Description | Docs |
|---------|-------------|------|
| At-rest encryption | AES-256-GCM encryption for policies stored in BoltDB. Key from environment variable. | [Configuration](CONFIGURATION.md) |
| HMAC token hashing | Wrapper tokens hashed with HMAC-SHA256 before storage. Raw tokens never persisted. | [Token Management](TOKEN_MANAGEMENT.md#hashing) |
| Auto TLS | Self-signed CA + server certificate generated on first run. CA valid 10 years, server cert 1 year. | [TLS](TLS.md#auto-generated-certificates) |
| Custom certificates | Bring your own cert and key. Auto-generation skipped when both are configured. | [TLS](TLS.md#custom-certificates) |

## CLI Commands

### Proxy Lifecycle

| Command | Description | Docs |
|---------|-------------|------|
| `tokenomics serve` | Start the proxy in the foreground. | [Configuration](CONFIGURATION.md) |
| `tokenomics start` | Start the proxy as a background daemon. Prints the proxy URL. | [Agent Integration](AGENT_INTEGRATION.md#manual-mode-tokenomics-start--tokenomics-init) |
| `tokenomics stop` | Stop the background proxy. Sends SIGTERM, then SIGKILL if needed. | [Agent Integration](AGENT_INTEGRATION.md#manual-mode-tokenomics-start--tokenomics-init) |
| `tokenomics run` | Start proxy, run a single command, stop proxy. Auto-detects provider from CLI name. | [Agent Integration](AGENT_INTEGRATION.md#quick-start-tokenomics-run-recommended) |

### Environment Setup

| Command | Description | Docs |
|---------|-------------|------|
| `tokenomics init` | Output environment variables for a provider. Supports `shell`, `dotenv`, and `json` formats. Does not start the proxy. | [Agent Integration](AGENT_INTEGRATION.md#tokenomics-init-flags) |

### Token Management

| Command | Description | Docs |
|---------|-------------|------|
| `tokenomics token create` | Create a wrapper token with a policy. Supports `--expires` for duration or timestamp. Supports `--policy @file.json` for file input. | [Token Management](TOKEN_MANAGEMENT.md#creating-a-token) |
| `tokenomics token get` | Inspect a token's details and policy by hash. Pretty-prints the policy JSON. | |
| `tokenomics token update` | Update a token's policy, expiration, or both. Use `--expires clear` to remove expiration. | |
| `tokenomics token list` | List all tokens with hash, creation time, expiration status, key env, model restrictions, and rule counts. | [Token Management](TOKEN_MANAGEMENT.md#listing-tokens) |
| `tokenomics token delete` | Revoke a token by hash. Requests using deleted tokens are rejected immediately. | [Token Management](TOKEN_MANAGEMENT.md#deleting-tokens) |

### Provider Management

| Command | Description | Docs |
|---------|-------------|------|
| `tokenomics provider list` | List all providers with upstream URL, auth scheme, API key env, key status, and model count. Supports `--output json`. | |
| `tokenomics provider test` | Test connectivity and credentials for one or more providers. Reports latency and auth status. | |
| `tokenomics provider models` | List known models for a provider or all providers. | |

### Session Ledger

| Command | Description | Docs |
|---------|-------------|------|
| `tokenomics ledger summary` | Aggregated token usage across all sessions. Totals plus breakdowns by model and provider. Supports `--json`. | [Ledger](LEDGER.md#tokenomics-ledger-summary) |
| `tokenomics ledger sessions` | List all recorded sessions with date, duration, request count, tokens, and branch. Supports `--json`. | [Ledger](LEDGER.md#tokenomics-ledger-sessions) |
| `tokenomics ledger show` | Show details for a specific session by ID (supports prefix matching). Supports `--json`. | [Ledger](LEDGER.md#tokenomics-ledger-show-session-id) |

### Diagnostics

| Command | Description | Docs |
|---------|-------------|------|
| `tokenomics doctor` | Run system health checks: config file, database, security keys, provider API keys, TLS certificates, proxy status, remote config. Reports ok/warn/fail per check. | |
| `tokenomics status` | Show proxy state (running/stopped), environment variables, provider key status, base URL overrides, and token count in database. | |
| `tokenomics version` | Print version, commit, build date, Go version, and OS/arch. | |

### Utilities

| Command | Description | Docs |
|---------|-------------|------|
| `tokenomics remote` | Start the central config server for remote token sync. Serves tokens over REST API with optional API key auth. | |
| `tokenomics completion` | Generate shell completion scripts for bash, zsh, fish, or powershell. | |

## Configuration

| Feature | Description | Docs |
|---------|-------------|------|
| YAML config | `config.yaml` from current directory or `$HOME/.tokenomics/`. Custom path via `--config`. | [Configuration](CONFIGURATION.md#config-file) |
| Environment overrides | Every config field maps to a `TOKENOMICS_` prefixed env var. | [Configuration](CONFIGURATION.md#environment-variables) |
| .env file | Auto-loads `.env` from current directory or `$HOME/.tokenomics/`. | [Agent Integration](AGENT_INTEGRATION.md#using-env-file) |
| Providers file | Separate `providers.yaml` with 17+ pre-configured providers. | [Policies](POLICIES.md#provider-configuration-file) |
| CLI maps | Map CLI tool names to providers for auto-detection in `tokenomics run`. | [Agent Integration](AGENT_INTEGRATION.md#auto-detection-cli-maps) |
| Priority order | CLI flags > environment variables > config file > defaults. | [Configuration](CONFIGURATION.md) |

## Distribution

| Feature | Description | Docs |
|---------|-------------|------|
| Pre-built binaries | Linux (x86_64, ARM64), macOS (Intel, Apple Silicon), Windows. | [Distribution](DISTRIBUTION.md) |
| Install script | One-line `curl` install with auto-detection of OS and architecture. | [Distribution](DISTRIBUTION.md#quick-install-recommended) |
| Build from source | `make build` produces `./bin/tokenomics`. | [Distribution](DISTRIBUTION.md#build-locally) |
| SHA256 checksums | Per-binary checksums published with each release. | [Distribution](DISTRIBUTION.md#checksums) |
| GitHub Actions | Automated build and release on version tags. | [Distribution](DISTRIBUTION.md#automated-release-process) |

## Architecture

| Component | Description | Code |
|-----------|-------------|------|
| Policy engine | Parses policy JSON, resolves provider by model, merges global + provider fields. | `internal/policy/` |
| Rules engine | Evaluates regex, keyword, and PII rules with fail/warn/log/mask actions. | `internal/policy/engine.go` |
| PII detector | 11 pattern-based detectors with masking support. | `internal/policy/pii.go` |
| Proxy handler | HTTP handler that extracts tokens, resolves policies, forwards requests, handles streaming. | `internal/proxy/` |
| Rate limiter | Per-token sliding/fixed window counters for requests and tokens, plus parallel request tracking. | `internal/proxy/ratelimit.go` |
| Token store | BoltDB with optional AES-256-GCM encryption. File watch for external DB changes. | `internal/store/` |
| Session tracking | In-memory or Redis-backed per-token usage counters. | `internal/session/` |
| Token counter | tiktoken-based token counting for budget enforcement. | `internal/tokencount/` |
| Event system | Emitter interface with webhook, multi-fan-out, and no-op implementations. | `internal/events/` |
| Session ledger | Per-session JSON recording with git context and provider metadata normalization. | `internal/ledger/` |
| Remote sync | REST server and client for centralized token management across proxy instances. | `internal/remote/` |
| TLS | Self-signed CA and server certificate generation. | `internal/tls/` |
