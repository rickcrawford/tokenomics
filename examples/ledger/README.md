# Ledger Examples

This directory contains example files for the session ledger feature.

## Files

| File | Description |
|------|-------------|
| `session-example.json` | Example session summary JSON written by the ledger |
| `ledger-config.yaml` | Minimal config to enable the ledger |
| `gitignore-example` | Suggested `.gitignore` patterns for `.tokenomics/` |

## Quick Start

1. Copy the config section into your `config.yaml`:

```yaml
ledger:
  enabled: true
  dir: ".tokenomics"
  memory: true
```

2. Run the proxy:

```bash
tokenomics serve
```

3. Make some requests. On shutdown, a session file appears in `.tokenomics/sessions/`.

4. View usage:

```bash
tokenomics ledger summary
tokenomics ledger sessions
tokenomics ledger show <session-id>
```

5. Commit `.tokenomics/` alongside your code:

```bash
git add .tokenomics/sessions/
git commit -m "Add token usage for feature X"
```

## Gitignore

To keep session data but exclude conversation memory:

```gitignore
.tokenomics/memory/
```

To exclude everything:

```gitignore
.tokenomics/
```
