# Tokenomics

OpenAI-compatible reverse proxy with token-scoped policies for budgets, rate limits, model access, content rules, and multi-provider routing.

## Build & Test

```bash
make build          # compile to ./bin/tokenomics
make test           # go test ./...
make lint           # golangci-lint run ./...
make tidy           # go mod tidy
```

Always run `make test` after adding or modifying features. Fix failures before committing.

## Project Layout

```
cmd/                   CLI commands (Cobra): serve, token, init, remote
internal/
  config/              YAML config loading, provider definitions, logging config
  events/              Event emitter interface, webhook delivery
  policy/              Policy parsing, rules engine, PII detection
  proxy/               HTTP handler, rate limiting, stats, logging
  remote/              Remote config server and client for centralized token sync
  session/             Usage tracking (memory or Redis)
  store/               BoltDB token storage, encryption
  tls/                 Certificate generation
  tokencount/          tiktoken-based token counting
examples/              Provider configs, sample policies, webhook collector
docs/                  Feature documentation
```

## Key Conventions

- Go 1.21+, modules at `github.com/rickcrawford/tokenomics`
- CLI uses Cobra with `cmd/root.go` as the entrypoint
- Config loaded via Viper from `config.yaml` or `$HOME/.tokenomics/config.yaml`
- Env prefix: `TOKENOMICS_` (e.g. `TOKENOMICS_HASH_KEY`)
- `.tokenomics` directory: Always relative to the current working directory where the command runs (e.g. `tokenomics serve` from project root creates `.tokenomics/`). Override with `TOKENOMICS_DIR` env var or `dir:` in config.yaml to use a different location (including absolute paths like `~/.tokenomics`).
- Policies are JSON, stored AES-256-GCM encrypted in BoltDB
- Rules use object format: `{"type":"regex|keyword|pii", "action":"fail|warn|log|mask", "scope":"input|output|both"}`
- Event emitter uses `Emitter` interface; pass `nil` for no-op in tests

## Memory Efficiency Conventions

- **Shared HTTP Client:** Always use a shared `*http.Client` on the `Handler` struct (initialized in `NewHandler()`). Never create `&http.Client{}` inside request handlers — each new client bypasses Go's connection pooling.
- **Body Reads:** Use `io.LimitReader` for all body reads (request and response). Reuse `maxRequestBodySize` (10 MB) and `maxResponseBodySize` (32 MB) constants to prevent unbounded memory allocation.
- **SSE Parsing:** Use `bytes.Buffer` with incremental `ReadBytes('\n')` for O(1)-per-chunk processing instead of string concatenation and `strings.Split`.
- **Content Accumulation:** Cap assistant response content (`contentBuilder.Len() < maxMemoryContentSize`) and user message accumulation (`partsSize < maxMemoryContentSize`). Default cap is 512 KB.
- **Persistent Loggers:** Use `sync.Once` to initialize file handles once (e.g., debug log) instead of opening/closing on every call.
- **File Handles:** Close stale file handles when paths change (e.g., date rollover in `DirMemoryWriter.getFile()`).

## Documentation

Update docs when adding features. Keep docs concise and scannable. Reference files:
- `docs/CONFIGURATION.md` for config fields
- `docs/POLICIES.md` for policy schema and rules
- `docs/EVENTS.md` for webhook event types
- `docs/TOKEN_MANAGEMENT.md` for CLI token commands
- `docs/AGENT_INTEGRATION.md` for init command usage
- `README.md` features table for new capabilities
- `docs/WEB.md` for embedded admin routes and UX
- `docs/ADMIN_UI.md` for admin tabs, policy editor UX, and embedded docs workflow

Documentation update rule:
- For every new feature or behavior change, update the relevant docs in the same change.
- If admin UX or in-app instructions change, update embedded docs content in `cmd/web/admin/assets/docs.json` in the same change.
- If policy behavior changes, update `docs/POLICIES.md`.
- If configuration or CLI behavior changes, update `docs/CONFIGURATION.md` and `README.md` where applicable.

## Writing Style

- No em dashes. Use periods or commas instead.
- Keep prose short. Tables over paragraphs where possible.
- No emojis unless explicitly asked.
