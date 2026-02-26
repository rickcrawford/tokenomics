# Quick Start

Get up and running in three steps.

## 1) Install

```bash
curl -fsSL https://github.com/rickcrawford/tokenomics/releases/latest/download/install.sh | bash
```

Or build from source:

```bash
git clone https://github.com/rickcrawford/tokenomics.git
cd tokenomics && make build
sudo cp bin/tokenomics /usr/local/bin/
```

Verify: `tokenomics --help`

## 2) Set environment variables

Two variables are required:

| Variable | Purpose |
|----------|---------|
| `OPENAI_API_KEY` | The real provider API key you want to wrap |
| `TOKENOMICS_HASH_KEY` | Secret used to hash wrapper tokens (pick any random string) |

```bash
export OPENAI_API_KEY="<your-openai-api-key>"
export TOKENOMICS_HASH_KEY="<any-random-secret-string>"
```

## 3) Create a token and run

Create a wrapper token that wraps your OpenAI key:

```bash
tokenomics token create --policy '{"base_key_env":"OPENAI_API_KEY"}'
```

Copy the returned token (`tkn_...`) and run a command through the proxy:

```bash
export TOKENOMICS_KEY="tkn_<paste-your-token-here>"
tokenomics run python my_script.py
```

The `run` command handles everything: it starts the proxy, configures environment variables, runs your command, and cleans up when done. No separate `serve` step is needed.

## What just happened?

1. Your real API key (`OPENAI_API_KEY`) stays on the server, never exposed to the command.
2. The wrapper token (`tkn_...`) is what the command sees. It has no direct access to the real key.
3. The proxy started on `http://localhost:8080`, forwarded the request to OpenAI, and shut down after the command finished.

## Next steps

- Add budgets, rate limits, and content rules to your token: [Policies](POLICIES.md)
- Run multiple commands with a persistent proxy: [Agent Integration](AGENT_INTEGRATION.md)
- Configure providers and settings: [Configuration](CONFIGURATION.md)
