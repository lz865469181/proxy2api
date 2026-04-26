# proxy2api

`proxy2api` is an OpenAI-compatible AI gateway that combines core ideas from `sub2api` and `VoAPI`:

- `sub2api` style: account pool, sticky session, gateway controls.
- `VoAPI` style: rule-driven routing, channel/group operations, runtime controls.

## Implemented Capabilities

- OpenAI-compatible ingress: `POST /v1/*`, `GET /v1/models`
- Gateway API key authentication (`Authorization: Bearer ...`)
- Multi-provider weighted routing
- Provider key pool (multiple upstream keys under one provider)
- Sticky session routing (default header: `session_id`)
- User RPM/TPM and provider-key RPM/TPM controls
- Auto key cooldown and recovery after repeated upstream failures
- JS rule engine (`condition_js`) with actions:
  - `deny`
  - `force_provider`
  - `rate_multiplier`
- Group schedule multiplier (time-window based channel throttling multiplier)
- Persistent config/state with SQLite
- Usage record persistence and 24h summary
- Admin APIs + minimal admin UI (`/admin/ui`)
- Runtime hot reload (periodic + manual)

## Quick Start

1. Prepare config:

```bash
cp config/config.example.yaml config/config.yaml
```

2. Edit keys in `config/config.yaml`:

- `auth.admin_key`
- `keys[].key`
- `providers[].upstream_keys[]`

3. Start:

```bash
go run ./cmd/proxy2api -config config/config.yaml
```

Default listen: `:8080`.

## Runtime Model

At startup:

1. Read YAML config
2. Open SQLite (`db.path`)
3. Seed DB from YAML only when DB is empty
4. Build runtime snapshot from DB

During runtime:

- gateway reloads DB snapshot every `gateway.reload_seconds`
- admin can trigger immediate reload via API
- provider key health (consecutive errors/cooldown) persists in DB

## Endpoints

Public:

- `GET /healthz`
- `GET /v1/models`
- `POST /v1/chat/completions` (or other `/v1/*`)

Admin (`X-Admin-Key` required):

- `GET /admin/providers`
- `POST /admin/providers/{name}/enable`
- `POST /admin/providers/{name}/disable`
- `GET /admin/provider-keys`
- `POST /admin/provider-keys?id={id}&enabled=true|false`
- `GET /admin/stats`
- `POST /admin/reload`
- `GET /admin/ui`

## Rule Context

Each rule `condition_js` receives:

- `ctx.api_key` (gateway key)
- `ctx.model`
- `ctx.path`

Example condition:

```js
ctx.api_key === "sk-enterprise" && ctx.model.startsWith("gpt-")
```

## Validation Status

Automated tests cover:

- proxy success + usage stats persistence
- deny rule behavior
- provider key cooldown and recovery

Run:

```bash
go test ./...
```
