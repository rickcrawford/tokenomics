# Secret Management

Tokenomics never stores API keys in code, configuration files, or token definitions. Instead, it references secrets by environment variable name. This keeps sensitive credentials secure and makes it easy to rotate keys without changing policies or tokens.

## Core Principle: References, Not Values

When you create a token, you specify which environment variable contains the API key:

```json
{
  "base_key_env": "OPENAI_PAT"
}
```

The policy stores the **reference** (`OPENAI_PAT`), not the actual key. When a request comes in, Tokenomics looks up the environment variable at runtime and uses the value.

## Example

### Setup

```bash
# Set environment variable with actual API key
export OPENAI_PAT="sk-proj-abc123def456..."

# Create policy that references it by name
tokenomics token create --policy '{"base_key_env":"OPENAI_PAT"}'
# Output: tkn_xyz789...
```

### Result

- Policy stores: `"base_key_env": "OPENAI_PAT"` (never the actual key)
- Token stores: encrypted policy (never sees the key)
- At request time: looks up `$OPENAI_PAT` from environment and uses it

### Rotation

Change the key without updating any policies:

```bash
export OPENAI_PAT="sk-proj-new-key-789xyz..."
# All existing tokens automatically use the new key
```

## Environment Variable Naming

Use custom names to isolate keys by purpose and make them easier to manage:

```bash
# Provider PATs (Personal Access Tokens)
export OPENAI_PAT="sk-proj-..."
export ANTHROPIC_PAT="sk-ant-..."
export AZURE_OPENAI_PAT="..."

# Tokenomics-specific configuration
export TOKENOMICS_HASH_KEY="random-secret-for-token-encryption"
export TOKENOMICS_KEY="tkn_..."

# Optional: webhook secrets
export WEBHOOK_SECRET="shared-secret-for-hmac-validation"
```

Avoid default SDK env var names like `OPENAI_API_KEY` or `ANTHROPIC_API_KEY` to keep API keys isolated from standard library lookups.

## .env File Handling

Tokenomics loads environment variables from multiple sources in this order:

1. **System environment variables** (set via `export` or shell startup files)
2. **`.tokenomics/.env`** in the data directory (if it exists)
3. **`.env` in current working directory** (if it exists)
4. **Process defaults** (built-in values)

### Creating a .env File

For local development, create `~/.tokenomics/.env`:

```bash
# ~/.tokenomics/.env
OPENAI_PAT=sk-proj-dev-key-here
ANTHROPIC_PAT=sk-ant-dev-key-here
TOKENOMICS_HASH_KEY=dev-hash-key-only-for-testing
```

Tokenomics will automatically load these when you start the server:

```bash
tokenomics serve
# Reads ~/.tokenomics/.env automatically
```

### .env in Current Directory

Alternatively, create `.env` in your working directory:

```bash
# .env (in project root)
OPENAI_PAT=sk-proj-...
TOKENOMICS_HASH_KEY=secret
```

Then run Tokenomics from that directory:

```bash
cd /my/project
tokenomics serve
# Reads .env from current directory
```

### .env Format

The `.env` file uses simple `KEY=VALUE` format:

```bash
# Comments start with #
OPENAI_PAT=sk-proj-abc123

# Values with spaces need quotes
CUSTOM_PROMPT="Be helpful and kind"

# Empty lines are ignored
ANOTHER_KEY=value

# No space around =
NOT_KEY = value  # This line is ignored (spaces around =)
```

### .env Security

**Important:** `.env` files contain secrets. Always:

- **Add `.env` to `.gitignore`** to prevent accidental commits:
  ```bash
  echo ".env" >> .gitignore
  echo ".tokenomics/.env" >> .gitignore
  ```

- **Restrict file permissions:**
  ```bash
  chmod 600 ~/.tokenomics/.env
  chmod 600 .env
  ```

- **Never commit `.env` to version control**

- **Use `.env.example`** to document required variables without exposing values:
  ```bash
  # .env.example
  OPENAI_PAT=sk-proj-your-key-here
  ANTHROPIC_PAT=sk-ant-your-key-here
  TOKENOMICS_HASH_KEY=random-secret-string
  ```

## Secret Rotation

Tokenomics supports zero-downtime secret rotation:

### Process

1. **Generate new API key** from your provider (OpenAI, Anthropic, etc.)
2. **Update environment variable:**
   ```bash
   export OPENAI_PAT="sk-proj-new-key"
   ```
3. **Restart Tokenomics** (or send SIGHUP to reload):
   ```bash
   kill -HUP $(pgrep tokenomics)
   ```
4. **Verify** with a test request
5. **Revoke old key** from provider once confirmed working

No policy or token changes needed.

## Multi-Provider Secrets

When using multiple providers, each has its own environment variable:

```json
{
  "providers": {
    "openai": {
      "api_key_env": "OPENAI_PAT"
    },
    "anthropic": {
      "api_key_env": "ANTHROPIC_PAT"
    },
    "azure": {
      "api_key_env": "AZURE_OPENAI_PAT"
    }
  }
}
```

Each variable is independent:

```bash
export OPENAI_PAT="sk-proj-..."
export ANTHROPIC_PAT="sk-ant-..."
export AZURE_OPENAI_PAT="..."
```

If a provider's env var is not set when a request matches that provider, Tokenomics returns an error.

## Server Configuration

The main Tokenomics server also uses environment variables for configuration:

### Server Configuration Variables

```bash
# Required
TOKENOMICS_HASH_KEY="random-secret-used-to-encrypt-tokens"

# Optional
TOKENOMICS_DIR="~/.tokenomics"       # Data directory
TOKENOMICS_LISTEN="0.0.0.0:8080"    # Listen address (default)
TOKENOMICS_TLS="true"               # Enable TLS
```

Override config file values:

```bash
# config.yaml has:
# listen: "0.0.0.0:8000"

# Override at runtime:
export TOKENOMICS_LISTEN="127.0.0.1:9000"
tokenomics serve
# Binds to 127.0.0.1:9000 instead
```

## Webhook Security

If using webhooks, Tokenomics can sign events with a shared secret:

```bash
export WEBHOOK_SECRET="shared-secret-between-tokenomics-and-receiver"
```

Receivers can verify the signature by:
1. Receiving the X-Signature header
2. Computing HMAC-SHA256(body, WEBHOOK_SECRET)
3. Comparing with X-Signature value

## Best Practices

### 1. Use Descriptive Names

```bash
# Good - clear purpose
export OPENAI_PAT="..."
export ANTHROPIC_PAT="..."

# Bad - too generic or misleading
export API_KEY="..."
export SECRET="..."
```

### 2. Rotate Regularly

- Set a reminder to rotate API keys monthly or quarterly
- Stagger rotations across providers to avoid outages
- Test new keys before revoking old ones

### 3. Use .env for Development, Env Vars for Production

**Development:**
```bash
# .env file (in .gitignore)
OPENAI_PAT=dev-key
```

**Production:**
```bash
# Set via secrets management (AWS Secrets Manager, Vault, etc.)
export OPENAI_PAT=$(aws secretsmanager get-secret-value ...)
```

### 4. Audit Access

- Log which tokens and policies use which secrets
- Monitor for unusual request patterns
- Set up alerts for rate limit or budget violations

### 5. Isolate by Environment

Use different API keys for different environments:

```bash
# development
export OPENAI_PAT="sk-proj-dev-..."

# staging
export OPENAI_PAT="sk-proj-stage-..."

# production
export OPENAI_PAT="sk-proj-prod-..."
```

Each has different rate limits, budgets, and quotas.

## Troubleshooting

### "API key environment variable not set"

```
Error: base_key_env "OPENAI_PAT" is not set
```

**Solution:** Set the environment variable:
```bash
export OPENAI_PAT="sk-proj-..."
tokenomics serve
```

Or add to `.env`:
```bash
# ~/.tokenomics/.env
OPENAI_PAT=sk-proj-...
```

### "Invalid API key from provider"

The environment variable is set, but the value is wrong.

```bash
# Verify the variable is set
echo $OPENAI_PAT

# Check if it's a valid key format (starts with sk-proj-)
# Verify the key is active in your provider console
# Make sure you're not using an old/revoked key
```

### Secrets not loading from .env

1. Check file location - should be `~/.tokenomics/.env` or `.env` in working directory
2. Check file permissions - must be readable by Tokenomics process
3. Check file format - `KEY=VALUE` with no spaces around `=`
4. Check startup logs - Tokenomics logs which files it reads

```bash
# Debug: see what Tokenomics loads
export TOKENOMICS_DEBUG=true
tokenomics serve
```

### Multiple .env Files

If both `~/.tokenomics/.env` and `.env` exist, system environment variables take precedence:

```
System env ($OPENAI_PAT)
    ↓ (if not set)
~/.tokenomics/.env
    ↓ (if not found)
.env (in working directory)
    ↓ (if not found)
Fail with error
```

## See Also

- [CONFIGURATION.md](CONFIGURATION.md) - Config file reference
- [TOKEN_MANAGEMENT.md](TOKEN_MANAGEMENT.md) - Token creation and management
- [AGENT_INTEGRATION.md](AGENT_INTEGRATION.md) - Connecting agents with tokens
