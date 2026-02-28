# Embedded Web Admin

Tokenomics includes an embedded admin SPA served directly from the binary.

## Tabs

- Analytics - totals, errors, provider/model usage, ledger rollups.
- Keys - wrapper token inventory and policy summaries.
- Sessions - session file list with usage drill-down.
- Memory - memory file list and content viewer.
- Docs - embedded operator instructions.

Detailed admin UX and policy editor instructions:

- See `ADMIN_UI.md`.

## UI Routes

- `GET /` - Admin SPA shell.
- `GET /admin` and `GET /admin/*` - SPA routes.
- `GET /assets/*` - Embedded JS/CSS.

## API Routes

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

Compatibility endpoints retained:

- `GET /admin/api/tokens`
- `GET /admin/api/tokens/{hash}`
- `GET /admin/api/usage/summary`
- `GET /admin/api/usage/tokens/{hash}`

## Access and Security

- Admin is local-only. Non-loopback access is rejected.
- Optional basic auth:

```yaml
admin:
  enabled: true
  auth:
    username: ""
    password: ""
```

When both auth values are set, all admin UI/API routes require HTTP Basic Auth.

## `run` Command Behavior

- `tokenomics run` disables admin on its ephemeral proxy by default.
- Admin can be enabled for run-managed ephemeral proxy only when:
  - `--admin` flag is set, and
  - `admin.enabled: true` in config.

## Provider Catalog Defaults

- Built-in defaults are embedded from `internal/config/providers.embedded.yaml`.
- `providers.yaml` remains the repo source file for edits.
- Sync is enforced by `internal/config/config_test.go`:
  - `TestEmbeddedProvidersYAMLSyncedWithRootProvidersYAML`.
