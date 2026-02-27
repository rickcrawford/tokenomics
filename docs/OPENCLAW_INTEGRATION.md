# OpenClaw Integration Guide

Tokenomics provides comprehensive guardrails for OpenClaw autonomous agents. Control costs, enforce safety policies, track usage, and attribute expenses across distributed agent fleets - all without modifying agent code.

## Overview

OpenClaw agents send requests to Tokenomics instead of directly to API providers. Tokenomics:

- **Controls Costs** - Daily/monthly budgets, rate limits, model restrictions per token
- **Enforces Safety** - Jailbreak detection, PII masking, content rules, system prompt injection
- **Tracks Usage** - Per-agent conversation logs, cost attribution, session analytics
- **Routes Providers** - Multi-provider failover, model-based selection, cost optimization
- **Emits Events** - Webhook notifications for requests, successes, errors, rule violations

## Architecture

```
OpenClaw Agent
    ↓
  [Token]
    ↓
Tokenomics Proxy ← Policy (budget, rules, routing)
    ↓
  [Token Cost Attribution & Analytics]
    ↓
OpenAI / Anthropic / Azure / Gemini / etc.
```

Each OpenClaw agent gets a scoped wrapper token bound to a policy. The policy controls what the agent can do and what gets recorded.

## Quick Start

### 1. Set Up Tokenomics

```bash
# Create ~/.tokenomics directory
mkdir -p ~/.tokenomics

# Set API keys (use your provider's PAT env var names)
export OPENAI_PAT="sk-proj-..."
export ANTHROPIC_PAT="sk-ant-..."

# Set hash key for token encryption
export TOKENOMICS_HASH_KEY="$(openssl rand -hex 16)"
```

### 2. Create a Token for Your Agent

```bash
# Create a token with a policy
tokenomics token create --policy '{
  "base_key_env": "OPENAI_PAT",
  "rate_limit": {
    "requests_per_minute": 100,
    "tokens_per_hour": 100000
  },
  "budget": {
    "type": "daily",
    "limit": 1000000
  },
  "rules": [
    {"type": "jailbreak", "action": "fail"},
    {"type": "pii", "detect": ["api_key", "private_key"], "action": "redact"}
  ]
}'
```

Output: `tkn_abc123...`

### 3. Run Your Agent

```bash
# Option A: Using tokenomics run command
export TOKENOMICS_KEY="tkn_abc123..."
tokenomics run python my_agent.py

# Option B: Manual proxy setup
tokenomics serve &
# In agent code:
# client = OpenAI(api_key="tkn_abc123...", base_url="http://localhost:8080/v1")
```

Your agent now has guardrails applied automatically.

## Configuration

### Config File

Create `~/.tokenomics/config.yaml`:

```yaml
# Server
listen: "0.0.0.0:8080"
tls: true

# Upstream providers
providers:
  openai:
    upstream_url: "https://api.openai.com/v1"
    api_key_env: "OPENAI_PAT"

  anthropic:
    upstream_url: "https://api.anthropic.com/v1"
    api_key_env: "ANTHROPIC_PAT"

# Logging
logging:
  disable_request: false
  hide_token_hash: false

# Session ledger (cost attribution)
ledger:
  enabled: true
  memory: true

# Webhooks for events
webhooks:
  - url: "http://localhost:5000/events"
    events:
      - "openclaw.agent.request"
      - "openclaw.agent.success"
      - "openclaw.agent.error"
      - "rule.violation"
```

### Policy JSON Schema

Policies control each token's behavior:

```json
{
  "base_key_env": "OPENAI_PAT",

  "budget": {
    "type": "daily|monthly|hourly",
    "limit": 1000000
  },

  "rate_limit": {
    "requests_per_minute": 100,
    "tokens_per_hour": 1000000,
    "max_parallel": 10
  },

  "model_allowlist": ["gpt-4", "gpt-3.5-turbo"],
  "model_allowlist_regex": "gpt-4.*",

  "rules": [
    {
      "type": "jailbreak",
      "action": "fail"
    },
    {
      "type": "pii",
      "detect": ["ssn", "credit_card", "api_key"],
      "action": "redact",
      "scope": "input"
    },
    {
      "type": "regex",
      "pattern": "SELECT.*FROM.*users",
      "action": "fail",
      "scope": "input"
    },
    {
      "type": "keyword",
      "keywords": ["password", "secret"],
      "action": "warn",
      "scope": "both"
    }
  ],

  "system_prompt": "You are a helpful assistant.",

  "memory": {
    "enabled": true,
    "file_path": "~/.tokenomics/memory",
    "file_name": "{token_hash}_{date}.md",
    "pii": "redact"
  },

  "routing": [
    {
      "model_regex": "gpt-4",
      "provider": "openai"
    },
    {
      "model_regex": "claude",
      "provider": "anthropic"
    }
  ],

  "fallback_chain": ["openai", "anthropic"]
}
```

## OpenClaw Metadata Headers

OpenClaw agents can send optional metadata headers to enable cost attribution and tracking:

```
X-OpenClaw-Agent-ID: slack-bot-123
X-OpenClaw-Agent-Type: slack
X-OpenClaw-Team: platform
X-OpenClaw-Channel: alerts
X-OpenClaw-Skill: search
X-OpenClaw-Environment: production
```

All headers are optional. Tokenomics captures them in session logs for analytics.

## Cost Attribution

View costs by agent, team, channel, or skill using the ledger:

```bash
# List all sessions
tokenomics ledger sessions

# Show session details
tokenomics ledger show SESSION_ID

# Summary statistics
tokenomics ledger summary
```

Or query programmatically:

```go
import "github.com/rickcrawford/tokenomics/internal/ledger"

analytics := ledger.NewOpenClawAnalytics(dir)

// Costs by team
teams, _ := analytics.ByMetadataKey("team")
for team, metrics := range teams {
  println(team, ":", metrics.TotalTokens)
}

// Costs by team/channel
breakdown, _ := analytics.ByTeamAndChannel()
for key, metrics := range breakdown {
  println(key, ":", metrics.TotalTokens)
}
```

## Webhook Events

Tokenomics emits events for real-time monitoring:

### Agent Request
Fired when agent starts a request with OpenClaw metadata:
```json
{
  "type": "openclaw.agent.request",
  "data": {
    "agent_id": "slack-bot-123",
    "team": "platform",
    "model": "gpt-4",
    "token_hash": "38974495c797ca7e"
  }
}
```

### Agent Success
Fired when request completes with 2xx status:
```json
{
  "type": "openclaw.agent.success",
  "data": {
    "agent_id": "slack-bot-123",
    "team": "platform",
    "model": "gpt-4",
    "status": 200,
    "token_hash": "38974495c797ca7e"
  }
}
```

### Agent Error
Fired when request fails:
```json
{
  "type": "openclaw.agent.error",
  "data": {
    "agent_id": "slack-bot-123",
    "team": "platform",
    "model": "gpt-4",
    "status": 429,
    "error": "rate limit exceeded",
    "token_hash": "38974495c797ca7e"
  }
}
```

### Rule Violation
Fired when content rules are violated:
```json
{
  "type": "rule.violation",
  "data": {
    "agent_id": "slack-bot-123",
    "team": "platform",
    "rule_name": "jailbreak",
    "message": "detected jailbreak attempt in input",
    "model": "gpt-4",
    "token_hash": "38974495c797ca7e"
  }
}
```

## Examples

See `examples/openclaw/` for complete working examples:

- **Slack Bot** - Slack integration with budget control and rule enforcement
- **Discord Bot** - Discord bot with cost tracking
- **Personal Assistant** - Personal use with aggressive safety
- **Enterprise Fleet** - Multi-team deployment with SQL injection blocking

## Integration Examples

### Slack Bot

```python
from slack_sdk import WebClient
from openai import OpenAI

# Point to Tokenomics proxy
client = OpenAI(
    api_key="tkn_slack_bot_xyz",
    base_url="http://tokenomics.local:8080/v1"
)

# Send metadata in headers
response = client.chat.completions.create(
    model="gpt-4",
    messages=[{"role": "user", "content": "help me"}],
    extra_headers={
        "X-OpenClaw-Agent-ID": "slack-bot-123",
        "X-OpenClaw-Team": "platform",
        "X-OpenClaw-Channel": channel,
        "X-OpenClaw-Skill": "search"
    }
)
```

### Discord Bot

```python
import discord
from openai import OpenAI

client = OpenAI(
    api_key="tkn_discord_bot_xyz",
    base_url="http://tokenomics.local:8080/v1"
)

async def on_message(message):
    if message.author == client.user:
        return

    response = client.chat.completions.create(
        model="gpt-3.5-turbo",
        messages=[{"role": "user", "content": message.content}],
        extra_headers={
            "X-OpenClaw-Agent-ID": "discord-bot",
            "X-OpenClaw-Team": "ml",
            "X-OpenClaw-Channel": message.channel.name
        }
    )
```

### Cost Attribution Script

```python
from pathlib import Path
from tokenomics.internal.ledger import NewOpenClawAnalytics

analytics = NewOpenClawAnalytics(Path.home() / ".tokenomics")

# Which teams used the most tokens?
teams, _ = analytics.ByMetadataKey("team")
for team, metrics in teams.items():
    print(f"{team}: {metrics.total_tokens} tokens ({metrics.request_count} requests)")

# Per-channel costs for platform team
channels, _ = analytics.ByMetadataKey("channel")
for channel, metrics in channels.items():
    if "platform/alerts" in channel:
        print(f"{channel}: ${metrics.total_tokens * 0.002 / 1000}")
```

## Best Practices

### Token Management

- **One token per agent** - Creates isolation and accountability
- **Separate policies per risk level** - Aggressive rules for untrusted agents, lenient for trusted
- **Use token expiration** - Temporary tokens for demos, seasonal scripts
- **Rotate tokens periodically** - Security best practice

### Policy Design

- **Enable jailbreak detection** - Always block prompt injection attempts
- **Redact PII by default** - Unaware agents won't leak secrets
- **Set realistic budgets** - Prevent accidental runaway costs
- **Use rate limiting** - Smooth load on downstream APIs, prevent DDoS
- **Monitor rule violations** - Webhook to Slack/Discord for alerts

### Webhook Integration

- **Real-time alerts** - Get notified of budget/rate limit/rule violations
- **Cost dashboards** - Visualize token usage by agent/team/channel
- **Audit trails** - Archive all events for compliance
- **Auto-remediation** - Disable tokens that exceed budgets

## Troubleshooting

### Agent can't connect

```bash
# Check Tokenomics is running
lsof -i :8080

# Verify token exists
tokenomics token list | grep tkn_

# Check proxy logs
tail -f ~/.tokenomics/tokenomics.log
```

### Requests blocked by jailbreak detection

```bash
# Review rule violations
tokenomics ledger sessions | grep -i violation

# Check session details
tokenomics ledger show SESSION_ID

# Adjust rules if false positives
tokenomics token update TOKEN --policy '{...}'
```

### Budget exceeded

```bash
# Check current usage
tokenomics ledger summary

# View by agent
analytics = NewOpenClawAnalytics(dir)
agents, _ = analytics.ByMetadataKey("agent_id")

# Increase budget or disable token
tokenomics token update TOKEN --policy '{...}'
```

### High latency

- Check rate limits (requests_per_minute, tokens_per_hour)
- Monitor upstream provider status
- Review fallback chain configuration
- Check Tokenomics server resources

## Security Considerations

### Token Isolation

Wrapper tokens are scoped to specific policies. Even if leaked, a token can only:
- Use allowed models
- Spend up to its budget
- Make requests respecting rate limits
- Be subject to content rules and jailbreak detection

### API Key Protection

Real API keys never touch agent code. Only wrapper tokens are shared. API keys stay secure in Tokenomics config.

### Audit Trail

All requests are logged with:
- Token hash (not token value)
- Model used
- Input/output tokens
- Rule matches
- Upstream response
- Agent metadata (team, channel, etc.)

### PII Redaction

Content rules can automatically redact sensitive data:
- SSNs, credit cards, API keys
- Private keys, tokens
- Database connection strings
- Custom regex patterns

## Monitoring

### Health Check

```bash
curl https://localhost:8080/stats
```

### Metrics Available

- Request count
- Token count (input/output)
- Error count
- Rule violation count
- Rate limit hits
- Budget exceeded count
- Per-model breakdown
- Per-provider breakdown

## Advanced Configuration

### Multi-Provider Routing

Route to different providers based on model:

```json
{
  "routing": [
    {"model_regex": "gpt-4", "provider": "openai"},
    {"model_regex": "claude", "provider": "anthropic"},
    {"model_regex": "gemini", "provider": "google"}
  ],
  "fallback_chain": ["openai", "anthropic", "google"]
}
```

### Custom System Prompts

Inject guardrail instructions:

```json
{
  "system_prompt": "You are a helpful assistant. You only answer questions about X. Never discuss Y."
}
```

### Conversation Memory

Record full conversation logs:

```json
{
  "memory": {
    "enabled": true,
    "file_path": "~/.tokenomics/memory",
    "file_name": "{token_hash}_{date}.md",
    "pii": "redact"
  }
}
```

## See Also

- [POLICIES.md](POLICIES.md) - Complete policy schema reference
- [CONFIGURATION.md](CONFIGURATION.md) - Server configuration options
- [TOKEN_MANAGEMENT.md](TOKEN_MANAGEMENT.md) - Token CRUD operations
- [FEATURES.md](FEATURES.md) - Full feature list
- [examples/openclaw/](../examples/openclaw/) - Working examples

## Support

- **GitHub Issues**: Report bugs and request features
- **Documentation**: See docs/ directory for detailed guides
- **Examples**: See examples/openclaw/ for working code
