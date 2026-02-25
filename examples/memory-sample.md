# Memory Sample

This file shows what Tokenomics session memory files look like when `memory.enabled` is set to `true` in a token policy.

## How it works

When a token policy includes memory configuration, the proxy logs every user message and assistant response to a file (or Redis). Each session (token) gets its own file.

### Per-session files (recommended)

Set `file_path` (directory) and `file_name` (pattern):

```json
{
  "memory": {
    "enabled": true,
    "file_path": "./memory",
    "file_name": "{token_hash}.md"
  }
}
```

This creates one file per token under the `./memory` directory:

```
memory/
  a1b2c3d4e5f6a1b2.md
  f7e8d9c0b1a2f3e4.md
```

### File name placeholders

| Placeholder | Replaced with | Example |
|-------------|---------------|---------|
| `{token_hash}` | First 16 chars of token HMAC hash | `a1b2c3d4e5f6a1b2` |
| `{date}` | Current UTC date | `2026-02-25` |

Patterns can include subdirectories:

```json
{ "file_name": "{date}/{token_hash}.md" }
```

Produces:

```
memory/
  2026-02-25/
    a1b2c3d4e5f6a1b2.md
    f7e8d9c0b1a2f3e4.md
  2026-02-26/
    a1b2c3d4e5f6a1b2.md
```

### Single-file mode (legacy)

Omit `file_name` to write all sessions to one file:

```json
{
  "memory": {
    "enabled": true,
    "file_path": "./memory/all-sessions.md"
  }
}
```

### Redis-based memory

Push entries to a Redis list keyed by token hash:

```json
{
  "memory": {
    "enabled": true,
    "redis": true
  }
}
```

Redis entries are stored in `tokenomics:memory:<session_id>` using RPUSH.

## Entry format

Each entry follows this structure:

```
## <timestamp> | <token_hash_prefix> | <role> | <model>

<content>

---
```

| Field | Description |
|-------|-------------|
| `timestamp` | UTC RFC3339 timestamp of the request |
| `token_hash_prefix` | First 16 characters of the token's HMAC hash |
| `role` | `user` or `assistant` |
| `model` | The model used for the request |
| `content` | The message content (user input or assistant response) |

## Sample output: per-session files

With the config `{"file_path": "./memory", "file_name": "{token_hash}.md"}`, here is what the individual files look like.

### `memory/a1b2c3d4e5f6a1b2.md`

---

## 2026-02-25T14:30:01Z | a1b2c3d4e5f6a1b2 | user | gpt-4o

What is the capital of France?

---

## 2026-02-25T14:30:02Z | a1b2c3d4e5f6a1b2 | assistant | gpt-4o

The capital of France is Paris. It is the largest city in France and serves as the country's political, economic, and cultural center.

---

## 2026-02-25T14:31:15Z | a1b2c3d4e5f6a1b2 | user | gpt-4o

Write a Python function that checks if a number is prime.

---

## 2026-02-25T14:31:17Z | a1b2c3d4e5f6a1b2 | assistant | gpt-4o

Here's a Python function to check if a number is prime:

```python
def is_prime(n):
    if n < 2:
        return False
    for i in range(2, int(n**0.5) + 1):
        if n % i == 0:
            return False
    return True
```

This works by checking divisibility up to the square root of `n`, which is sufficient to determine primality.

---

## 2026-02-25T15:02:08Z | a1b2c3d4e5f6a1b2 | user | gpt-4o

Explain the difference between TCP and UDP in simple terms.

---

## 2026-02-25T15:02:10Z | a1b2c3d4e5f6a1b2 | assistant | gpt-4o

TCP (Transmission Control Protocol) and UDP (User Datagram Protocol) are two ways to send data over a network.

**TCP** is like sending a registered letter. It guarantees delivery, maintains order, and confirms receipt. It's used for web pages, email, and file transfers where every byte matters.

**UDP** is like shouting across a room. It's faster because it skips the handshake and confirmation steps, but packets can be lost or arrive out of order. It's used for video calls, gaming, and streaming where speed matters more than perfection.

---

### `memory/f7e8d9c0b1a2f3e4.md`

---

## 2026-02-25T14:35:42Z | f7e8d9c0b1a2f3e4 | user | gpt-4o-mini

My SSN is 123-45-6789 and my credit card is 4111-1111-1111-1111. Can you help me fill out this form?

---

## 2026-02-25T14:35:43Z | f7e8d9c0b1a2f3e4 | assistant | gpt-4o-mini

I can help you fill out the form. I noticed your message contained sensitive information that was redacted for security. Please provide the form details and I'll assist you with completing it safely.

---

## Notes

- PII masking happens before memory writes. If a policy has PII rules with `mask` action, the content logged to memory will contain `[REDACTED]` instead of sensitive data.
- Each token hash prefix identifies a unique wrapper token. All requests through the same token write to the same file.
- Memory is only written for successful requests (HTTP 200). Failed requests (rate limited, blocked by rules, budget exceeded) are not logged to memory.
- Files are opened in append mode. Restarting the proxy preserves existing entries.
- Subdirectories in the `file_name` pattern (e.g. `{date}/...`) are created automatically.
