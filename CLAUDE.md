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
- Policies are JSON, stored AES-256-GCM encrypted in BoltDB
- Rules use object format: `{"type":"regex|keyword|pii", "action":"fail|warn|log|mask", "scope":"input|output|both"}`
- Event emitter uses `Emitter` interface; pass `nil` for no-op in tests

## Documentation

Update docs when adding features. Keep docs concise and scannable. Reference files:
- `docs/CONFIGURATION.md` for config fields
- `docs/POLICIES.md` for policy schema and rules
- `docs/EVENTS.md` for webhook event types
- `docs/TOKEN_MANAGEMENT.md` for CLI token commands
- `docs/AGENT_INTEGRATION.md` for init command usage
- `README.md` features table for new capabilities

## Writing Style

- No em dashes. Use periods or commas instead.
- Keep prose short. Tables over paragraphs where possible.
- No emojis unless explicitly asked.
