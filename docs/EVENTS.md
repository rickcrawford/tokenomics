# Events & Webhooks

Tokenomics emits events for key lifecycle moments -- token CRUD, policy violations, budget alerts, rate limiting, and request completion. Events are delivered via webhooks, with an interface designed for future transport types (message bus, pub/sub, etc.).

## Configuration

Add webhooks to your `config.yaml`:

```yaml
events:
  webhooks:
    - url: https://example.com/tokenomics/webhook
      secret: "shared-secret-for-auth"
      signing_key: "hmac-key-for-signatures"
      events:
        - "token.*"
        - "rule.*"
        - "budget.exceeded"
      timeout: 10

    - url: https://slack-webhook.example.com/events
      events:
        - "rule.violation"
        - "budget.exceeded"
        - "rate.exceeded"
```

Multiple webhooks can be configured. Each receives events independently with its own filter.

### Webhook Fields

| Field | Description | Default |
|-------|-------------|---------|
| `url` | HTTP endpoint to POST events to (required) | — |
| `secret` | Shared secret sent as `X-Webhook-Secret` header | — |
| `signing_key` | HMAC-SHA256 key; signature sent as `X-Webhook-Signature` | — |
| `events` | Event type filter (supports trailing `*` wildcard); empty = all | all |
| `timeout` | HTTP timeout in seconds | 10 |

## Event Types

### Token Lifecycle

| Event | Fired When |
|-------|------------|
| `token.created` | A new wrapper token is created |
| `token.updated` | A token's policy or expiration is modified |
| `token.deleted` | A token is revoked/deleted |
| `token.expired` | An expired token is used (detected at lookup time) |

### Policy Rules

| Event | Fired When |
|-------|------------|
| `rule.violation` | A content rule with `fail` action blocked a request |
| `rule.warning` | A content rule with `warn` action matched |
| `rule.match` | A content rule with `log` action matched |
| `rule.mask` | Content was redacted by a `mask` rule before forwarding |

### Budget & Rate Limiting

| Event | Fired When |
|-------|------------|
| `budget.exceeded` | A request would exceed the token's budget limit |
| `budget.update` | Token usage was recorded after a successful request |
| `rate.exceeded` | A request was rejected due to rate limiting |

### Request Lifecycle

| Event | Fired When |
|-------|------------|
| `request.completed` | A proxied request finished (success or upstream error) |

### System

| Event | Fired When |
|-------|------------|
| `server.start` | The proxy server has started and is ready to accept requests |

## Event Payload

Every event is a JSON object with the following structure:

```json
{
  "id": "evt_a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4",
  "type": "token.created",
  "timestamp": "2025-01-15T10:30:00.000000000Z",
  "data": {
    "token_hash": "abc12345",
    "expires_at": "2025-02-15T10:30:00Z"
  }
}
```

| Field | Description |
|-------|-------------|
| `id` | Unique event identifier (`evt_` prefix + 32 hex chars) |
| `type` | Event type string (see table above) |
| `timestamp` | RFC3339Nano UTC timestamp |
| `data` | Event-specific payload (varies by type) |

### Data Fields by Event Type

**`token.created`**
```json
{ "token_hash": "abc12345", "expires_at": "2025-02-15T10:30:00Z" }
```

**`token.updated`**
```json
{ "token_hash": "abc12345", "expires_at": "2025-03-15T10:30:00Z" }
```

**`token.deleted`**
```json
{ "token_hash": "abc12345" }
```

**`token.expired`**
```json
{ "token_hash": "abc12345", "expired_at": "2025-01-15T10:30:00Z" }
```

**`rule.violation`**
```json
{
  "token_hash": "abc12345def67890",
  "model": "gpt-4o",
  "rule_name": "prompt-injection",
  "message": "matched regex rule \"prompt-injection\""
}
```

**`rule.warning`** / **`rule.match`**
```json
{
  "token_hash": "abc12345def67890",
  "model": "gpt-4o",
  "rule_name": "pii-detector",
  "action": "warn",
  "message": "detected PII in rule \"pii-detector\": SSN"
}
```

**`rule.mask`**
```json
{ "token_hash": "abc12345def67890", "model": "gpt-4o" }
```

**`budget.exceeded`**
```json
{
  "token_hash": "abc12345def67890",
  "model": "gpt-4o",
  "used": 95000,
  "input": 6000,
  "limit": 100000
}
```

**`budget.update`**
```json
{ "token_hash": "abc12345def67890", "model": "gpt-4o", "input_tokens": 1500 }
```

**`rate.exceeded`**
```json
{
  "token_hash": "abc12345def67890",
  "model": "gpt-4o",
  "error": "rate limit exceeded: 61 requests in 1m window (limit 60)"
}
```

**`request.completed`**
```json
{
  "token_hash": "abc12345def67890",
  "model": "gpt-4o",
  "stream": false,
  "status_code": 200,
  "input_tokens": 1500,
  "output_tokens": 800,
  "error": false
}
```

**`server.start`**
```json
{
  "http_port": 8080,
  "https_port": 8443,
  "tls": true,
  "upstream": "https://api.openai.com"
}
```

## Event Filtering

The `events` field supports:

- **Exact match**: `"token.created"` matches only `token.created`
- **Trailing wildcard**: `"token.*"` matches `token.created`, `token.deleted`, `token.updated`, `token.expired`
- **Catch-all**: `"*"` matches everything (same as omitting the field)
- **Multiple patterns**: `["token.*", "rule.violation", "budget.*"]`

## Security

### Shared Secret

Set `secret` to include a static token in the `X-Webhook-Secret` header. Your endpoint can verify this to authenticate that requests come from Tokenomics.

```yaml
events:
  webhooks:
    - url: https://example.com/webhook
      secret: "my-shared-secret"
```

Your endpoint checks:
```
X-Webhook-Secret: my-shared-secret
```

### HMAC Signature

Set `signing_key` to include an HMAC-SHA256 signature of the request body in the `X-Webhook-Signature` header. This lets you verify payload integrity.

```yaml
events:
  webhooks:
    - url: https://example.com/webhook
      signing_key: "my-hmac-key"
```

Your endpoint receives:
```
X-Webhook-Signature: sha256=a1b2c3d4...
```

To verify (pseudocode):
```python
expected = hmac_sha256(signing_key, request_body)
assert request.headers["X-Webhook-Signature"] == f"sha256={expected}"
```

Both `secret` and `signing_key` can be used together for defense in depth.

## HTTP Headers

Every webhook request includes:

| Header | Description |
|--------|-------------|
| `Content-Type` | `application/json` |
| `User-Agent` | `Tokenomics-Webhook/1.0` |
| `X-Event-ID` | Unique event identifier |
| `X-Event-Type` | Event type string |
| `X-Webhook-Secret` | Shared secret (if configured) |
| `X-Webhook-Signature` | `sha256=<hex>` HMAC signature (if configured) |

## Delivery

- Events are **queued asynchronously** -- webhook delivery does not block request processing
- Failed deliveries are **retried up to 3 times** with exponential backoff (2s, 4s)
- **4xx errors** (except 429) are not retried (considered permanent failures)
- **429 and 5xx errors** trigger retries
- The internal queue holds up to 256 events; events are dropped if the queue is full
- On shutdown, the queue is drained before the process exits

## Webhook Receiver (Inbound)

Proxy instances can also receive inbound webhooks for push-based token sync. When the central config server emits a `token.*` event, it can push that event to all proxy instances via their webhook receiver endpoint. This triggers an immediate remote sync instead of waiting for the poll interval.

### Configuration

On each proxy instance, enable the receiver in `config.yaml`:

```yaml
remote:
  url: http://config-server:9090
  api_key: my-remote-key
  sync: 300                      # Fallback polling every 5 minutes
  webhook:
    enabled: true
    path: /v1/webhook            # Default path
    secret: my-webhook-secret    # Must match outbound webhook secret
    signing_key: my-signing-key  # Must match outbound webhook signing key
```

On the central config server, add outbound webhooks pointing to each proxy:

```yaml
events:
  webhooks:
    - url: https://proxy-1.internal:8443/v1/webhook
      secret: my-webhook-secret
      signing_key: my-signing-key
      events: ["token.*"]

    - url: https://proxy-2.internal:8443/v1/webhook
      secret: my-webhook-secret
      signing_key: my-signing-key
      events: ["token.*"]
```

### How It Works

1. Admin creates/updates/deletes a token on the central config server
2. The central server emits a `token.*` event via its outbound webhooks
3. Each proxy's webhook receiver validates the request (secret and/or HMAC signature)
4. On valid `token.*` events, the proxy immediately syncs from the central server
5. Non-token events (e.g., `request.completed`) are ignored
6. Rapid successive events are debounced (1 second window)

### Receiver Responses

| Status | Body | Meaning |
|--------|------|---------|
| `200` | `{"status":"accepted"}` | Token sync triggered |
| `200` | `{"status":"debounced"}` | Skipped (synced less than 1 second ago) |
| `200` | `{"status":"ignored","reason":"not a token event"}` | Non-token event, no sync needed |
| `400` | `{"error":"invalid json"}` | Malformed request body |
| `401` | `{"error":"unauthorized"}` | Missing or incorrect `X-Webhook-Secret` |
| `403` | `{"error":"invalid signature"}` | HMAC signature verification failed |
| `405` | `{"error":"method not allowed"}` | Not a POST request |

### Authentication

The receiver supports the same authentication mechanisms as outbound webhooks:

- **Shared secret**: Set `remote.webhook.secret` and include `X-Webhook-Secret` in the request
- **HMAC signature**: Set `remote.webhook.signing_key` and include `X-Webhook-Signature: sha256=<hex>`
- Both can be used together for defense in depth

### Push vs. Poll

| Method | Latency | Config |
|--------|---------|--------|
| Polling only | Up to `sync` seconds | `remote.sync: 60` |
| Push only | Sub-second | `remote.webhook.enabled: true`, `remote.sync: 0` |
| Push + poll fallback | Sub-second, with safety net | Both enabled |

The recommended setup is push with a long poll fallback (e.g., `sync: 300`) to handle cases where webhook delivery fails.

## Interface

The event system is built on the `Emitter` interface:

```go
type Emitter interface {
    Emit(ctx context.Context, event Event) error
    Close() error
}
```

Current implementations:
- **`WebhookEmitter`** — HTTP POST to a URL (outbound)
- **`WebhookReceiver`** — HTTP endpoint that accepts events and triggers sync (inbound)
- **`Multi`** — Fan-out to multiple emitters
- **`Nop`** — No-op (used when no webhooks are configured)

Future implementations could include message buses (Kafka, NATS, RabbitMQ), cloud pub/sub (AWS SNS, GCP Pub/Sub), or log sinks.
