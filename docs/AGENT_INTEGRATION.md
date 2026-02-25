# Agent Integration

There are two ways to run agents through the Tokenomics proxy:

1. **`tokenomics run`** (recommended for single commands) — Starts proxy, runs your command, stops proxy (all in one go)
2. **`tokenomics init`** (recommended for multiple commands) — Starts proxy in background, run commands manually, then stop proxy when done

| Scenario | Use | Proxy Lifecycle |
|----------|-----|-----------------|
| Single command | `tokenomics run claude "test"` | Start → Run → Stop |
| Multiple commands | `tokenomics init` then run commands | Start once → Run many → Stop once |
| Quick test | `tokenomics run --insecure cmd` | Start → Run → Stop (no cert needed) |
| Long session | `tokenomics init` with many commands | Proxy stays running for all commands |

## Quick Start: `tokenomics run` (Recommended)

### Step 1: Install CA Certificate

On first run, Tokenomics generates a self-signed CA certificate. You need to install it once:

```bash
make build
./bin/tokenomics serve  # Generates certs and shows installation instructions
```

The proxy will show something like:

```
CA cert for trust installation: certs/ca.crt
On macOS: sudo security add-trusted-cert -d -r trustRoot -k /Library/Keychains/System.keychain certs/ca.crt
On Linux: sudo cp certs/ca.crt /usr/local/share/ca-certificates/ && sudo update-ca-certificates
```

Follow the instructions for your OS. Once done, agents will trust the proxy's TLS certificate.

### Step 2: Run Commands

```bash
export TOKENOMICS_KEY="tkn_my-wrapper-token"
tokenomics run claude "What is AI?"
```

That's it! The `run` command:
- Auto-detects which provider to use (claude → anthropic, python → generic, etc.)
- Starts the proxy
- Sets up environment variables
- Runs your command
- Cleans up when done

### Auto-Detection (CLI Maps)

Configure which CLI maps to which provider in `config.yaml`:

```yaml
cli_maps:
  claude: anthropic
  anthropic: anthropic
  python: generic
  node: generic
  curl: generic
```

### Examples

Each command starts its own proxy instance (proxy runs while command runs, then stops):

```bash
# Auto-detect provider from CLI name
TOKENOMICS_KEY=tkn_test tokenomics run claude "What is AI?"

# Override provider if needed
TOKENOMICS_KEY=tkn_test tokenomics run --provider azure -- custom-cli arg1

# Python script (proxy runs for duration of script)
TOKENOMICS_KEY=tkn_test tokenomics run python my_script.py

# Node.js application
TOKENOMICS_KEY=tkn_test tokenomics run node app.js

# With explicit host/port
TOKENOMICS_KEY=tkn_test tokenomics run --host proxy.example.com --port 9000 -- python script.py
```

### `tokenomics run` Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--token` | `$TOKENOMICS_KEY` | The wrapper token (reads from env var if not provided) |
| `--provider` | (auto-detected) | Override provider: `generic`, `anthropic`, `azure`, `gemini` |
| `--host` | `localhost` | Proxy hostname |
| `--port` | `8443` | Proxy port |
| `--tls` | `true` | Use HTTPS scheme |
| `--insecure` | `false` | Skip TLS verification (not recommended; install CA cert instead) |

## Manual Mode: `tokenomics init`

The `run` command starts the proxy for a single command and stops it when done. If you want to run **multiple commands** through the same proxy session without restarting it each time, use `init` to start the proxy in the background once.

### Usage

```bash
export TOKENOMICS_KEY="tkn_my-wrapper-token"
tokenomics init                    # Start proxy in background (stays running)

# Run multiple commands (they all use the running proxy)
claude "prompt 1"
python my_script.py
node app.js

tokenomics stop                    # Stop proxy when done
```

This is more efficient than `tokenomics run` for multiple commands since you only start/stop the proxy once.

### `tokenomics init` Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--token` | `$TOKENOMICS_KEY` | The wrapper token |
| `--provider` | `generic` | Target provider: `generic`, `anthropic`, `azure`, `gemini` |
| `--host` | `localhost` | Proxy hostname |
| `--port` | `8443` | Proxy port |
| `--tls` | `true` | Use HTTPS scheme |
| `--insecure` | `false` | Skip TLS verification (not recommended; install CA cert instead) |
| `--output` | `shell` | Output format: `shell`, `dotenv`, `json` |
| `--dotenv` | (empty) | Path to .env file (used with `--output dotenv`) |

### Output Formats

#### Shell (default)

Returns environment variable export statements:

```bash
tokenomics init --token tkn_abc123 --provider generic --output shell
```

Output:
```bash
export OPENAI_API_KEY="tkn_abc123"
export OPENAI_BASE_URL="https://localhost:8443/v1"
export NODE_TLS_REJECT_UNAUTHORIZED="0"
```

#### Dotenv

Writes to a `.env` file:

```bash
tokenomics init --token tkn_abc123 --output dotenv --dotenv .env.proxy
```

#### JSON

Returns JSON representation:

```bash
tokenomics init --token tkn_abc123 --output json
```

Output:
```json
{
  "OPENAI_API_KEY": "tkn_abc123",
  "OPENAI_BASE_URL": "https://localhost:8443/v1",
  "NODE_TLS_REJECT_UNAUTHORIZED": "0"
}
```

## Supported Providers

### Generic / OpenAI (default)

Sets standard OpenAI SDK environment variables. The base URL includes the `/v1` path suffix.

```bash
tokenomics run python my_script.py
```

Configures:
```bash
OPENAI_API_KEY=tkn_...
OPENAI_BASE_URL=https://localhost:8443/v1
```

### Anthropic

```bash
tokenomics run claude "What is AI?"
```

Configures:
```bash
ANTHROPIC_API_KEY=tkn_...
ANTHROPIC_BASE_URL=https://localhost:8443
```

### Azure OpenAI

```bash
tokenomics run --provider azure -- python my_script.py
```

Configures:
```bash
AZURE_OPENAI_API_KEY=tkn_...
AZURE_OPENAI_ENDPOINT=https://localhost:8443
```

### Gemini

```bash
tokenomics run --provider gemini -- python my_script.py
```

Configures:
```bash
GEMINI_API_KEY=tkn_...
GEMINI_BASE_URL=https://localhost:8443
```

## Environment Setup

### Using .env File

Tokenomics automatically loads `.env` from the current directory or `~/.tokenomics/.env`. Set the wrapper token there:

```bash
# .env
TOKENOMICS_KEY=tkn_my-wrapper-token
OPENAI_API_KEY=sk-...  # Real provider keys (optional)
```

Then just run:
```bash
tokenomics run claude "prompt"
```

### Using Environment Variables

Export before running:

```bash
export TOKENOMICS_KEY="tkn_my-wrapper-token"
tokenomics run python my_script.py
```

### Secure Key Storage

For production, use a secrets manager or `.env` file that's not committed to version control:

```bash
# In .env (don't commit to git)
TOKENOMICS_KEY=tkn_...
OPENAI_API_KEY=sk-...
ANTHROPIC_API_KEY=sk-ant-...
```

Add to `.gitignore`:
```
.env
.env.local
```

## TLS Configuration

By default, TLS verification is **enabled** for security. The proxy generates a self-signed CA certificate that you should install once.

### Proper Setup (Recommended)

Install the CA certificate (one-time):

```bash
# On macOS
sudo security add-trusted-cert -d -r trustRoot -k /Library/Keychains/System.keychain certs/ca.crt

# On Linux
sudo cp certs/ca.crt /usr/local/share/ca-certificates/
sudo update-ca-certificates

# On Windows
certutil -addstore -f "Root" certs/ca.crt
```

Then use without `--insecure`:

```bash
tokenomics run python my_script.py
```

### Development Only: Skip TLS Verification

If you cannot install certificates, use `--insecure` **only for development**:

```bash
tokenomics run --insecure claude "test"
```

**Not recommended for production.** See [TLS](TLS.md) for more details.

## Troubleshooting

### Token Not Found

If you get "no token provided":

```bash
# Make sure TOKENOMICS_KEY is set
export TOKENOMICS_KEY="tkn_abc123"
tokenomics run claude "test"

# Or pass directly
tokenomics run --token tkn_abc123 claude "test"
```

### Provider Not Auto-Detected

If `tokenomics run claude` doesn't work, add the mapping to `config.yaml`:

```yaml
cli_maps:
  claude: anthropic
  # Add more mappings here
```

### TLS Certificate Errors

Use `--insecure=true` (which is already the default):

```bash
tokenomics run claude "test"  # Already skips TLS verification
```

Or install the CA certificate and use `--insecure=false`. See [TLS](TLS.md).
