# Admin UI Guide

Tokenomics includes an embedded admin SPA for local operations.

## Goals

- Provide operator visibility for usage, keys, sessions, and memory.
- Keep all assets embedded in the binary.
- Keep admin access local-only by default, with optional basic auth.

## Access

- Start persistent proxy:
  - `tokenomics start`
- Open:
  - `https://localhost:8443` (default)
  - `http://localhost:8080` (if HTTP enabled)

Admin routing and security:

- Admin UI and API are local-only.
- Optional basic auth is controlled by:
  - `admin.auth.username`
  - `admin.auth.password`

## Tabs

- `Analytics` - realtime counters and rollups.
- `Keys` - wrapper token list and policy editor modal.
- `Sessions` - session-level totals and drill-down.
- `Memory` - memory file browser and viewer.
- `Docs` - embedded operator instructions loaded from `cmd/web/admin/assets/docs.json`.

## Key Management UX

The `Keys` tab supports:

- `New Key` -> opens policy editor modal.
- `View` -> loads existing key into editor modal.
- `Save` -> creates or updates key.
- `Delete` -> deletes selected key.

When a key is created:

- A one-time modal shows the raw token value.
- Operator can copy the value.
- This is the only time the raw key is shown.

## Policy Editor Behavior

The policy editor supports:

- Global fields:
  - `base_key_env`, `upstream_url`, `default_provider`
  - `max_tokens`, `timeout`
  - `model` (exact), `model_regex` (pattern)
- Prompt rows with role and multiline content.
- Rule rows (`regex`, `keyword`, `pii`, `jailbreak`).
- Rate limit and retry/fallback fields.
- Metadata key-value rows.
- Provider policy rows (provider name + provider policy JSON object).
- Advanced JSON sync:
  - `Generate JSON From Form`
  - `Load JSON Into Form`

Notes:

- `model` is exact match.
- `model_regex` is pattern match.
- `Test regex` validates current regex against a model input.
- Memory settings are not edited in token policy UI. Memory is treated as tool-level configuration in this workflow.

## Admin API Endpoints

Core endpoints:

- `GET /admin/api/health`
- `GET /admin/api/analytics/summary`
- `GET /admin/api/keys`
- `POST /admin/api/keys`
- `PUT /admin/api/keys/{hash}`
- `DELETE /admin/api/keys/{hash}`
- `GET /admin/api/env-vars`
- `GET /admin/api/sessions`
- `GET /admin/api/sessions/{id}`
- `GET /admin/api/memory/files`
- `GET /admin/api/memory/files/{path}`

Compatibility aliases:

- `GET /admin/api/tokens`
- `GET /admin/api/tokens/{hash}`
- `GET /admin/api/usage/summary`
- `GET /admin/api/usage/tokens/{hash}`

## Embedded Assets

Admin assets are embedded via `go:embed` in `cmd/admin_ui.go`:

- `cmd/web/admin/index.html`
- `cmd/web/admin/assets/*`

Embedded docs source:

- `cmd/web/admin/assets/docs.json`

## Maintenance Rules

When admin behavior changes:

- Update this file: `docs/ADMIN_UI.md`.
- Update route and behavior summary: `docs/WEB.md`.
- Update in-app docs payload: `cmd/web/admin/assets/docs.json`.
- If config behavior changed, update `docs/CONFIGURATION.md`.
- If policy behavior changed, update `docs/POLICIES.md`.
