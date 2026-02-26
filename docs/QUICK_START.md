# Quick Start

This guide gets Tokenomics running in a few minutes.

## 1) Install

```bash
curl -fsSL https://github.com/rickcrawford/tokenomics/releases/latest/download/install.sh | bash
```

If the installer puts the binary in your current directory, move it to your PATH:

```bash
sudo cp ./tokenomics /usr/local/bin/
```

Verify:

```bash
tokenomics --help
```

## 2) Set required secrets

```bash
export TOKENOMICS_HASH_KEY="replace-with-random-secret"
export OPENAI_API_KEY="sk-..."
```

## 3) Start the proxy

```bash
tokenomics serve
```

This creates `.tokenomics/` in your current directory and starts the proxy.

## 4) Create a wrapper token

Open another terminal:

```bash
tokenomics token create --policy '{
  "base_key_env": "OPENAI_API_KEY",
  "max_tokens": 100000
}'
```

Copy the returned wrapper token (`tkn_...`).

## 5) Send a request through Tokenomics

```bash
curl -s http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer tkn_your_wrapper_token" \
  -d '{
    "model": "gpt-4o-mini",
    "messages": [{"role":"user","content":"Say hello in one sentence."}]
  }'
```

## 6) Inspect usage and logs

```bash
tokenomics ledger summary
ls -la .tokenomics/
```

## Next steps

- Configure providers and defaults: `docs/CONFIGURATION.md`
- Add guardrails and budgets: `docs/POLICIES.md`
- Integrate with agents and CLI wrappers: `docs/AGENT_INTEGRATION.md`
- Distribution and release details: `docs/DISTRIBUTION.md`
