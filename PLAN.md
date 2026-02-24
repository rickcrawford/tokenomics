# Tokenomics — OpenAI Reverse Proxy Implementation Plan

## Decisions

| Question | Decision |
|----------|----------|
| API Endpoints | Full `/v1/*` passthrough, policy enforcement on chat completions |
| Storage Driver | BoltDB via `go.etcd.io/bbolt` (pure Go, no CGO) |
| Streaming | SSE streaming from day one |
| Init Targets | OpenAI, Anthropic, Azure, Gemini presets |
| Token Format | UUID-based: `tkn_<uuid-v4>` |
| TLS | Auto-gen CA + server cert for trust installation |
| Upstream URL | Per-policy configurable (policy override > global default) |
| Router | `go-chi/chi` with common middleware (RequestID, RealIP, Recoverer, Timeout, Heartbeat) |
| Logging | Structured JSON request logging |
| Stats | `/stats` endpoint with aggregation by model/key + per-token session tracking |

## Overview

A Go-based reverse proxy that sits in front of OpenAI (and compatible) APIs. It issues "wrapper tokens" that map to policies and real provider credentials (referenced by env var name, never stored as secrets). The proxy intercepts requests, enforces policies (model restrictions, token budgets, prompt rules), injects system prompts, and forwards requests using the real provider key.

---

## Architecture

```
                         tokenomics init
                         ┌─────────────────────────┐
                         │ Sets env vars / config:  │
                         │  OPENAI_API_KEY=tkn_xxx  │
                         │  OPENAI_BASE_URL=proxy   │
                         └────────────┬────────────┘
                                      │
                                      ▼
                         Agent CLI (aider, claude, etc.)
                         Uses wrapper-token as Bearer
                                      │
                                      ▼
┌──────────────────────────────────────────────────────┐
│                 Tokenomics Proxy (chi)                │
├──────────────────────────────────────────────────────┤
│   Middleware: RequestID, RealIP, Recoverer, Timeout  │
├──────────────────────────────────────────────────────┤
│   /health  → health check                           │
│   /stats   → usage stats by model/key + token       │
│   /ping    → heartbeat                              │
│   /*       → reverse proxy handler                  │
├──────────────────────────────────────────────────────┤
│   TLS Termination        (auto-gen CA + server cert) │
├──────────────────────────────────────────────────────┤
│   Reverse Proxy Handler                              │
│   1. Extract bearer (wrapper-token)                  │
│   2. Hash token (HMAC-SHA256)                        │
│   3. Lookup policy (in-memory from BoltDB)           │
│   4. For /chat/completions: enforce policy           │
│      a. Model check (exact / regex)                  │
│      b. Rules check (regex on user content)          │
│      c. Inject policy prompts                        │
│      d. Count input tokens (tiktoken)                │
│      e. Check session budget                         │
│   5. Resolve base key from env var                   │
│   6. Forward to upstream (per-policy or global)      │
│   7. Stream SSE or buffer response                   │
│   8. Count output tokens, update session             │
│   9. Log request as JSON, update stats               │
│   For all other /v1/*: passthrough with key swap     │
├──────────────────────────────────────────────────────┤
│   Session Manager        (in-memory / Redis)         │
│   Usage Stats            (in-memory, per model/key)  │
│   Session Tracker        (in-memory, per token)      │
├──────────────────────────────────────────────────────┤
│   Token Store (BoltDB → memory, file-watch reload)   │
│   hash → policy JSON                                 │
└──────────────────────────────────────────────────────┘
                                      │
                                      ▼
                         Upstream Provider (configurable per policy)
                         (Bearer: real API key from env)
```

---

## Directory Structure

```
tokenomics/
├── cmd/
│   └── tokenomics/
│       └── main.go
├── internal/
│   ├── cli/
│   │   ├── root.go          # cobra root command
│   │   ├── serve.go         # serve command (chi router + middleware)
│   │   ├── token.go         # token create/delete/list commands
│   │   └── init.go          # init command — configure agent CLI to use proxy
│   ├── config/
│   │   └── config.go        # viper config struct + loading
│   ├── proxy/
│   │   ├── handler.go       # main HTTP handler (proxy + policy enforcement)
│   │   ├── logging.go       # structured JSON request logging
│   │   └── stats.go         # usage stats by model/key + per-token sessions
│   ├── policy/
│   │   ├── policy.go        # policy struct + validation
│   │   └── engine.go        # model check, rules check, prompt injection
│   ├── store/
│   │   ├── bolt.go          # BoltDB operations + in-memory cache
│   │   └── store.go         # store interface
│   ├── session/
│   │   ├── memory.go        # in-memory session store
│   │   ├── redis.go         # redis session store
│   │   └── session.go       # session store interface
│   ├── tls/
│   │   └── certgen.go       # auto-generate CA + server cert
│   └── tokencount/
│       └── counter.go       # tiktoken wrapper
├── config.yaml               # default config
├── .env.example              # example env vars
├── .gitignore
├── go.mod
├── go.sum
├── Makefile
└── README.md
```

---

## Dependencies

| Package | Purpose |
|---------|---------|
| `github.com/spf13/cobra` | CLI framework |
| `github.com/spf13/viper` | Configuration |
| `github.com/go-chi/chi/v5` | HTTP router + middleware |
| `github.com/pkoukk/tiktoken-go` | Token counting (OpenAI's tiktoken port) |
| `go.etcd.io/bbolt` | Embedded key-value store (pure Go) |
| `github.com/google/uuid` | UUID token generation |
| `github.com/redis/go-redis/v9` | Redis client (optional) |
| `crypto/x509`, `crypto/ecdsa` | Cert generation (stdlib) |
