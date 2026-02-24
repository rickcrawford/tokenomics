# Agent Integration

The `init` command configures agent frameworks and SDKs to route API calls through the Tokenomics proxy. It outputs the correct environment variables for your chosen provider.

## Usage

```bash
./bin/tokenomics init --token <tkn_...> [flags]
```

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--token` | (required) | The wrapper token to use |
| `--host` | `localhost` | Proxy hostname |
| `--port` | `8443` | Proxy port |
| `--tls` | `true` | Use HTTPS scheme |
| `--insecure` | `false` | Skip TLS verification (adds `NODE_TLS_REJECT_UNAUTHORIZED=0`) |
| `--cli` | `generic` | Target provider: `generic`, `anthropic`, `azure`, `gemini`, `custom` |
| `--env-key` | (empty) | Custom env var name for the API key (for `custom` provider) |
| `--env-base-url` | (empty) | Custom env var name for the base URL (for `custom` provider) |
| `--output` | `shell` | Output format: `shell`, `dotenv`, `json` |
| `--dotenv` | `.env` | Path to .env file (used with `--output dotenv`) |

## Providers

### Generic / OpenAI (default)

Sets the standard OpenAI SDK environment variables. The base URL includes the `/v1` path suffix.

```bash
eval $(./bin/tokenomics init --token tkn_abc123)
```

Produces:

```bash
export OPENAI_API_KEY="tkn_abc123"
export OPENAI_BASE_URL="https://localhost:8443/v1"
```

### Anthropic

```bash
eval $(./bin/tokenomics init --token tkn_abc123 --cli anthropic)
```

Produces:

```bash
export ANTHROPIC_API_KEY="tkn_abc123"
export ANTHROPIC_BASE_URL="https://localhost:8443"
```

### Azure OpenAI

```bash
eval $(./bin/tokenomics init --token tkn_abc123 --cli azure)
```

Produces:

```bash
export AZURE_OPENAI_API_KEY="tkn_abc123"
export AZURE_OPENAI_ENDPOINT="https://localhost:8443"
```

### Gemini

```bash
eval $(./bin/tokenomics init --token tkn_abc123 --cli gemini)
```

Produces:

```bash
export GEMINI_API_KEY="tkn_abc123"
export GEMINI_BASE_URL="https://localhost:8443"
```

### Custom Provider

Use `--env-key` and `--env-base-url` to set arbitrary environment variable names:

```bash
eval $(./bin/tokenomics init --token tkn_abc123 \
  --env-key MY_API_KEY --env-base-url MY_BASE_URL)
```

Produces:

```bash
export MY_API_KEY="tkn_abc123"
export MY_BASE_URL="https://localhost:8443"
```

## Output Formats

### Shell (default)

Prints `export` statements to stdout. Use with `eval` to set variables in the current shell:

```bash
eval $(./bin/tokenomics init --token tkn_abc123 --output shell)
```

### Dotenv

Writes key-value pairs to a `.env` file. If the file exists, matching keys are updated in place; new keys are appended.

```bash
./bin/tokenomics init --token tkn_abc123 --output dotenv --dotenv .env.local
```

### JSON

Prints a JSON object to stdout:

```bash
./bin/tokenomics init --token tkn_abc123 --output json
```

```json
{
  "OPENAI_API_KEY": "tkn_abc123",
  "OPENAI_BASE_URL": "https://localhost:8443/v1"
}
```

## Using --insecure

When the proxy uses auto-generated certificates that are not trusted by the system, pass `--insecure` to add `NODE_TLS_REJECT_UNAUTHORIZED=0` to the output. This disables TLS certificate verification for Node.js-based agents.

```bash
eval $(./bin/tokenomics init --token tkn_abc123 --insecure)
```

For non-Node.js agents, you may need to configure TLS verification separately. See [TLS](TLS.md) for alternatives like installing the CA certificate.

## Using HTTP Instead of HTTPS

To connect over plain HTTP (e.g., when TLS is disabled on the proxy):

```bash
eval $(./bin/tokenomics init --token tkn_abc123 --port 8080 --tls=false)
```
