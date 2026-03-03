# OpenClaw + Tokenomics Integration Examples

This directory contains example configurations for integrating OpenClaw with Tokenomics.

## What's Included

### Configuration Files

- **tokenomics-config.yaml** - Tokenomics server config with OpenClaw support
- **policies/** - Example policies for different OpenClaw deployments
  - `slack-bot.json` - Slack bot policy with budgets and security rules
  - `discord-bot.json` - Discord bot policy
  - `personal-assistant.json` - Personal assistant policy (mobile/desktop)
  - `enterprise-fleet.json` - Enterprise multi-agent policy with team budgets
- **agents/** - Example OpenClaw agent configurations
  - `slack-config.json` - OpenClaw Slack bot pointing to tokenomics
  - `discord-config.json` - OpenClaw Discord bot pointing to tokenomics
  - `personal-config.json` - OpenClaw personal assistant (macOS/iOS/Android)

### Setup & Testing

- **.env.example** - Environment variables template
- **docker-compose.yml** - Full stack: tokenomics + webhook collector + monitoring
- **integration-test.sh** - Automated integration test script

## Quick Start

### 1. Setup Environment

```bash
cp .env.example .env
# Edit .env and add your API keys:
# - OPENAI_PAT (your OpenAI API key)
# - ANTHROPIC_PAT (your Anthropic API key)
# - TOKENOMICS_HASH_KEY (32-char hex for token hashing)
# - TOKENOMICS_ENCRYPTION_KEY (32-char hex for encryption)
```

### 2. Start Tokenomics + Webhook Collector

```bash
# Option A: Using Docker Compose (recommended)
docker-compose up -d

# Option B: Manual setup
export $(cat .env | xargs)
tokenomics serve --dir ~/.tokenomics
```

Verify tokenomics is running:

```bash
curl http://localhost:8080/health
# Expected: {"status":"ok"}
```

### 3. Create Tokens for Each Agent

Create a Slack bot token:

```bash
../../bin/tokenomics token create \
  --policy @policies/slack-bot.json \
  --expires 1y
# Output includes:
#   - Token: tkn_...
#   - Hash:  <sha256 hash>
```

Store the raw token (`tkn_...`) for use in OpenClaw config.

Create a Discord bot token:

```bash
../../bin/tokenomics token create \
  --policy @policies/discord-bot.json \
  --expires 1y
# Output: Token hash abc123def...
```

### 4. Configure OpenClaw Agents

Edit `agents/slack-config.json`:

```json
{
  "llm": {
    "api_url": "http://localhost:8080",
    "api_key": "tkn_..."  // Raw token from step 3
  }
}
```

### 5. Run Integration Tests

```bash
./integration-test.sh
```

This will:
- Create test tokens for each agent type
- Verify budget enforcement
- Test jailbreak detection
- Test PII masking
- Verify webhook events are received
- Generate a test report

## Architecture Diagram

```
┌─────────────────────────────────────────────────────────┐
│         OpenClaw Agents (distributed)                   │
│  - Slack Bot (agents/slack-config.json)                 │
│  - Discord Bot (agents/discord-config.json)             │
│  - Personal Assistant (agents/personal-config.json)     │
└──────────────┬──────────────────────────────────────────┘
               │ HTTP requests to /v1/chat/completions
               │ Token: Bearer {token-hash}
               ▼
┌─────────────────────────────────────────────────────────┐
│   Tokenomics Reverse Proxy (tokenomics-config.yaml)    │
│                                                         │
│  1. Validate wrapper token                             │
│  2. Apply policy rules                                 │
│     - Check budget (daily/monthly)                     │
│     - Apply PII masking                                │
│     - Detect jailbreak attempts                        │
│  3. Route to provider                                  │
│  4. Track usage in ledger                              │
│  5. Send webhook events                                │
└────────────────┬────────────────────────────────────────┘
                 │
    ┌────────────┼────────────┐
    ▼            ▼            ▼
 OpenAI     Anthropic     Azure OpenAI

    │
    └──────────────────────────────────────────┐
                                               ▼
                                   ┌─────────────────────┐
                                   │ Webhook Collector   │
                                   │ (localhost:9090)    │
                                   │                     │
                                   │ Receives events:    │
                                   │ - budget.alert      │
                                   │ - security.pii      │
                                   │ - rate.exceeded     │
                                   └─────────────────────┘
```

## File Structure

```
examples/openclaw/
├── README.md                          # This file
├── .env.example                       # Environment variables template
├── tokenomics-config.yaml             # Tokenomics server config
├── docker-compose.yml                 # Full stack setup
├── integration-test.sh                # Automated tests
│
├── policies/                          # Token policies
│   ├── slack-bot.json                # Slack agent policy
│   ├── discord-bot.json               # Discord agent policy
│   ├── personal-assistant.json        # Personal assistant policy
│   └── enterprise-fleet.json          # Multi-team policy
│
└── agents/                            # OpenClaw agent configs
    ├── slack-config.json              # Slack bot pointing to tokenomics
    ├── discord-config.json            # Discord bot pointing to tokenomics
    └── personal-config.json           # Personal assistant config
```

## Testing & Validation

### Run All Tests

```bash
./integration-test.sh --full
```

This runs:
1. **Token creation test** - Creates test tokens for each policy
2. **Budget enforcement test** - Verifies overspending is blocked
3. **PII detection test** - Confirms PII is masked in requests/responses
4. **Jailbreak detection test** - Verifies jailbreak attempts are caught
5. **Webhook delivery test** - Confirms events are sent to webhook receiver
6. **Performance test** - Checks latency overhead
7. **Recovery test** - Verifies proxy recovers from provider failures

### View Test Results

```bash
tail -f ~/.tokenomics/test-results.json
```

## Integration Test Success Criteria

For each OpenClaw deployment, verify:

- [ ] **Token Creation**: Agent token created with policy successfully
- [ ] **Authentication**: OpenClaw agent can authenticate to tokenomics
- [ ] **Routing**: Request routed to correct provider
- [ ] **Budget Enforcement**: Request denied when budget exceeded
- [ ] **Security Rules**: PII masked, jailbreak blocked
- [ ] **Webhook Events**: Events received by webhook collector
- [ ] **Usage Tracking**: Session recorded in ledger
- [ ] **Performance**: Proxy latency < 500ms (no slow provider)

## Troubleshooting

### Tokenomics won't start

```bash
# Check if port 8080 is in use
lsof -i :8080

# Kill conflicting process
kill -9 <PID>

# Check logs
tail -f ~/.tokenomics/tokenomics.log
```

### OpenClaw can't connect to tokenomics

```bash
# Verify tokenomics is running
curl http://localhost:8080/health

# Check firewall (if using remote tokenomics)
curl -v https://tokenomics.example.com/health

# Verify token record by hash (from `token create` output)
../../bin/tokenomics token get --hash <token-hash>
```

### Webhook not receiving events

```bash
# Start webhook collector in foreground
go run ../../examples/webhook-collector/main.go -secret my-webhook-secret

# Check tokenomics config has webhook endpoint
grep -A5 "webhooks:" tokenomics-config.yaml
```

### Budget not enforcing

```bash
# Check policy has rate_limit section
cat policies/slack-bot.json | grep -A10 "rate_limit"

# Verify token is using correct policy
../../bin/tokenomics token get --hash <token-hash> | grep -A20 "\"policy\""

# Check session usage files
ls ~/.tokenomics/sessions/
```

## Production Deployment

For production:

1. **Use absolute paths** for all config files
2. **Store .env securely** - Use secrets manager (Vault, Sealed Secrets, etc.)
3. **Enable TLS** - Set `tls.enabled: true` in tokenomics-config.yaml
4. **Set up monitoring** - Wire webhook events to your alerting system
5. **Enable high-availability** - Run multiple tokenomics instances with shared Redis backend
6. **Backup ledger data** - `.tokenomics/tokenomics.db` contains all token data

See `docs/CONFIGURATION.md`, `docs/POLICIES.md`, and `docs/EVENTS.md` for production settings.

## Examples by Use Case

### Single Team, Multiple Bots

Use `policies/slack-bot.json` + `policies/discord-bot.json` for a team running multiple agents.

```bash
./integration-test.sh --team-mode
```

### Enterprise with Multiple Teams

Use `policies/enterprise-fleet.json` for cost center tracking and per-team budgets.

```bash
./integration-test.sh --enterprise-mode
```

### Personal Assistant Setup

Use `policies/personal-assistant.json` for a single-user mobile/desktop deployment.

```bash
./integration-test.sh --personal-mode
```

## Performance Expectations

With default settings (1000 req/min rate limit):

| Metric | Expected | Max |
|--------|----------|-----|
| Latency (p50) | 50ms | 100ms |
| Latency (p99) | 200ms | 500ms |
| Throughput | 900-1000 req/min | Depends on provider |
| Memory per agent | ~5MB | ~10MB |
| CPU per 100 agents | ~1 core | ~2 cores |

## References

- Main integration guide: `docs/OPENCLAW_INTEGRATION.md`
- Policy reference: `docs/POLICIES.md`
- Configuration reference: `docs/CONFIGURATION.md`
- Webhook events: `docs/EVENTS.md`
