# Distributed Team Example

Run a central Tokenomics proxy for a team with role-based token policies, multi-provider routing, and centralized token management.

## Architecture

```
                    ┌──────────────────────────────────┐
                    │   Central Config Server (:9090)   │
                    │   ./tokenomics remote             │
                    │   - Token database (BoltDB)       │
                    │   - Serves tokens via REST API    │
                    │   - Pushes token events via       │
                    │     webhook to proxy instances    │
                    └──────────┬──────────┬────────────┘
                     REST sync │          │ webhook push
                    ┌──────────┴──────────┴────────────┐
                    │   Proxy Server (:8443 / :8080)    │
                    │   ./tokenomics serve              │
                    │   - Webhook receiver for instant  │
                    │     token sync on push events     │
                    │   - Poll fallback every 5 min     │
                    │   - Routes to providers           │
                    │   - Enforces policies per token   │
                    └──────────────┬───────────────────┘
                                   │
              ┌────────────────────┼────────────────────┐
              │                    │                     │
      ┌───────────────┐  ┌────────────────┐  ┌──────────────────┐
      │  OpenAI       │  │  Anthropic     │  │  Groq / Mistral  │
      │  gpt-4o, o3   │  │  claude-*      │  │  llama, mistral  │
      └───────────────┘  └────────────────┘  └──────────────────┘
```

Team members connect to the proxy with their assigned tokens. Each token carries its own policy (budget, model access, rate limits, content rules).

Token changes propagate instantly via webhook push. When you create, update, or delete a token on the central server, it sends a `token.*` event to each proxy's webhook receiver, triggering an immediate sync.

## Files

| File | Purpose |
|------|---------|
| `central-config.yaml` | Config for the central config server (token management) |
| `proxy-config.yaml` | Config for the proxy instance (routes requests to LLMs) |
| `providers.yaml` | Multi-provider definitions (OpenAI, Anthropic, Groq, Mistral) |
| `.env.example` | Required environment variables |
| `policies/lead-engineer.json` | Full access, generous limits, all providers |
| `policies/developer.json` | Standard access, moderate limits, main providers |
| `policies/contractor.json` | Restricted access, tight budget, PII masking |
| `policies/ci-pipeline.json` | Automation token, single model, no interactive use |
| `setup.sh` | Script to initialize the database, create tokens, and start services |

## Quick Start

### 1. Set environment variables

```bash
cp examples/distributed-team/.env.example .env

# Edit with your real values
vim .env
source .env
```

### 2. Build

```bash
make build
```

### 3. Initialize the token database

```bash
# Copy configs
cp examples/distributed-team/central-config.yaml config.yaml
cp examples/distributed-team/providers.yaml providers.yaml

# Create role-based tokens
./bin/tokenomics token create \
  --policy "$(cat examples/distributed-team/policies/lead-engineer.json)" \
  --expires 1y
# Save the printed token for your lead engineer

./bin/tokenomics token create \
  --policy "$(cat examples/distributed-team/policies/developer.json)" \
  --expires 90d
# Save for each developer (create one per person)

./bin/tokenomics token create \
  --policy "$(cat examples/distributed-team/policies/contractor.json)" \
  --expires 30d
# Save for each contractor

./bin/tokenomics token create \
  --policy "$(cat examples/distributed-team/policies/ci-pipeline.json)" \
  --expires 1y
# Save for CI/CD systems
```

### 4. Start the central config server

```bash
# Terminal 1: Serves tokens to proxy instances
./bin/tokenomics remote --addr :9090 --api-key "$TOKENOMICS_REMOTE_KEY"
```

### 5. Start the proxy

```bash
# Terminal 2: Copy proxy config and start
cp examples/distributed-team/proxy-config.yaml config.yaml

./bin/tokenomics serve
```

### 6. Connect a team member

```bash
# On the developer's machine
eval $(./bin/tokenomics init --token tkn_<their-token> --host proxy.internal --port 8443 --insecure)

# Now use any OpenAI-compatible client
curl $OPENAI_BASE_URL/chat/completions \
  -H "Authorization: Bearer $OPENAI_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model":"gpt-4o","messages":[{"role":"user","content":"Hello"}]}'
```

Or use the setup script to automate everything:

```bash
chmod +x examples/distributed-team/setup.sh
./examples/distributed-team/setup.sh
```

## Role Comparison

| Role | Models | Budget | Rate Limit | Rules | Retry | Memory |
|------|--------|--------|------------|-------|-------|--------|
| Lead Engineer | All providers | 1M tokens | 120/min, 500k/hr | PII warn | 3 retries + fallback | Per-token files |
| Developer | OpenAI + Anthropic | 200k tokens | 60/min, 200k/hr | PII mask | 2 retries | Per-token files |
| Contractor | OpenAI only | 50k tokens | 20/min, 50k/hr | PII mask, prompt guard | None | Single file |
| CI Pipeline | gpt-4o-mini only | 500k tokens | 30/min | None | 2 retries + fallback | Disabled |

## Managing Tokens

All token management happens on the machine running the central config server.

```bash
# List all tokens
./bin/tokenomics token list

# Check a specific token
./bin/tokenomics token get --hash <hash-prefix>

# Revoke a contractor token immediately
./bin/tokenomics token delete --hash <hash-prefix>

# Extend a developer token
./bin/tokenomics token update --hash <hash-prefix> --expires 180d

# Reduce a budget
./bin/tokenomics token update --hash <hash-prefix> \
  --policy '{"max_tokens": 100000}'
```

Changes propagate to proxy instances instantly via webhook push. A poll fallback runs every 5 minutes as a safety net.

## Token Sync: Push + Poll

The proxy uses two mechanisms to stay in sync with the central server:

| Method | Latency | How it works |
|--------|---------|--------------|
| **Webhook push** | Sub-second | Central server sends `token.*` events to proxy's `/v1/webhook` |
| **Poll fallback** | Up to 5 min | Proxy fetches all tokens from central server on a timer |

Both are enabled by default in `proxy-config.yaml`. The webhook receiver authenticates requests using the same shared secret and HMAC signature used by outbound webhooks.

```yaml
# On the proxy (proxy-config.yaml)
remote:
  url: http://config-server:9090
  api_key: change-me-to-match-remote-server
  sync: 300                        # Poll fallback every 5 minutes
  webhook:
    enabled: true                  # Accept push events from central server
    secret: change-me-webhook-secret
    signing_key: change-me-signing-key

# On the central server (central-config.yaml)
events:
  webhooks:
    - url: https://proxy.internal:8443/v1/webhook
      secret: change-me-webhook-secret
      signing_key: change-me-signing-key
      events: ["token.*"]          # Only push token lifecycle events
```

## Scaling to Multiple Proxies

Deploy additional proxy instances pointing to the same central config server. Add a webhook entry on the central server for each proxy.

```
Central Config Server (:9090)
        │ webhook push
   ┌────┼────┐
   │    │    │
   v    v    v
Proxy  Proxy  Proxy
(US)   (EU)   (APAC)
```

Each proxy uses the same `proxy-config.yaml` with `remote.url` pointing to the central server. On the central server, add one outbound webhook per proxy:

```yaml
# central-config.yaml
events:
  webhooks:
    - url: https://proxy-us.internal:8443/v1/webhook
      secret: change-me-webhook-secret
      signing_key: change-me-signing-key
      events: ["token.*"]
    - url: https://proxy-eu.internal:8443/v1/webhook
      secret: change-me-webhook-secret
      signing_key: change-me-signing-key
      events: ["token.*"]
    - url: https://proxy-apac.internal:8443/v1/webhook
      secret: change-me-webhook-secret
      signing_key: change-me-signing-key
      events: ["token.*"]
```

Session tracking (usage counters) is local to each proxy unless you enable Redis:

```yaml
session:
  backend: redis
  redis:
    addr: redis.internal:6379
```

## Webhook Alerts

The proxy config includes webhook targets for monitoring. Configure a Slack incoming webhook or use the included webhook collector for debugging:

```bash
# Debug mode
go run examples/webhook-collector/main.go \
  -secret "$TOKENOMICS_WEBHOOK_SECRET" \
  -signing-key "$TOKENOMICS_SIGNING_KEY"
```

Events emitted: `token.created`, `token.deleted`, `budget.exceeded`, `rate.exceeded`, `rule.violation`, `request.completed`.
