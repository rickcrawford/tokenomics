# Token Management

Wrapper tokens are the credentials you hand out instead of real API keys. Each token is a unique, one-time-visible string that maps to a policy stored in the database.

## Token Format

Tokens follow the format `tkn_<uuid>`, for example:

```
tkn_a1b2c3d4-e5f6-7890-abcd-ef1234567890
```

The raw token is shown exactly once at creation time. It is never stored -- only its HMAC-SHA256 hash is persisted in the database.

## Hashing

Tokenomics hashes tokens using HMAC-SHA256 with a secret key. The key is read from the environment variable named in `security.hash_key_env` (default: `TOKENOMICS_HASH_KEY`).

```
HMAC-SHA256(token, key) -> hex-encoded hash
```

If `TOKENOMICS_HASH_KEY` is not set, a built-in default key is used. **Always set a custom key in production.**

When the proxy receives a request, it hashes the incoming token with the same key and looks up the resulting hash in the database to find the associated policy.

## Creating Tokens

### Important: `base_key_env` Must Match Provider Configuration

The `base_key_env` in your policy must match the `api_key_env` configured for that provider in `config.yaml`.

For example, if your config has:
```yaml
providers:
  anthropic:
    api_key_env: MY_ANTHROPIC_PAT_NEW
```

Then your token policy must use:
```json
"base_key_env": "MY_ANTHROPIC_PAT_NEW"
```

**If they don't match**, the proxy won't find the real API key and will return a 401 error.

### Creating a Token

```bash
./bin/tokenomics token create --policy '<policy-json>'
```

The `--policy` flag is required and takes a JSON string. See [Policies](POLICIES.md) for the full schema.

Example:

```bash
export TOKENOMICS_HASH_KEY="my-secret-hash-key"

./bin/tokenomics token create --policy '{
  "base_key_env": "MY_ANTHROPIC_PAT_NEW",
  "max_tokens": 100000
}'
```

Output:

```
Token created successfully.
WARNING: This token will only be shown once. Store it securely.

  Token: tkn_a1b2c3d4-e5f6-7890-abcd-ef1234567890
  Hash:  9f86d0818...
```

Save the `Token` value immediately. It cannot be recovered later. The `Hash` value is what gets stored in the database and appears in `token list` output.

### Token Expiration

Add `--expires` to set an expiration on the token:

```bash
# Duration-based (relative to now)
./bin/tokenomics token create --policy '{"base_key_env":"OPENAI_PAT"}' --expires 24h
./bin/tokenomics token create --policy '{"base_key_env":"OPENAI_PAT"}' --expires 7d
./bin/tokenomics token create --policy '{"base_key_env":"OPENAI_PAT"}' --expires 1y

# Exact timestamp
./bin/tokenomics token create --policy '{"base_key_env":"OPENAI_PAT"}' --expires 2025-12-31T00:00:00Z
```

Supported formats: Go durations (`24h`, `168h`), day shorthand (`7d`, `30d`), year shorthand (`1y`), or RFC3339 timestamps. When a token expires, requests using it are rejected and a `token.expired` event is emitted.

### Policy from File

Use `@` prefix to read policy JSON from a file:

```bash
./bin/tokenomics token create --policy @policy.json
```

## Inspecting Tokens

```bash
./bin/tokenomics token get --hash <hash>
```

Retrieves a token's full details including its policy JSON:

```
Hash:       9f86d0818...
Created:    2025-01-15 10:30:00 +0000 UTC
Expires:    2025-02-15T10:30:00Z (active)
Policy:
{
  "base_key_env": "OPENAI_PAT",
  "max_tokens": 100000
}
```

The expiration status shows `active` or `EXPIRED`. If the token has no expiration, it shows `never`.

## Updating Tokens

```bash
./bin/tokenomics token update --hash <hash> [--policy '<json>'] [--expires <value>]
```

Update a token's policy, expiration, or both. At least one of `--policy` or `--expires` is required.

```bash
# Update the policy
./bin/tokenomics token update --hash 9f86d0818... --policy '{"base_key_env":"OPENAI_PAT","max_tokens":200000}'

# Extend the expiration
./bin/tokenomics token update --hash 9f86d0818... --expires 30d

# Remove expiration (make permanent)
./bin/tokenomics token update --hash 9f86d0818... --expires clear

# Update both at once
./bin/tokenomics token update --hash 9f86d0818... --policy '{"max_tokens":500000}' --expires 7d
```

A `token.updated` event is emitted on successful update.

## Listing Tokens

```bash
./bin/tokenomics token list
```

Displays all stored token hashes along with their policy details:

```
Hash:       9f86d0818...
Created:    2025-01-15 10:30:00 +0000 UTC
Expires:    2025-02-15T10:30:00Z (active)
Key Env:    OPENAI_PAT
Model Regex: ^gpt-4.*
Max Tokens: 100000
Prompts:    0
Rules:      0
---
```

Fields shown per token:

| Field | Description |
|-------|-------------|
| Hash | The HMAC-SHA256 hash (the database key) |
| Created | Timestamp when the token was created |
| Expires | Expiration timestamp with status (`active` or `EXPIRED`). Only shown if set. |
| Key Env | The `base_key_env` from the policy |
| Upstream | The `upstream_url` (only shown if set) |
| Model | The exact `model` restriction (only shown if set) |
| Model Regex | The `model_regex` pattern (only shown if set) |
| Max Tokens | The token budget (only shown if set) |
| Prompts | Number of injected prompt messages |
| Rules | Number of content-blocking rules |

## Deleting Tokens

```bash
./bin/tokenomics token delete --hash <hash>
```

The `--hash` flag takes the hex-encoded hash shown during creation or in `token list` output.

Example:

```bash
./bin/tokenomics token delete --hash 9f86d0818...
```

Output:

```
Token deleted.
```

Once deleted, any requests using that token will be rejected immediately.

## Global Flags

All token subcommands accept the global flags:

| Flag | Description |
|------|-------------|
| `--config <path>` | Path to config file |
| `--db <path>` | Database path (overrides config) |
