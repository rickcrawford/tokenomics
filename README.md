```
  _____     _                            _
 |_   _|__ | | _____ _ __   ___  _ __ __(_) ___ ___
   | |/ _ \| |/ / _ \ '_ \ / _ \| '_ ` _ \| |/ __/ __|
   | | (_) |   <  __/ | | | (_) | | | | | | | (__\__ \
   |_|\___/|_|\_\___|_| |_|\___/|_| |_| |_|_|\___|___/
```

Tokenomics is a reverse proxy for OpenAI-compatible APIs that lets you issue scoped wrapper tokens instead of sharing real API keys. Each token is bound to a policy that controls which models can be used, enforces token budgets, blocks unwanted content, and injects system prompts.

## Quick Start

### 1. Build

```bash
make build
```

### 2. Create a token

```bash
export TOKENOMICS_HASH_KEY="my-secret-hash-key"

./bin/tokenomics token create --policy '{
  "base_key_env": "OPENAI_API_KEY",
  "max_tokens": 100000,
  "model_regex": "^gpt-4.*"
}'
```

Save the printed `tkn_<uuid>` token -- it is only shown once.

### 3. Start the proxy

```bash
export OPENAI_API_KEY="sk-your-real-key"
export TOKENOMICS_HASH_KEY="my-secret-hash-key"

./bin/tokenomics serve
```

The proxy listens on `:8443` (HTTPS) and `:8080` (HTTP) by default.

### 4. Connect your agent

```bash
eval $(./bin/tokenomics init --token tkn_<your-token> --port 8443 --insecure)
```

Your agent now routes all API calls through Tokenomics. Run it as usual.

## Documentation

| Topic | Description |
|-------|-------------|
| [Configuration](docs/CONFIGURATION.md) | config.yaml fields, environment variables, CLI flags |
| [Policies](docs/POLICIES.md) | Policy JSON schema, model filtering, rules, prompts |
| [Token Management](docs/TOKEN_MANAGEMENT.md) | Creating, listing, and deleting tokens |
| [Agent Integration](docs/AGENT_INTEGRATION.md) | Connecting OpenAI, Anthropic, Azure, Gemini, and custom agents |
| [TLS](docs/TLS.md) | Auto-generated certificates, CA trust, custom certs |
| [Stats & Logging](docs/STATS_AND_LOGGING.md) | Request logging, /stats endpoint, usage tracking |

## License

MIT
