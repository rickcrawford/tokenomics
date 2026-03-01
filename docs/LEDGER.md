# Session Ledger

The session ledger records token usage, provider metadata, and conversation memory under your configured `dir` (default `~/.tokenomics`).

## Enable

```yaml
# config.yaml
dir: "~/.tokenomics"    # base directory (default)
ledger:
  enabled: true         # enable session ledger (default)
  memory: true          # record conversation content (default)
  event_ledger: false   # structured communication events (default off, dual-write phase)
```

Or via environment variables:

```bash
export TOKENOMICS_LEDGER_ENABLED=true
export TOKENOMICS_LEDGER_MEMORY=true
export TOKENOMICS_LEDGER_EVENT_LEDGER=true
```

Note: The ledger directory is determined by the `dir` setting and cannot be overridden. Session files and memory logs are stored under `{dir}/sessions/` and `{dir}/memory/` respectively.

## Folder Structure

```
~/.tokenomics/              # default base directory
├── sessions/
│   └── 2026-02-25_a1b2c3d4.json     # one file per proxy session
└── memory/
    └── 2026-02-25_a1b2c3d4.md       # conversation log (if enabled)
```

Session files use `<YYYY-MM-DD>_<session-id>.json` naming. Session ID is an 8-char hex generated at proxy startup.

Memory files live in a separate directory so teams can `.gitignore` them independently if conversation content is sensitive.

## How It Works

1. Proxy starts, ledger opens a new session, snapshots git branch and HEAD commit
2. Every proxied request is recorded with token counts, provider metadata, and timing
3. Raw request/response (with content-type and safe headers) is optionally written to a memory markdown file
4. Optional communication events (`request.received`, `response.*`) are captured when `ledger.event_ledger: true`
5. On shutdown, the ledger computes rollups and writes the session JSON
6. Optionally commit session artifacts if your workflow tracks usage in git

## Session JSON Format

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
  "by_model": { ... },
  "by_provider": { ... },
  "by_token": { ... },
  "requests": [ ... ],
  "communication_events": [ ... ]
}
```

### Git Context

| Field | Description |
|-------|-------------|
| `branch` | Current branch when proxy started |
| `commit_start` | HEAD commit (short) when proxy started |
| `commit_end` | HEAD commit (short) when proxy stopped |
| `repo_root` | Absolute path to the git repo root |

Empty strings if not in a git repo.

### Session Totals

| Field | Description |
|-------|-------------|
| `request_count` | Total proxied requests |
| `input_tokens` | Total input tokens (tiktoken count) |
| `output_tokens` | Total output tokens |
| `total_tokens` | input + output |
| `cached_input_tokens` | Tokens served from cache (50-90% cheaper) |
| `cache_creation_tokens` | Tokens written to cache (Anthropic, 1.25x rate) |
| `reasoning_tokens` | Reasoning tokens (o1/o3/o4, higher rate) |
| `error_count` | Requests with status >= 400 |
| `retry_count` | Total retry attempts across all requests |
| `rule_violation_count` | Content rule "fail" actions |
| `rate_limit_count` | 429 responses from upstream |

### Rollup Dimensions

Three rollup maps aggregate tokens for different analysis needs.

**`by_model`** groups by the requested model name. Each entry has request_count, input/output/total tokens, and cached/reasoning breakdowns.

**`by_provider`** groups by the provider name from the policy. Useful for understanding spend across API keys.

**`by_token`** groups by wrapper token hash. Includes `models_used` (list of models that token accessed) and `first_seen`/`last_seen` timestamps.

### Per-Request Entry

| Field | Description |
|-------|-------------|
| `timestamp` | Request time (RFC3339) |
| `token_hash` | HMAC-SHA256 of the wrapper token |
| `model` | Requested model |
| `provider` | Provider name from policy |
| `input_tokens` | Input token count |
| `output_tokens` | Output token count |
| `duration_ms` | Request latency |
| `status_code` | Upstream HTTP status |
| `stream` | Whether streaming was used |
| `error` | Error message (if any) |
| `upstream_id` | Provider's completion ID (chatcmpl-*, msg_*) |
| `upstream_request_id` | Provider's request correlation ID |
| `retry_count` | Number of retry attempts |
| `fallback_model` | Model used after fallback |
| `rule_matches` | Content rule matches (warn, log, mask) |
| `metadata` | Policy metadata tags (team, project, cost_center) |
| `provider_meta` | Provider response metadata (see below) |

### Communication Events (Optional)

When `ledger.event_ledger` is enabled, `communication_events` captures bounded request/response communication details for debugging and replay.

| Field | Description |
|-------|-------------|
| `timestamp` | Event time (RFC3339) |
| `type` | `request.received`, `response.started`, `response.chunk`, `response.body`, `response.completed`, `response.error` |
| `token_hash` | Token hash prefix |
| `model` | Model used for the request/attempt |
| `provider` | Provider name |
| `method` | HTTP method (request events) |
| `path` | HTTP path (request events) |
| `status_code` | Upstream status code when known |
| `content_type` | Request/response content type |
| `headers` | Sanitized headers (auth and API key headers removed) |
| `body` | Bounded payload sample, or `[binary]` marker |
| `body_bytes` | Original payload size before truncation |
| `chunk_index` | 1-based chunk index for `response.chunk` |
| `stream` | Whether request used streaming |
| `retry_count` | Retry count at event emission time |
| `error` | Error text for `response.error` |

### Provider Metadata

Normalized fields extracted from each provider's response headers and body. These matter for cost correlation because different token types have different billing rates.

| Field | Description |
|-------|-------------|
| `cached_input_tokens` | Tokens served from provider cache |
| `cache_creation_tokens` | Tokens written to cache (Anthropic) |
| `reasoning_tokens` | Reasoning tokens (OpenAI o1/o3/o4) |
| `actual_model` | Model actually served (may differ from requested) |
| `finish_reason` | Why generation stopped (stop, length, content_filter) |
| `rate_limit_remaining_requests` | Provider's remaining request quota |
| `rate_limit_remaining_tokens` | Provider's remaining token quota |
| `rate_limit_reset` | When the rate limit window resets |

**Provider header mappings:**

| Provider | Remaining Requests | Remaining Tokens | Reset |
|----------|-------------------|------------------|-------|
| OpenAI | `x-ratelimit-remaining-requests` | `x-ratelimit-remaining-tokens` | `x-ratelimit-reset-requests` |
| Anthropic | `anthropic-ratelimit-requests-remaining` | `anthropic-ratelimit-tokens-remaining` | `anthropic-ratelimit-tokens-reset` |
| Azure | `x-ratelimit-remaining-requests` | `x-ratelimit-remaining-tokens` | `x-ratelimit-reset-tokens` |
| Gemini | (not exposed) | (not exposed) | `retry-after` (429 only) |
| Mistral | Same as OpenAI | Same as OpenAI | Same as OpenAI |

**Token detail extraction:**

| Provider | Cached Input | Cache Write | Reasoning |
|----------|-------------|-------------|-----------|
| OpenAI | `usage.prompt_tokens_details.cached_tokens` | n/a | `usage.completion_tokens_details.reasoning_tokens` |
| Anthropic | `usage.cache_read_input_tokens` | `usage.cache_creation_input_tokens` | n/a |
| Gemini | `usageMetadata.cachedContentTokenCount` | n/a | n/a |
| Azure | Same as OpenAI | Same as OpenAI | Same as OpenAI |
| Mistral | n/a | n/a | n/a |

## CLI Commands

### `tokenomics ledger summary`

Show aggregated token usage across all sessions.

```bash
tokenomics ledger summary
tokenomics ledger summary --json
tokenomics ledger summary --dir /path/to/.tokenomics
```

Example output:

```
Sessions: 9

Totals:
  Requests     199
  Input tokens   695000
  Output tokens  438000
  Total tokens   1133000
  Cached input   120000
  Reasoning      35000
  Errors         4
  Retries        7

By Model:
  NAME                       REQUESTS  INPUT   OUTPUT  TOTAL
  claude-sonnet-4-20250514   120       400000  250000  650000
  gpt-4o                     79        295000  188000  483000

By Provider:
  NAME                REQUESTS  INPUT   OUTPUT  TOTAL
  ANTHROPIC_PAT   120       400000  250000  650000
  OPENAI_PAT      79        295000  188000  483000
```

### `tokenomics ledger sessions`

List all recorded sessions.

```bash
tokenomics ledger sessions
tokenomics ledger sessions --json
```

Example output:

```
SESSION     STARTED              DURATION  REQUESTS  TOKENS   BRANCH
a1b2c3d4    2026-02-25T10:30:00  1h15m0s   45        214000   feature/add-auth
e5f6a7b8    2026-02-25T14:00:00  32m15s    22        98000    bugfix/login
```

### `tokenomics ledger show <session-id>`

Show details for a specific session. Supports prefix matching.

```bash
tokenomics ledger show a1b2c3d4
tokenomics ledger show a1b2 --json
```

### Flags

| Flag | Description |
|------|-------------|
| `--dir` | Ledger directory (default: from config or `.tokenomics`) |
| `--json` | Output as JSON |

## Memory Files

When `ledger.memory: true`, request and response data are written to `memory/<date>_<session_id>.md`. Each entry records raw request or response with no JSON transformation: Content-Type, safe headers (Authorization and API-key headers are stripped), and body. For binary or non-UTF-8 bodies, the body is recorded as `[binary, N bytes]` so you can inspect content-type and headers and decide later what to extract.

When `ledger.event_ledger: true`, event entries are also written to memory with `Event: <type>` blocks. This is additive in the dual-write rollout phase.

Example shape:

```markdown
## 2026-02-25T10:30:05Z | a1b2c3d4 | request | claude-sonnet-4-20250514

Content-Type: application/json
Request-Headers:
  Accept: application/json
  Content-Type: application/json

Body:
{"model":"claude-sonnet-4","messages":[...]}

---

## 2026-02-25T10:30:08Z | a1b2c3d4 | response | claude-sonnet-4-20250514

Content-Type: application/json
Response-Headers:
  Content-Type: application/json
  X-Request-Id: ...

Body:
{"id":"msg_...","content":[...],"usage":{...}}

---
```

## Git Workflow

1. Enable ledger in config or via `TOKENOMICS_LEDGER_ENABLED=true`
2. Run `tokenomics serve` (or via `tokenomics run`/`tokenomics init`)
3. Proxy records every request to the in-memory session
4. On shutdown, session summary is written to `.tokenomics/sessions/`
5. Commit `.tokenomics/` alongside your code changes
6. Use `tokenomics ledger summary` to view aggregated usage

## Gitignore

To commit session data but exclude conversation content:

```gitignore
# Keep session JSON, ignore memory content
.tokenomics/memory/
```

To exclude everything:

```gitignore
.tokenomics/
```

## Edge Cases

| Scenario | Handling |
|----------|----------|
| Not a git repo | Git fields are empty strings, session still recorded |
| Proxy crashes (no graceful shutdown) | Session data lost for that run |
| `.tokenomics/` dir is read-only | Log warning, continue without ledger |
| Concurrent proxy instances | Each gets a unique session ID, no conflicts |
| No pricing stored | Intentional. Token counts are raw facts. Cost is a downstream concern at query time |
