# Agent Integration

There are three commands for connecting agents to the proxy:

| Command | Purpose |
|---------|---------|
| `tokenomics run` | Starts proxy, runs a single command, stops proxy |
| `tokenomics start` | Starts proxy as a background daemon |
| `tokenomics init` | Outputs environment variables for a provider (does not start the proxy) |

| Scenario | Commands |
|----------|----------|
| Single command | `tokenomics run claude "test"` |
| Multiple commands | `tokenomics start` then `eval $(tokenomics init)` then run commands |
| Remote proxy | `tokenomics init --proxy-url https://proxy.company.com` |
| Agent config | `tokenomics init --agent claude-code` |

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

# Using a remote proxy (uses shared proxy instead of local)
TOKENOMICS_KEY=tkn_test tokenomics run --proxy-url https://proxy.company.com:8443 claude "test"
```

### Remote Proxy Configuration

You can use a remote Tokenomics proxy instead of starting a local one. This is useful when:
- Running on multiple machines that share a central proxy
- Using a managed Tokenomics service
- Testing against a shared staging proxy

Set either:
- **Environment variable:** `TOKENOMICS_PROXY_URL=https://proxy.example.com:8443`
- **Command-line flag:** `--proxy-url https://proxy.example.com:8443`

When a proxy URL is provided, the `--host`, `--port`, and `--tls` flags are ignored (they only apply to local proxy startup).

```bash
# Using environment variable
export TOKENOMICS_PROXY_URL="https://shared-proxy.company.com:8443"
export TOKENOMICS_KEY="tkn_my-wrapper-token"
tokenomics run claude "test"

# Using flag (overrides env var)
tokenomics run --proxy-url https://other-proxy.com:8443 claude "test"
```

### `tokenomics run` Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--token` | `$TOKENOMICS_KEY` | The wrapper token (reads from env var if not provided) |
| `--proxy-url` | `$TOKENOMICS_PROXY_URL` | Remote proxy URL (if set, uses remote proxy instead of starting local) |
| `--provider` | (auto-detected) | Override provider: `generic`, `anthropic`, `azure`, `gemini` |
| `--host` | `localhost` | Proxy hostname (only used if starting local proxy) |
| `--port` | `8080` | Proxy port (only used if starting local proxy) |
| `--tls` | `false` | Use HTTPS scheme (default false for run, traffic is localhost only) |
| `--insecure` | `false` | Skip TLS verification (only applies when --tls is enabled) |

## Manual Mode: `tokenomics start` + `tokenomics init`

The `run` command starts the proxy for a single command and stops it when done. For multiple commands, start the proxy separately with `start`, then use `init` to get the environment variables.

`init` does not start the proxy. It only outputs environment variables.

### Usage

```bash
export TOKENOMICS_KEY="tkn_my-wrapper-token"

tokenomics start                   # Start proxy daemon in background
eval $(tokenomics init)            # Set env vars for the default provider

# Run multiple commands (they all use the running proxy)
claude "prompt 1"
python my_script.py
node app.js

tokenomics stop                    # Stop proxy when done
```

### Using a Remote Proxy

When pointing at a remote proxy, you only need `init` (no `start` needed):

```bash
export TOKENOMICS_KEY="tkn_my-wrapper-token"
eval "$(tokenomics init --proxy-url https://proxy.company.com:8443 --provider anthropic)"
claude "prompt 1"
python script.py
```

### `tokenomics start` Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--host` | `localhost` | Proxy hostname |
| `--port` | `8443` | Proxy port |
| `--tls` | `true` | Use HTTPS |
| `--pid-file` | `~/.tokenomics/tokenomics.pid` | PID file path |
| `--log-file` | `~/.tokenomics/tokenomics.log` | Log file path |

The `start` command prints the proxy URL to stdout, which can be captured:

```bash
export TOKENOMICS_PROXY_URL=$(tokenomics start)
```

### `tokenomics stop` Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--pid-file` | `~/.tokenomics/tokenomics.pid` | PID file path |

Sends SIGTERM for graceful shutdown. Falls back to SIGKILL after 3 seconds if the process does not exit.

### `tokenomics init` Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--token` | `$TOKENOMICS_KEY` | The wrapper token |
| `--proxy-url` | `$TOKENOMICS_PROXY_URL` | Proxy URL (if set, used directly in env var output) |
| `--provider` | `generic` | Target provider: any name from providers.yaml, or `all` for every provider |
| `--host` | `localhost` | Proxy hostname for constructing the base URL |
| `--port` | `8443` | Proxy port for constructing the base URL |
| `--tls` | `true` | Use HTTPS scheme in the base URL |
| `--insecure` | `false` | Add `NODE_TLS_REJECT_UNAUTHORIZED=0` to env output |
| `--output` | `shell` | Output format: `shell`, `dotenv`, `json` |
| `--dotenv` | (empty) | Path to .env file (used with `--output dotenv`) |
| `--agent` | (empty) | Write config for an agent framework (`claude-code`) |

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

The `run` command defaults to `--tls=false` (plain HTTP on localhost), so TLS errors do not apply. The `start` command defaults to `--tls=true`. If TLS is enabled, you have two options:

1. Install the CA certificate (recommended). See [TLS](TLS.md).
2. Use `--insecure` to skip TLS verification (development only):

```bash
tokenomics run --tls --insecure claude "test"
tokenomics init --insecure --token tkn_abc123  # adds NODE_TLS_REJECT_UNAUTHORIZED=0
```
