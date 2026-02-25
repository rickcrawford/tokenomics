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
    "error_count": 2
  },
  "by_model": {
    "claude-sonnet-4-20250514": {
      "request_count": 30,
      "input_tokens": 80000,
      "output_tokens": 60000,
      "total_tokens": 140000
    },
    "gpt-4o": {
      "request_count": 15,
      "input_tokens": 45000,
      "output_tokens": 29000,
      "total_tokens": 74000
    }
  },
  "requests": [
    {
      "timestamp": "2026-02-25T10:30:05Z",
      "model": "claude-sonnet-4-20250514",
      "input_tokens": 1500,
      "output_tokens": 800,
      "duration_ms": 3200,
      "status_code": 200,
      "stream": true,
      "upstream_id": "msg_abc123"
    }
  ],
  "metadata": {
    "team": "platform",
    "cost_center": "engineering"
  }
}
```

**Design decisions:**

- `requests` array captures every proxied call with enough detail for debugging without storing full bodies.
- `git.commit_start` is HEAD when the proxy starts. `git.commit_end` is HEAD when it stops. This brackets what code was being worked on.
- `by_model` rollup enables quick per-model token breakdowns.
- `metadata` is carried from the token's policy, enabling team/project tagging.
- No pricing or cost fields. Downstream analytics can join model + token counts with current pricing at query time.

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
func (l *Ledger) Close() error                    // snapshot git end, compute costs, write JSON
```

**Key behaviors:**

- `Open()` creates `.tokenomics/sessions/` and `.tokenomics/memory/` directories if missing. Captures `git.commit_start` and `git.branch`.
- `RecordRequest()` is called from the proxy after each completed request. Thread-safe. Appends to an in-memory list.
- `Close()` captures `git.commit_end`, computes `by_model` rollups, writes the session JSON file, and closes the memory writer.
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
        Timestamp:    time.Now().UTC(),
        Model:        model,
        InputTokens:  inputTokens,
        OutputTokens: outputTokens,
        DurationMs:   elapsed.Milliseconds(),
        StatusCode:   statusCode,
        Stream:       isStream,
        UpstreamID:   upstreamID,
        TokenHash:    safePrefix(tokenHash, 16),
    })
}
```

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
            |                           |
    +-------v--------+          +-------v--------+
    | sessions/*.json |          | memory/*.md    |
    +-----------------+          +----------------+
            |
      Close() -> write summary with by_model rollups


    CLI reads .tokenomics/ directly:

    +------------------+
    | ledger summary   |----> reads sessions/*.json, aggregates tokens
    | ledger sessions  |----> lists sessions/*.json
    | ledger show <id> |----> reads one session file
    +------------------+

    Downstream analytics (database, dashboard, etc.)
    joins session data with current model pricing at query time.
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
| `internal/proxy/handler_chat.go` | Modify | Call `ledger.RecordRequest()` and `RecordMemory()` |
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
