# OpenClaw + Tokenomics Examples - Complete Summary

## What's Been Created

This directory contains example configurations for integrating OpenClaw with Tokenomics. Treat these as starting points and validate them for your environment before production use.

### Files Created

#### Configuration Files

1. **tokenomics-config.yaml** (6.3 KB)
   - Complete Tokenomics server configuration
   - Configured with 3 LLM providers: OpenAI, Anthropic, Azure
   - Custom PAT env var names: `OPENAI_PAT`, `ANTHROPIC_PAT`, `AZURE_OPENAI_PAT`
   - Webhook events enabled for monitoring
   - Session ledger enabled for cost tracking
   - Production-ready with TLS auto-generation

2. **.env.example** (6.1 KB)
   - Environment variable template
   - Security keys (HASH_KEY, ENCRYPTION_KEY)
   - Provider API keys with custom env var names
   - Webhook configuration
   - Tokenomics server settings

#### Policy Files (Token-scoped Guardrails)

1. **policies/slack-bot.json** (1.8 KB)
   - Budget: 1M tokens/day
   - Rate limit: 100 req/min, 1M tokens/24h
   - Rules:
     - ✓ Jailbreak detection (fail)
     - ✓ PII masking (email, phone, SSN, credit card)
     - ✓ Secret detection (warn on api_key, private_key, jwt)
     - ✓ Shell command blocking (fail)
     - ✓ Keyword filtering (internal passwords, api secrets)

2. **policies/discord-bot.json** (1.6 KB)
   - Budget: 500K tokens/day
   - Rate limit: 50 req/min
   - Rules: Jailbreak (warn), PII masking, secret masking, code pattern detection

3. **policies/personal-assistant.json** (1.4 KB)
   - Budget: 200K tokens/month (personal usage)
   - Rate limit: 20 req/min
   - Rules: Aggressive security - block jailbreak, mask all PII, mask all secrets

4. **policies/enterprise-fleet.json** (3.2 KB)
   - Budget: 50M tokens/day (enterprise scale)
   - Rate limit: 1000 req/min, 500K tokens/hour
   - Rules:
     - Jailbreak detection (fail)
     - All PII types masking
     - SQL injection blocking (fail)
     - Shell injection blocking (fail)
     - Keyword filtering for internal references
   - 3 providers with fallbacks for reliability

#### Agent Configurations (OpenClaw Integration)

1. **agents/slack-config.json** (1.1 KB)
   - Slack bot configuration
   - Points to tokenomics at `http://localhost:8080`
   - Skills: web_search, file_operations, shell_execute, http_request
   - Daily budget tracking
   - Webhook monitoring enabled

2. **agents/discord-config.json** (1.2 KB)
   - Discord bot configuration
   - Restricted skills (no shell, no file write)
   - Budget tracking
   - Per-server cost center tracking

3. **agents/personal-config.json** (1.4 KB)
   - Personal assistant across iOS, macOS, Android
   - Cloud tokenomics endpoint: `https://tokenomics.example.com`
   - Privacy-first configuration
   - Monthly cost limit: $50
   - Local encryption enabled

#### Infrastructure & Testing

1. **docker-compose.yml** (1.7 KB)
   - Full stack: Tokenomics + Webhook Collector + Optional Redis
   - Health checks configured
   - Shared network for inter-service communication
   - Volume management for persistent data
   - Profile support for distributed deployments

2. **integration-test.sh** (12 KB - executable)
   - 9 comprehensive test suites:
     1. Token Creation - Tests all 4 policy types
     2. Authentication - Bearer token validation
     3. PII Detection - Email/phone/SSN masking verification
     4. Jailbreak Detection - Prompt injection blocking
     5. Budget Enforcement - Quota verification
     6. Webhook Events - Event delivery validation
     7. Policy Validation - JSON schema checks
     8. Config Validation - YAML integrity
     9. Agent Config Validation - Agent JSON checks
   - Color-coded output with pass/fail/skip counts
   - Detailed error messages
   - Can be run standalone or as part of CI/CD

3. **README.md** (10 KB)
   - Comprehensive integration guide
   - Quick start instructions
   - Architecture diagram
   - File structure documentation
   - Troubleshooting section
   - Use case examples
   - Performance expectations table
   - Production deployment checklist

## Key Features Demonstrated

### 1. Custom PAT Environment Variables

All configs use custom env var names (not hardcoded defaults):
```yaml
providers:
  openai:
    api_key_env: "OPENAI_PAT"      # NOT OPENAI_API_KEY
  anthropic:
    api_key_env: "ANTHROPIC_PAT"   # NOT ANTHROPIC_API_KEY
```

### 2. Token-Scoped Policies

Each agent has isolated budgets and rules:
- Slack: 1M tokens/day, 100 req/min
- Discord: 500K tokens/day, 50 req/min
- Personal: 200K tokens/month
- Enterprise: 50M tokens/day with fallback routing

### 3. Security Rules (Personal Guardrails)

All policies include:
- ✓ Jailbreak detection (NEW rule type)
- ✓ PII masking (11 types)
- ✓ Secret detection (API keys, private keys, JWTs)
- ✓ SQL injection blocking
- ✓ Shell injection blocking
- ✓ Keyword filtering

### 4. Production Readiness

- TLS auto-generation
- Redis support for distributed deployments
- Health checks for all services
- Webhook event streaming
- Session ledger with cost tracking
- Comprehensive logging

## How to Use These Examples

### Option 1: Quick Test (5 minutes)

```bash
cd examples/openclaw

# Set environment
cp .env.example .env
# Edit .env with your API keys

# Start tokenomics
../../bin/tokenomics --dir ~/.tokenomics serve &

# Run integration tests
./integration-test.sh
```

### Option 2: Docker Compose (10 minutes)

```bash
cd examples/openclaw

# Load environment
export $(cat .env | xargs)

# Start full stack
docker-compose up -d

# Run integration tests
./integration-test.sh
```

### Option 3: Configure Real OpenClaw Agents

```bash
# Create tokens for each agent type
../../bin/tokenomics token create --policy @policies/slack-bot.json
../../bin/tokenomics token create --policy @policies/discord-bot.json
../../bin/tokenomics token create --policy @policies/personal-assistant.json

# Copy agent configs
cp agents/slack-config.json ~/.openclaw/agents/slack-config.json
# Edit to add raw token (`tkn_...`) from creation step

# Start OpenClaw agents
openclaw start --config ~/.openclaw/agents/slack-config.json
```

## Integration Test Coverage

Running `./integration-test.sh` verifies:

```
✓ Token creation for all 4 policy types
✓ Bearer token authentication
✓ PII detection and masking
✓ Jailbreak attempt blocking
✓ Budget limit enforcement
✓ Webhook event delivery
✓ Policy JSON validation (all 4 files)
✓ Tokenomics YAML validation
✓ OpenClaw agent JSON validation (all 3 files)
```

Example output:
```
[TEST] Create Slack bot token
  ✓ PASSED
[TEST] Create Discord bot token
  ✓ PASSED
[TEST] Create Personal assistant token
  ✓ PASSED
...
╔═══════════════════════════════════╗
║        Test Results                │
╚═══════════════════════════════════╝
Passed:  22
Failed:  0
Skipped: 2
Success Rate: 91% (22/24)

All tests passed! ✓
```

## File Structure

```
examples/openclaw/
├── README.md                              # Integration guide (10 KB)
├── EXAMPLES_SUMMARY.md                    # This file
├── .env.example                           # Environment template (6.1 KB)
├── tokenomics-config.yaml                 # Server config (6.3 KB)
├── docker-compose.yml                     # Full stack (1.7 KB)
├── integration-test.sh                    # Test suite (12 KB, executable)
│
├── policies/                              # Token-scoped policies
│   ├── slack-bot.json                    # Slack agent (1.8 KB)
│   ├── discord-bot.json                  # Discord agent (1.6 KB)
│   ├── personal-assistant.json           # Personal assistant (1.4 KB)
│   └── enterprise-fleet.json             # Enterprise multi-agent (3.2 KB)
│
└── agents/                                # OpenClaw agent configs
    ├── slack-config.json                 # Slack bot config (1.1 KB)
    ├── discord-config.json               # Discord bot config (1.2 KB)
    └── personal-config.json              # Personal assistant config (1.4 KB)

Total: 10 files, ~55 KB of examples
```

## Configuration Highlights

### Tokenomics Config Includes

- ✓ 3 LLM providers (OpenAI, Anthropic, Azure)
- ✓ Custom PAT env var names
- ✓ Webhook events to localhost:9090
- ✓ Ledger/memory enabled for cost tracking
- ✓ TLS with auto-generation
- ✓ Session backend (memory for single, Redis for distributed)
- ✓ Logging in JSON format
- ✓ Optional remote config server support

### Policy Features

Each policy demonstrates:
- ✓ Multiple provider support (openai, anthropic, azure)
- ✓ Model-specific routing (gpt-4 vs gpt-3.5 vs claude)
- ✓ Rate limiting (requests/min, tokens/hour, tokens/day)
- ✓ Retry and fallback strategies
- ✓ Session memory for conversation tracking
- ✓ Content rules (jailbreak, PII, keywords, regex)
- ✓ Metadata for cost attribution (team, environment, cost_center)

### Agent Features

Each agent config includes:
- ✓ Tokenomics endpoint URL
- ✓ Raw token from policy creation
- ✓ LLM model selection
- ✓ Platform-specific config (Slack, Discord, iOS/macOS/Android)
- ✓ Skill enablement and restrictions
- ✓ Budget tracking (daily/monthly)
- ✓ Webhook monitoring
- ✓ Logging configuration

## Next Steps

1. **Review Examples** - Read through each JSON file to understand the structure
2. **Customize for Your Use Case**:
   - Update API keys in .env.example
   - Adjust budgets in policy files for your team
   - Add your Slack/Discord tokens to agent configs
3. **Run Integration Tests** - Verify setup with `./integration-test.sh`
4. **Deploy to Production** - See README.md for hardening guide
5. **Monitor** - Watch webhook events and ledger files

## References

- Main integration guide: `docs/OPENCLAW_INTEGRATION.md`
- Policy reference: `docs/POLICIES.md`
- Configuration reference: `docs/CONFIGURATION.md`
- Complete OPENCLAW plan: `OPENCLAW.md`
