# shagan_pos

The **Shagan POS backend**: a multi-tenant point-of-sale API for multi-branch
retail shops in Myanmar - catalog, sales, shifts and cash, returns, inventory,
procurement, customers, reports and offline sync.

> **New here?** Read [docs/OVERVIEW.md](docs/OVERVIEW.md) (the whole system in
> one page), then [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md). Business rules
> are in [docs/WORKFLOWS.md](docs/WORKFLOWS.md) - the source of truth for every
> client.

| Doc | What's in it |
|---|---|
| [OVERVIEW.md](docs/OVERVIEW.md) | The three repos, who uses what, system diagram |
| [ARCHITECTURE.md](docs/ARCHITECTURE.md) | Key technical decisions and why |
| [WORKFLOWS.md](docs/WORKFLOWS.md) | Roles, permissions, journeys, business rules |
| [ERD.md](docs/ERD.md) | Database tables and relationships |
| [STATUS.md](docs/STATUS.md) | What's done, in progress, blocked; risks |
| [backend-recommendations.md](docs/backend-recommendations.md) | Change requests raised during frontend integration |

## Stack

- **Go 1.25**, **Gin**, **GORM** on **PostgreSQL 16**
- JWT (`golang-jwt/jwt/v5`), bcrypt for passwords and PINs
- `shopspring/decimal` for money, `google/uuid`
- **MinIO** (any S3-compatible store) for product images and payment QR codes
- Tests: `testify`, `mockery` mocks, in-memory SQLite for repository tests

## Run it locally

```bash
cp .env.example .env          # set JWT_SECRET and INTERNAL_API_KEY at least
docker compose up -d          # postgres :5432, minio :9000 (console :9001), api :8000
curl localhost:8000/healthz   # {"status":"ok"}
```

Or run the API on the host against the Docker database:

```bash
docker compose up -d postgres minio
go run ./cmd                  # reads .env; listens on :8000
```

On start-up the server runs GORM **AutoMigrate** for every domain (no SQL
migration files) and needs `JWT_SECRET` or it exits.

### Data

- `go run ./cmd/seed` - seeds the permission list and default Manager /
  Cashier roles for any org without roles. Idempotent.
- `db/demo_seed.sql` - a full demo org ("Golden Mart Myanmar": 3 branches,
  12 staff, 42 products, 21 days of trading). Start the API once to create the
  tables, load the file, then run `go run ./cmd/seed`.
- New tenants are normally created through `/internal/*` by
  [shagan_team](https://github.com/channyein-thu/shagan_team), or the Postman
  "UAT - Full User Flow" folder.

### Environment

| Variable | Purpose |
|---|---|
| `DATABASE_URL` | Postgres DSN |
| `PORT` | Listen port (default `8000`) |
| `JWT_SECRET` | Signs every JWT. **Required** |
| `INTERNAL_API_KEY` | Shared secret for `/internal/*` (`X-Internal-Key`). Unset = all internal calls rejected |
| `CORS_ALLOWED_ORIGINS` | Comma-separated exact origins; empty = none; `*` for dev only |
| `STORAGE_ENDPOINT`, `STORAGE_ACCESS_KEY`, `STORAGE_SECRET_KEY`, `STORAGE_BUCKET`, `STORAGE_USE_SSL` | S3 / MinIO |

## Tests

```bash
go test ./...                 # ~740 tests
```

Repository and migration tests use in-memory SQLite and need **CGO** (a C
compiler). With `CGO_ENABLED=0` they are silently skipped. No live Postgres is
needed.

## API at a glance

Base path `/api/v1` (≈125 routes). The Postman collection in `postman/`
mirrors them one-to-one, plus a UAT end-to-end folder.

| Header | Who sends it | Lifetime |
|---|---|---|
| `Authorization: Bearer <access>` | Every signed-in client (owner, service center, POS device) | 60 min (refresh token 30 days) |
| `X-Staff-Token` | A till, for the cashier signed in by PIN | 12 h |
| `X-Manager-Approval-Token` | A till, for one approved action (discount, void, ...) | 2 min |
| `X-Internal-Key` | shagan_team only, on `/internal/*` | - |

Health: `GET /healthz` (pings the database). The only stub left is
`POST /reports/export` (501).

## Code layout

```
cmd/
  main.go            wiring: env, DB, migrate, storage, CORS, route groups
  api/<domain>.go    HTTP handlers + route registration per domain
  seed/              permission + default-role seeding
internal/<domain>/   audit, catalog, customer, datasync, identity, inventory,
                     platform, procurement, reports, returns, sales, shift
  <d>_model.go       GORM models
  <d>_dto.go         request / response types
  <d>_migrate.go     AutoMigrate
  <d>_repository*.go interface + GORM implementation
  <d>_service*.go    interface + business logic
internal/common, middleware, authtoken, storage, migrate, seed, healthcheck
db/demo_seed.sql     demo dataset
postman/             collection + environment
```

Each `cmd/api/<d>.go` builds repository → service → handler. A service gets
other domains' repositories as small interfaces (e.g. returns uses sales,
inventory and audit readers/writers), so domains don't import each other's
services.
