# Stats and Logging

Tokenomics provides two observability features: structured JSON request logs written to stdout, and a `/stats` HTTP endpoint for aggregated usage data.

## Request Logging

Every proxied request produces a JSON log entry written to stdout. Each entry contains:

| Field | Type | Description |
|-------|------|-------------|
| `timestamp` | string | ISO 8601 timestamp of the request |
| `method` | string | HTTP method (`POST`, `GET`, etc.) |
| `path` | string | Request path (e.g. `/v1/chat/completions`) |
| `token_hash` | string | HMAC hash of the wrapper token used |
| `model` | string | Model requested (if applicable) |
| `base_key_env` | string | Environment variable name for the upstream API key |
| `upstream_url` | string | Upstream URL the request was forwarded to |
| `status_code` | int | HTTP status code returned to the client |
| `duration_ms` | int | Request duration in milliseconds |
| `input_tokens` | int | Input tokens consumed (from upstream response) |
| `output_tokens` | int | Output tokens consumed (from upstream response) |
| `stream` | bool | Whether the request used streaming |
| `error` | string | Error message, if the request failed |
| `remote_addr` | string | Client IP address |
| `user_agent` | string | Client User-Agent header |

Example log entry:

```json
{
  "timestamp": "2025-01-15T10:30:00Z",
  "method": "POST",
  "path": "/v1/chat/completions",
  "token_hash": "9f86d081884c...",
  "model": "gpt-4o",
  "base_key_env": "OPENAI_PAT",
  "upstream_url": "https://api.openai.com",
  "status_code": 200,
  "duration_ms": 1234,
  "input_tokens": 150,
  "output_tokens": 300,
  "stream": false,
  "remote_addr": "127.0.0.1:54321",
  "user_agent": "python-requests/2.31.0"
}
```

Logs can be piped to any JSON-aware log aggregator (e.g. `jq`, Datadog, Loki).

## /stats Endpoint

The `/stats` endpoint returns aggregated usage data as JSON. No authentication is required.

```bash
curl http://localhost:8080/stats
```

### Response Format

```json
{
  "totals": {
    "request_count": 150,
    "input_tokens": 45000,
    "output_tokens": 30000,
    "total_tokens": 75000,
    "error_count": 3
  },
  "by_model_and_key": [
    {
      "model": "gpt-4o",
      "base_key_env": "OPENAI_PAT",
      "request_count": 100,
      "input_tokens": 30000,
      "output_tokens": 20000,
      "total_tokens": 50000,
      "error_count": 1
    },
    {
      "model": "gpt-4o-mini",
      "base_key_env": "OPENAI_PAT",
      "request_count": 50,
      "input_tokens": 15000,
      "output_tokens": 10000,
      "total_tokens": 25000,
      "error_count": 2
    }
  ],
  "by_token": [
    {
      "token_hash": "9f86d081884c7d65",
      "request_count": 75,
      "input_tokens": 22000,
      "output_tokens": 15000,
      "total_tokens": 37000,
      "error_count": 0,
      "last_model": "gpt-4o",
      "base_key_env": "OPENAI_PAT",
      "first_seen": "2025-01-15T10:00:00Z",
      "last_seen": "2025-01-15T11:30:00Z"
    }
  ]
}
```

### Response Fields

#### `totals`

Global counters across all models and tokens.

| Field | Description |
|-------|-------------|
| `request_count` | Total number of proxied requests |
| `input_tokens` | Total input tokens consumed |
| `output_tokens` | Total output tokens consumed |
| `total_tokens` | Sum of input + output tokens |
| `error_count` | Total number of failed requests |

#### `by_model_and_key`

Usage broken down by each unique combination of model and `base_key_env`. Sorted by `total_tokens` descending.

| Field | Description |
|-------|-------------|
| `model` | The model name |
| `base_key_env` | The upstream API key environment variable |
| `request_count` | Number of requests for this model/key pair |
| `input_tokens` | Input tokens for this model/key pair |
| `output_tokens` | Output tokens for this model/key pair |
| `total_tokens` | Sum of input + output tokens |
| `error_count` | Number of failed requests |

#### `by_token`

Per-token session tracking. Each wrapper token that has made at least one request appears here. Token hashes are truncated to 16 characters. Sorted by `total_tokens` descending.

| Field | Description |
|-------|-------------|
| `token_hash` | First 16 characters of the HMAC hash |
| `request_count` | Number of requests from this token |
| `input_tokens` | Input tokens consumed by this token |
| `output_tokens` | Output tokens consumed by this token |
| `total_tokens` | Sum of input + output tokens |
| `error_count` | Number of failed requests from this token |
| `last_model` | Most recently used model |
| `base_key_env` | Upstream API key environment variable |
| `first_seen` | Timestamp of the first request from this token |
| `last_seen` | Timestamp of the most recent request from this token |

## Notes

- Stats are held in memory and reset when the proxy restarts.
- The `/stats` endpoint is available on both HTTP and HTTPS listeners.
- The `/stats` endpoint only accepts `GET` requests.
- The `/health` endpoint returns `{"status":"ok"}` and can be used for health checks.
- The `/ping` endpoint returns a `200 OK` heartbeat response.
