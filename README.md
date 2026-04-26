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
- Persistent config/state with SQLite/MySQL/PostgreSQL
- Redis distributed limiter (optional)
- Usage record persistence and 24h summary
- Full web admin console (`/admin/ui`)
- Runtime hot reload (periodic + manual)
- Prometheus metrics endpoint (`/metrics`)

## Quick Start

1. Prepare config:

```bash
cp config/config.example.yaml config/config.yaml
```

2. Edit keys in `config/config.yaml`:

- `auth.admin_key`
- `keys[].key`
- `providers[].upstream_keys[]`
- DB and Redis section if needed

3. Start:

```bash
go run ./cmd/proxy2api -config config/config.yaml
```

Default listen: `:8080`.

## Database Modes

`db.driver` supports:

- `sqlite` (default, local file path `db.path`)
- `mysql` (use `db.dsn`)
- `postgres` (use `db.dsn`)

Example PostgreSQL DSN:

```text
host=127.0.0.1 user=proxy2api password=proxy2api dbname=proxy2api port=5432 sslmode=disable
```

Example MySQL DSN:

```text
proxy2api:proxy2api@tcp(127.0.0.1:3306)/proxy2api?parseTime=true&loc=Local
```

## Docker Local Deployment

1. Bootstrap config file:

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\bootstrap-config.ps1
```

2. Edit `config/config.yaml`:

- `auth.admin_key`
- `keys[].key`
- `providers[].upstream_keys[]`

3. Start with Docker Compose:

```bash
docker compose up -d --build
```

Optional profiles:

- PostgreSQL: `docker compose --profile postgres up -d --build`
- MySQL: `docker compose --profile mysql up -d --build`

4. Check status:

```bash
docker compose ps
curl http://localhost:8080/healthz
```

5. Stop:

```bash
docker compose down
```

Notes:

- `./config` and `./data` are mounted into container, data persists locally.
- Default container command uses `/app/config/config.yaml`.

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
- `GET /metrics`

Admin (`X-Admin-Key` required):

- `GET /admin/providers`
- `POST /admin/providers/{name}/enable`
- `POST /admin/providers/{name}/disable`
- `GET /admin/provider-keys`
- `POST /admin/provider-keys?id={id}&enabled=true|false`
- `GET /admin/stats`
- `POST /admin/reload`
- `GET /admin/ui`
- `GET /admin/rules`
- `POST /admin/rules`
- `DELETE /admin/rules?id={id}`
- `GET /admin/schedules`
- `POST /admin/schedules`
- `DELETE /admin/schedules?id={id}`
- `GET /admin/config/export`
- `POST /admin/config/import`
- `GET /admin/api-keys`
- `POST /admin/api-keys`
- `DELETE /admin/api-keys?id={id}`

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

Optional Docker validation:

```bash
docker compose config
docker compose up -d --build
docker compose logs -f proxy2api
```
