# Ledger: Per-Session Token Tracking in Git

Record memory, logs, and token counts for every proxy session into a `.tokenomics/` folder in the project root. Data is committed alongside code changes, enabling token-per-feature and token-per-branch analysis over time. Cost estimation is intentionally excluded. Pricing is volatile and belongs at query time in whatever analytics layer consumes this data.

## Problem

Today, token usage lives only in-memory (`/stats` endpoint) or in stdout JSON logs. Both are ephemeral. There is no persistent, git-native record of what a coding session consumed. Teams cannot answer: "How many tokens did feature X use across all sessions?"

## Goals

| Goal | How |
|------|-----|
| Track token usage per session | Write a session summary JSON after each proxy run |
| Track token usage per feature | Tag every record with `git_branch` and `git_commit` |
| Record conversation memory | Capture user/assistant messages alongside token counts |
| Aggregate across sessions | CLI command reads `.tokenomics/` and rolls up by branch, model, date |
| Commit-friendly | Small, append-friendly files. No binary data |
| Raw facts only | No pricing or cost estimates. Store tokens, models, timing. Cost is a downstream concern |

## Folder Structure

```
.tokenomics/
  sessions/
    2026-02-25_a1b2c3d4.json            # one file per proxy session
  memory/
    2026-02-25_a1b2c3d4.md              # conversation log (optional, per session)
```

**Naming convention:** `<YYYY-MM-DD>_<session-id-short>.json` where session ID is an 8-char hex generated at proxy startup.

**Why separate memory files?** Conversation content can be large and may contain sensitive code. Keeping it in a separate `memory/` directory lets teams `.gitignore` it if they only want the cost data committed.

## Data Model

### Per-Request Entry

Each proxied request captures the full context the proxy already tracks in `RequestLog` (logging.go) and `UsageStats` (stats.go), plus metadata extracted from the provider's response headers and body. One proxy session can serve multiple tokens, so `token_hash` is on every entry.

| Field | Source | Purpose |
|-------|--------|---------|
| `timestamp` | Request start time | When |
| `token_hash` | HMAC-SHA256 of wrapper token | Which token (enables per-token rollups, multi-token sessions) |
| `model` | `reqBody["model"]` | Which model was requested |
| `provider` | `resolved.BaseKeyEnv` | Which provider API key was used (maps to provider for cost correlation) |
| `input_tokens` | tiktoken count | Input consumption |
| `output_tokens` | Response usage or delta count | Output consumption |
| `duration_ms` | `time.Since(start)` | Latency |
| `status_code` | Upstream response code | Success/failure |
| `stream` | `reqBody["stream"]` | Streaming vs buffered |
| `error` | Error message if any | What went wrong |
| `upstream_id` | Response body `id` field | Provider's completion ID (chatcmpl-*, msg_*) for billing correlation |
| `upstream_request_id` | Response headers | Provider's request correlation ID |
| `retry_count` | Retry loop counter | Wasted attempts (tokens burned on retries) |
| `fallback_model` | Fallback model used | When primary model failed |
| `rule_matches` | Content rule engine | Security/compliance events (warn, log, mask actions) |
| `metadata` | Policy tags | Team, cost_center, project labels from token policy |
| `provider_meta` | Response headers + body | Provider-reported token details, rate limits, model served (see below) |

### Provider Metadata (`provider_meta`)

Normalized fields extracted from each provider's response. All fields are optional and omitted when not present. This matters for cost correlation because cached tokens and reasoning tokens are billed at different rates.

| Field | Source | Purpose |
|-------|--------|---------|
| **Token detail** | | |
| `cached_input_tokens` | OpenAI `prompt_tokens_details.cached_tokens`, Anthropic `cache_read_input_tokens` | Cached tokens are typically 50-90% cheaper |
| `cache_creation_tokens` | Anthropic `cache_creation_input_tokens` | Tokens written to cache (billed at 1.25x on Anthropic) |
| `reasoning_tokens` | OpenAI `completion_tokens_details.reasoning_tokens` | o1/o3/o4 reasoning tokens (billed separately, often higher) |
| **Model identity** | | |
| `actual_model` | Response body `model` field | Model actually served. May differ from requested (e.g., request `gpt-4o` but get `gpt-4o-2024-11-20`) |
| `finish_reason` | OpenAI `choices[0].finish_reason`, Anthropic `stop_reason`, Gemini `candidates[0].finishReason` | Why generation stopped: stop, length, content_filter, tool_calls |
| **Rate limit state** | | |
| `rate_limit_remaining_requests` | Provider rate limit headers | Remaining requests in current window |
| `rate_limit_remaining_tokens` | Provider rate limit headers | Remaining tokens in current window |
| `rate_limit_reset` | Provider rate limit headers | When the current window resets (ISO 8601 or seconds) |

**Provider header mappings:**

| Provider | Remaining Requests | Remaining Tokens | Reset |
|----------|-------------------|------------------|-------|
| OpenAI | `x-ratelimit-remaining-requests` | `x-ratelimit-remaining-tokens` | `x-ratelimit-reset-requests` |
| Anthropic | `anthropic-ratelimit-requests-remaining` | `anthropic-ratelimit-tokens-remaining` | `anthropic-ratelimit-tokens-reset` |
| Azure | `x-ratelimit-remaining-requests` | `x-ratelimit-remaining-tokens` | `x-ratelimit-reset-tokens` |
| Gemini | (not exposed per-request) | (not exposed per-request) | `retry-after` (on 429 only) |
| Mistral | (same as OpenAI pattern) | (same as OpenAI pattern) | (same as OpenAI pattern) |

**Token detail extraction by provider:**

| Provider | Cached Input | Cache Write | Reasoning |
|----------|-------------|-------------|-----------|
| OpenAI | `usage.prompt_tokens_details.cached_tokens` | n/a | `usage.completion_tokens_details.reasoning_tokens` |
| Anthropic | `usage.cache_read_input_tokens` | `usage.cache_creation_input_tokens` | n/a |
| Gemini | `usageMetadata.cachedContentTokenCount` | n/a | n/a |
| Azure | Same as OpenAI | Same as OpenAI | Same as OpenAI |
| Mistral | n/a | n/a | n/a |

### Session Summary (`sessions/<date>_<id>.json`)

```json
{
  "session_id": "a1b2c3d4",
  "started_at": "2026-02-25T10:30:00Z",
  "ended_at": "2026-02-25T11:45:00Z",
  "duration_ms": 4500000,
  "git": {
    "branch": "feature/add-auth",
    "commit_start": "abc1234",
    "commit_end": "def5678",
    "repo_root": "/home/user/myproject"
  },
  "totals": {
    "request_count": 45,
    "input_tokens": 125000,
    "output_tokens": 89000,
    "total_tokens": 214000,
    "cached_input_tokens": 40000,
    "cache_creation_tokens": 5000,
    "reasoning_tokens": 12000,
    "error_count": 2,
    "retry_count": 3,
    "rule_violation_count": 1,
    "rate_limit_count": 0
  },
  "by_model": {
    "claude-sonnet-4-20250514": {
      "request_count": 30,
      "input_tokens": 80000,
      "output_tokens": 60000,
      "total_tokens": 140000,
      "cached_input_tokens": 40000,
      "cache_creation_tokens": 5000
    },
    "gpt-4o": {
      "request_count": 15,
      "input_tokens": 45000,
      "output_tokens": 29000,
      "total_tokens": 74000,
      "reasoning_tokens": 12000
    }
  },
  "by_provider": {
    "ANTHROPIC_API_KEY": {
      "request_count": 30,
      "input_tokens": 80000,
      "output_tokens": 60000,
      "total_tokens": 140000,
      "cached_input_tokens": 40000,
      "cache_creation_tokens": 5000
    },
    "OPENAI_API_KEY": {
      "request_count": 15,
      "input_tokens": 45000,
      "output_tokens": 29000,
      "total_tokens": 74000,
      "reasoning_tokens": 12000
    }
  },
  "by_token": {
    "a1b2c3d4e5f6g7h8": {
      "request_count": 40,
      "input_tokens": 110000,
      "output_tokens": 78000,
      "total_tokens": 188000,
      "models_used": ["claude-sonnet-4-20250514", "gpt-4o"],
      "first_seen": "2026-02-25T10:30:05Z",
      "last_seen": "2026-02-25T11:44:00Z"
    },
    "h8g7f6e5d4c3b2a1": {
      "request_count": 5,
      "input_tokens": 15000,
      "output_tokens": 11000,
      "total_tokens": 26000,
      "models_used": ["gpt-4o"],
      "first_seen": "2026-02-25T11:00:00Z",
      "last_seen": "2026-02-25T11:20:00Z"
    }
  },
  "requests": [
    {
      "timestamp": "2026-02-25T10:30:05Z",
      "token_hash": "a1b2c3d4e5f6g7h8",
      "model": "claude-sonnet-4-20250514",
      "provider": "ANTHROPIC_API_KEY",
      "input_tokens": 1500,
      "output_tokens": 800,
      "duration_ms": 3200,
      "status_code": 200,
      "stream": true,
      "upstream_id": "msg_abc123",
      "upstream_request_id": "req_def456",
      "provider_meta": {
        "actual_model": "claude-sonnet-4-20250514",
        "finish_reason": "end_turn",
        "cached_input_tokens": 1200,
        "cache_creation_tokens": 0,
        "rate_limit_remaining_requests": 95,
        "rate_limit_remaining_tokens": 78000
      }
    },
    {
      "timestamp": "2026-02-25T10:35:12Z",
      "token_hash": "a1b2c3d4e5f6g7h8",
      "model": "gpt-4o",
      "provider": "OPENAI_API_KEY",
      "input_tokens": 2000,
      "output_tokens": 1200,
      "duration_ms": 4100,
      "status_code": 200,
      "stream": true,
      "upstream_id": "chatcmpl-xyz789",
      "retry_count": 1,
      "fallback_model": "gpt-4o",
      "rule_matches": [{"action": "warn", "message": "keyword match: TODO"}],
      "metadata": {"team": "platform"},
      "provider_meta": {
        "actual_model": "gpt-4o-2024-11-20",
        "finish_reason": "stop",
        "reasoning_tokens": 450,
        "rate_limit_remaining_requests": 58,
        "rate_limit_remaining_tokens": 42000
      }
    }
  ]
}
```

**Design decisions:**

- `token_hash` on every request. One proxy session serves multiple tokens. This enables per-token rollups and multi-token attribution.
- `provider` (`base_key_env`) on every request. Different providers have different pricing. Downstream analytics needs this to join with cost tables.
- `provider_meta` captures what the provider actually reports back. This is the provider's truth, not our estimate. Key fields:
  - `cached_input_tokens` / `cache_creation_tokens` / `reasoning_tokens`: different pricing tiers. Cached tokens are 50-90% cheaper. Reasoning tokens (o1/o3/o4) can be 3-4x more expensive. Without these, cost correlation is inaccurate.
  - `actual_model`: providers may serve a different model version than requested. If you ask for `gpt-4o` you might get `gpt-4o-2024-11-20`. The actual model determines the billing rate.
  - `finish_reason`: tells you if generation was cut short by token limits, content filters, or natural completion. Useful for debugging truncated responses.
  - `rate_limit_remaining_*`: the provider's remaining capacity after each request. Useful for capacity planning and understanding throttling patterns.
- `cached_input_tokens`, `cache_creation_tokens`, and `reasoning_tokens` are rolled up into `totals`, `by_model`, and `by_provider` so downstream analytics can compute accurate costs without scanning individual requests.
- `by_model`, `by_provider`, and `by_token` rollups are computed at `Close()` time. Three dimensions to slice: what model, which provider API key, which wrapper token.
- `by_token` includes `models_used` and time range per token, mirroring what `UsageStats.SessionEntry` already tracks in memory.
- `retry_count` and `fallback_model` expose wasted work. Retries burn tokens on attempts that failed.
- `upstream_id` and `upstream_request_id` enable correlation with provider billing dashboards and dispute resolution.
- `rule_matches` captures security/compliance events without blocking the request (warn/log/mask actions). Violations that blocked requests show as errors.
- `error` field on requests captures the failure reason, not just the status code.
- `metadata` from the token's policy flows through to each request, enabling team/project/cost-center attribution.
- `totals` includes `retry_count`, `rule_violation_count`, `rate_limit_count` as session-level signals for operational health.
- `git.commit_start` is HEAD when the proxy starts. `git.commit_end` is HEAD when it stops. This brackets what code was being worked on.
- No pricing or cost fields. The raw token detail (cached, reasoning, standard) gives downstream analytics everything it needs to apply current rates.

### Session Memory (`memory/<date>_<id>.md`)

```markdown
## 2026-02-25T10:30:05Z | user | claude-sonnet-4-20250514

Write a function that validates email addresses.

---

## 2026-02-25T10:30:08Z | assistant | claude-sonnet-4-20250514

Here's a function that validates email addresses...

---
```

Same markdown format already used by `DirMemoryWriter`. The ledger reuses the existing memory writer infrastructure but targets the `.tokenomics/memory/` directory.

## Implementation Plan

### Phase 1: Core Ledger Package

**New package:** `internal/ledger/`

| File | Purpose |
|------|---------|
| `ledger.go` | `Ledger` struct, `Open()`, `Close()`, `RecordRequest()`, `WriteSession()` |
| `session.go` | `Session` struct, accumulates requests, computes rollups |
| `git.go` | `GitContext()`: detect branch, commit, repo root via `git` commands |
| `ledger_test.go` | Unit tests |

**Ledger struct:**

```go
type Ledger struct {
    dir        string       // .tokenomics/ path
    sessionID  string       // 8-char hex, generated at Open()
    session    *Session     // accumulates requests
    memWriter  session.MemoryWriter  // reuse existing DirMemoryWriter
    mu         sync.Mutex
}

func Open(dir string) (*Ledger, error)           // create dirs, snapshot git
func (l *Ledger) RecordRequest(entry RequestEntry) // append to session
func (l *Ledger) RecordMemory(tokenHash, role, model, content string) error
func (l *Ledger) Close() error                    // snapshot git end, compute rollups, write JSON
```

**Key behaviors:**

- `Open()` creates `.tokenomics/sessions/` and `.tokenomics/memory/` directories if missing. Captures `git.commit_start` and `git.branch`.
- `RecordRequest()` is called from the proxy after each completed request. Thread-safe. Appends to an in-memory list.
- `Close()` captures `git.commit_end`, computes `by_model`, `by_provider`, and `by_token` rollups, writes the session JSON file, and closes the memory writer.
- If the `.tokenomics/` directory does not exist and ledger is enabled, it gets created with a brief `README` explaining the folder.

### Phase 2: Config Integration

Add a `Ledger` section to `Config`:

```go
type LedgerConfig struct {
    Enabled bool   `mapstructure:"enabled"`  // default: false
    Dir     string `mapstructure:"dir"`      // default: ".tokenomics"
    Memory  bool   `mapstructure:"memory"`   // record conversation content, default: true
}
```

**config.yaml example:**

```yaml
ledger:
  enabled: true
  dir: ".tokenomics"
  memory: true
```

**Environment overrides:**

| Env Var | Field |
|---------|-------|
| `TOKENOMICS_LEDGER_ENABLED` | `ledger.enabled` |
| `TOKENOMICS_LEDGER_DIR` | `ledger.dir` |
| `TOKENOMICS_LEDGER_MEMORY` | `ledger.memory` |

### Phase 3: Proxy Integration

Wire the ledger into the serve command and proxy handler.

**`cmd/serve.go` changes:**

```go
// After session store init, before handler creation:
var ledgerInstance *ledger.Ledger
if cfg.Ledger.Enabled {
    dir := cfg.Ledger.Dir
    if dir == "" {
        dir = ".tokenomics"
    }
    ledgerInstance, err = ledger.Open(dir)
    if err != nil {
        log.Printf("Warning: ledger init failed: %v (continuing without ledger)", err)
    } else {
        defer ledgerInstance.Close()
    }
}

// Pass ledger to handler:
handler.SetLedger(ledgerInstance)
```

**`internal/proxy/handler.go` changes:**

Add `ledger *ledger.Ledger` field to `Handler`. After each `request.completed` event emission in `handleChatCompletions`, call:

```go
if h.ledger != nil {
    h.ledger.RecordRequest(ledger.RequestEntry{
        Timestamp:          time.Now().UTC(),
        TokenHash:          safePrefix(tokenHash, 16),
        Model:              tryModel,
        Provider:           resolved.BaseKeyEnv,
        InputTokens:        inputTokens,
        OutputTokens:       outputTokens,
        DurationMs:         elapsed.Milliseconds(),
        StatusCode:         resp.StatusCode,
        Stream:             stream,
        Error:              logEntry.Error,
        UpstreamID:         logEntry.UpstreamID,
        UpstreamRequestID:  logEntry.UpstreamRequestID,
        RetryCount:         retryCount,
        FallbackModel:      logEntry.FallbackModel,
        RuleMatches:        logEntry.RuleMatches,
        Metadata:           resolved.Metadata,
        ProviderMeta:       extractProviderMeta(resp.Header, respBody),
    })
}
```

The `RequestEntry` struct mirrors `RequestLog` from logging.go. We capture the same data the structured logs already emit, plus provider metadata, and persist it to disk.

**New helper: `extractProviderMeta()`**

Extracts normalized metadata from the provider's response headers and body. Added to `internal/proxy/` alongside the existing `extractUpstreamRequestID()` and `extractUpstreamID()` helpers.

```go
// ProviderMeta holds normalized metadata from the provider's response.
type ProviderMeta struct {
    // Token detail (from response body usage object)
    CachedInputTokens  int    `json:"cached_input_tokens,omitempty"`
    CacheCreationTokens int   `json:"cache_creation_tokens,omitempty"`
    ReasoningTokens    int    `json:"reasoning_tokens,omitempty"`

    // Model identity (from response body)
    ActualModel        string `json:"actual_model,omitempty"`
    FinishReason       string `json:"finish_reason,omitempty"`

    // Rate limit state (from response headers)
    RateLimitRemainingRequests int    `json:"rate_limit_remaining_requests,omitempty"`
    RateLimitRemainingTokens   int    `json:"rate_limit_remaining_tokens,omitempty"`
    RateLimitReset             string `json:"rate_limit_reset,omitempty"`
}

func extractProviderMeta(headers http.Header, body []byte) *ProviderMeta {
    meta := &ProviderMeta{}

    // Rate limit headers (normalized across providers)
    meta.RateLimitRemainingRequests = parseIntHeader(headers,
        "x-ratelimit-remaining-requests",              // OpenAI, Azure
        "anthropic-ratelimit-requests-remaining",       // Anthropic
    )
    meta.RateLimitRemainingTokens = parseIntHeader(headers,
        "x-ratelimit-remaining-tokens",                // OpenAI, Azure
        "anthropic-ratelimit-tokens-remaining",         // Anthropic
    )
    meta.RateLimitReset = firstHeader(headers,
        "x-ratelimit-reset-requests",                  // OpenAI
        "anthropic-ratelimit-tokens-reset",             // Anthropic
        "x-ratelimit-reset-tokens",                    // Azure
    )

    // Body fields (actual_model, finish_reason, token details)
    // Parsed from response JSON...

    return meta
}
```

For streaming responses, `actual_model`, `finish_reason`, and usage details are extracted from SSE chunks (the final chunk often contains the `usage` object). The existing `handleStreamingResponse` already parses chunks for token counting and will be extended to capture these fields.

For memory recording, when the existing memory writer fires, also write to the ledger's memory writer if enabled:

```go
if h.ledger != nil {
    h.ledger.RecordMemory(tokenHash, role, model, content)
}
```

### Phase 4: CLI Commands

**New command group:** `tokenomics ledger`

| Command | Description |
|---------|-------------|
| `tokenomics ledger summary` | Print totals across all sessions in `.tokenomics/` |
| `tokenomics ledger summary --by-branch` | Aggregate and group by git branch |
| `tokenomics ledger summary --by-model` | Aggregate and group by model |
| `tokenomics ledger summary --by-provider` | Aggregate and group by provider (base_key_env) |
| `tokenomics ledger summary --by-token` | Aggregate and group by wrapper token hash |
| `tokenomics ledger summary --branch <name>` | Filter to a specific branch |
| `tokenomics ledger summary --since 2026-02-01` | Filter by date range |
| `tokenomics ledger sessions` | List all recorded sessions |
| `tokenomics ledger show <session-id>` | Print details for one session |
| `tokenomics ledger init` | Create `.tokenomics/` directory structure |

**Example output for `tokenomics ledger summary --by-branch`:**

```
Branch                    Sessions  Requests  Input Tokens  Output Tokens  Total Tokens
feature/add-auth               5       120       450,000        280,000       730,000
feature/refactor-db            3        67       210,000        140,000       350,000
bugfix/login-redirect          1        12        35,000         18,000        53,000

TOTAL                          9       199       695,000        438,000     1,133,000
```

**Example output for `tokenomics ledger summary --by-model`:**

```
Model                        Requests  Input Tokens  Output Tokens  Total Tokens
claude-sonnet-4-20250514          120       400,000        250,000       650,000
gpt-4o                            55       220,000        150,000       370,000
gpt-4o-mini                       24        75,000         38,000       113,000

TOTAL                            199       695,000        438,000     1,133,000
```

**Example output for `tokenomics ledger summary --by-provider`:**

```
Provider              Requests  Input Tokens  Output Tokens  Total Tokens
ANTHROPIC_API_KEY          120       400,000        250,000       650,000
OPENAI_API_KEY              79       295,000        188,000       483,000

TOTAL                      199       695,000        438,000     1,133,000
```

**Example output for `tokenomics ledger summary --by-token`:**

```
Token Hash        Requests  Input Tokens  Output Tokens  Total Tokens  Models Used
a1b2c3d4e5f6g7h8      150       550,000        350,000       900,000  claude-sonnet-4-20250514, gpt-4o
h8g7f6e5d4c3b2a1       49       145,000         88,000       233,000  gpt-4o-mini

TOTAL                  199       695,000        438,000     1,133,000
```

### Phase 5: Documentation

| File | Changes |
|------|---------|
| `docs/LEDGER.md` | New file. Full ledger documentation: config, CLI, data format |
| `docs/CONFIGURATION.md` | Add `ledger` section |
| `README.md` | Add ledger to features table |
| `examples/config.yaml` | Add commented `ledger:` section |

## Integration Points

```
                    +-----------+
                    |  serve.go |
                    +-----+-----+
                          |
                    Open(dir)
                          |
                    +-----v-----+
                    |   Ledger  |
                    +-----+-----+
                          |
            +-------------+-------------+
            |                           |
      RecordRequest()            RecordMemory()
      (mirrors RequestLog)       (reuses DirMemoryWriter)
            |                           |
    +-------v--------+          +-------v--------+
    | sessions/*.json |          | memory/*.md    |
    +-----------------+          +----------------+
            |
      Close() -> compute rollups:
            |     by_model    (what model)
            |     by_provider (which API key)
            |     by_token    (which wrapper token)
            |
    +-------v--------+
    | Write JSON     |
    +----------------+


    CLI reads .tokenomics/ directly:

    +-------------------------------+
    | ledger summary                |----> totals across all sessions
    | ledger summary --by-branch    |----> group by git branch
    | ledger summary --by-model     |----> group by model
    | ledger summary --by-provider  |----> group by provider API key
    | ledger summary --by-token     |----> group by wrapper token
    | ledger sessions               |----> list sessions
    | ledger show <id>              |----> one session detail
    +-------------------------------+

    Downstream analytics (database, dashboard, etc.)
    joins provider + model + token counts with current pricing at query time.
```

## Files Changed

| File | Change Type | Description |
|------|-------------|-------------|
| `internal/ledger/ledger.go` | New | Core ledger: Open, RecordRequest, Close |
| `internal/ledger/session.go` | New | Session accumulator, rollup computation |
| `internal/ledger/git.go` | New | Git branch/commit detection |
| `internal/ledger/ledger_test.go` | New | Unit tests for ledger package |
| `internal/config/config.go` | Modify | Add `LedgerConfig` struct and field |
| `internal/proxy/handler.go` | Modify | Add `ledger` field, `SetLedger()` method |
| `internal/proxy/handler_chat.go` | Modify | Call `ledger.RecordRequest()` and `RecordMemory()`, extract provider metadata |
| `internal/proxy/provider_meta.go` | New | `extractProviderMeta()`, `ProviderMeta` struct, header/body parsing helpers |
| `internal/proxy/provider_meta_test.go` | New | Tests for provider metadata extraction across OpenAI, Anthropic, Gemini, Azure |
| `cmd/serve.go` | Modify | Initialize ledger on startup, defer Close |
| `cmd/ledger.go` | New | CLI commands: summary, sessions, show, init |
| `cmd/ledger_test.go` | New | CLI command tests |
| `docs/LEDGER.md` | New | Feature documentation |
| `docs/CONFIGURATION.md` | Modify | Add ledger config section |
| `README.md` | Modify | Add ledger to features table |
| `examples/config.yaml` | Modify | Add ledger config example |

## Git Workflow

1. Developer enables ledger in config (or via `TOKENOMICS_LEDGER_ENABLED=true`)
2. Runs `tokenomics serve` (or via `tokenomics run`/`tokenomics init`)
3. Proxy records every request to the in-memory session
4. On shutdown, session summary is written to `.tokenomics/sessions/`
5. Developer commits `.tokenomics/` alongside their code changes
6. PR reviewers can see the cost of the feature in the diff
7. After merge, `tokenomics ledger summary --by-branch` shows cost per feature

## Edge Cases

| Scenario | Handling |
|----------|----------|
| Not a git repo | `git` fields are empty strings, session still recorded |
| Proxy crashes (no graceful shutdown) | Session data lost for that run. Future: periodic flush |
| `.tokenomics/` dir is read-only | Log warning, continue without ledger |
| Concurrent proxy instances | Each gets unique session ID. No file conflicts |
| Very long session (1000+ requests) | `requests` array grows. Consider truncation flag in future |

## Future Enhancements

- **Periodic flush**: Write partial session data every N minutes to survive crashes.
- **JSONL mode**: For very long sessions, use append-only `requests.jsonl` instead of a single JSON array.
- **GitHub Actions integration**: `tokenomics ledger summary` as a PR comment showing feature token usage.
- **Token budget alerts**: Warn when session token count exceeds a threshold.
- **Export**: JSON or CSV export for loading into databases, spreadsheets, or BI tools.
- **Dashboard**: HTML report generation from ledger data.
