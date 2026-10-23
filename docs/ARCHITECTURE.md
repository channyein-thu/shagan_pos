# Architecture and key decisions

Each section gives the decision, why it was made, and its cost. For the
system's shape see [OVERVIEW.md](OVERVIEW.md); for business rules see
[WORKFLOWS.md](WORKFLOWS.md).

## 1. Backend structure

```
HTTP (cmd/api/<domain>.go)  →  service (internal/<d>/<d>_service_impl.go)  →  repository (<d>_repository_impl.go)  →  GORM / Postgres
```

- **One package per business domain** under `internal/`: identity, catalog,
  sales, returns, shift, inventory, procurement, customer, platform, datasync,
  audit, reports.
- **Services depend on interfaces, not on other services.** For example, the
  returns service receives small `SalesReader`, `InventoryWriter` and
  `AuditWriter` interfaces backed by other domains' repositories
  (`cmd/api/returns.go`). Domains stay decoupled and services are unit-tested
  with mockery mocks.
- **Multi-step writes run in one transaction** (`common.Transactioner`): for
  example, a sale, its stock movements and the audit row commit together.
- **Schema by GORM AutoMigrate** at start-up, plus per-domain data migrations.
  - *Why:* fast iteration while the schema was settling.
  - *Cost:* no versioned migrations, no rollback, and **no database foreign
    keys** - integrity lives in code (see [ERD.md](ERD.md)). Moving to versioned
    SQL migrations (goose / atlas) before production is worth considering.

## 2. Multi-tenancy

- Every request's organisation comes from the **signed access token**, never
  from the request body.
- A **POS-device token is also bound to one branch**. Queries from a till are
  always scoped to its own branch, whatever `branch_id` the client sends.
- Owner and Service Center tokens are org-wide and may pass `branch_id` to
  filter.
- Uniqueness is per organisation (barcode, PO number, phone, role code).

## 3. Identity: logins, staff and approvals

Four kinds of credential:

| Credential | Represents | Lifetime | Sent as |
|---|---|---|---|
| Access token (JWT) | A login: owner, service center, or a POS device | 60 min | `Authorization: Bearer` |
| Refresh token | Same login; stored hashed in `sessions`, rotated on use | 30 days | body of `/auth/refresh` |
| Staff token (JWT) | The **person** at the till, after a PIN; carries their permissions | 12 h | `X-Staff-Token` |
| Manager approval (JWT) | **One action** a manager approved with their PIN | 2 min | `X-Manager-Approval-Token` |

- **Why separate the device from the person:** a till is shared. The device
  logs in once (long-lived); cashiers rotate on it by PIN without retyping
  passwords. Every sale and drawer event is attributed to the person from the
  verified staff token, never to an id the client sends.
- **Approval as an OR-fallback, not a separate flow:** a handler checks "the
  cashier has permission X **or** a valid approval token for X". Discounts,
  no-sale drawer, void, return and exchange all reuse that one pattern.
- **PIN lockout:** repeated wrong PINs lock the staff member for a period
  (`failed_pin_attempts`, `pin_locked_until`).
- **Roles are per organisation** (`org_role_permission`), with Manager and
  Cashier seeded as system roles. Owners manage the rest in Back Office.
- **An Owner has no staff record** (staff belong to a branch; an owner doesn't).
  Where an owner acts directly (expenses, void), the row records a *user* actor
  instead of a staff actor. Drawer events don't support this yet
  (recommendation #19).
- `/internal/*` is protected by a shared secret (`X-Internal-Key`) and only
  called server-to-server by shagan_team.

## 4. Frontends never call the API from the browser

Both Next.js apps follow a **backend-for-frontend** pattern. Server Components
read, Server Functions write, and the browser only ever talks to its own
Next.js server.

```
browser ──(same-origin, cookies)──► Next.js server ──(Bearer, X-Staff-Token)──► Go API
```

**Why**
- **Tokens never reach JavaScript.** Access, refresh and staff tokens live in
  `httpOnly` cookies. An XSS bug can't steal a till's 30-day refresh token.
- **One place to refresh.** `proxy.ts` (Next 16's middleware) renews the access
  token before it expires. The API rotates refresh tokens, so concurrent
  refreshes must be de-duplicated, which is easier in one place.
- **Shapes and secrets are adapted server-side.** Mappers turn the API's
  snake_case / decimal strings into UI models, and fields such as hashes never
  reach the browser.
- **One origin for the browser.** This also matters for WebUSB permissions on
  the till.

**Cost:** an extra hop per request, and the frontend server must be able to
reach the API. Manager approval tokens are the exception: they live in React
state for the one request they authorise.

## 5. Money

- **Backend:** `decimal(10,2)` columns via `shopspring/decimal`. Money crosses
  the API as **decimal strings**, never floats.
- **Server-derived totals.** `POST /sales` prices every line from its product
  and recomputes subtotal, discount, tax and total (`total = subtotal -
  discount`: tax is a % per product that is already inside the price, never
  added) and rejects a sale whose payments don't add up exactly.
- **Till:** works in **integer cents** (`shagan-retail/lib/pos/pricing.ts`) and
  converts only at the edges. An order discount is split across lines in
  proportion to their value, and the rounding remainder goes to the largest
  line, so the shares add up exactly.
- **Snapshots:** each sale line stores its name, unit price and cost at the time
  of sale, so changing the catalog never rewrites history.

## 6. Time zones

- Each organisation has a `timezone` (default `Asia/Yangon`, UTC+06:30).
  Reports ("today", daily buckets) use the organisation's local day, not UTC.
- The Go binary embeds the tz database, so this works on Alpine.
- The retail frontend still hard-codes `Asia/Yangon` for display; it should
  read the organisation's zone once the API exposes it to tenants.

## 7. Offline-first design (backend built, till not yet)

- **Sale ids are UUIDs generated by the till**, so a sale created offline keeps
  its identity when it syncs.
- **Retries are safe because of that id.** Resending `POST /sales` with an id
  already recorded at the same branch returns the existing sale (flagged as a
  replay) instead of creating a second one. `POST /sync/sales` skips a sale
  that has already arrived. (An `idempotency_keys` table exists but no code
  uses it.)
- `/sync/catalog` (with ETag), `/sync/sales` (batch upload), `/sync/status`,
  and **sync conflicts** (e.g. stock went negative) that an owner resolves.
- **Planned till side:** an IndexedDB catalog cache and sales outbox, a service
  worker for the app shell, and strict offline rules (no new PIN sign-ins, no
  approvals, no manual discounts). Not built yet - see [STATUS.md](STATUS.md).

## 8. Audit and stock history

- **`audit_log`:** before/after JSON for every significant change, with actor
  (staff or user) and branch. Shown in Back Office.
- **`inventory_ledger` is append-only.** Every stock movement records its
  quantity, the resulting balance and a reference to its source document
  (sale, return, transfer, goods receipt, ...). `stock_levels` holds the
  current quantity; the ledger is the explanation.

## 9. Files

Product images and payment QR codes go to **MinIO / S3**. The API returns
**presigned URLs valid for 15 minutes**. Clients must not cache them longer,
and the retail app uses plain `<img>` for them, not Next's image optimiser.

## 10. Till hardware

The till is a **Duotu C5** running Chrome on Android. The web app prints
receipts and opens the cash drawer itself over **WebUSB**, sending ESC/POS
commands. There is no native app and no print server.

- Myanmar and Chinese text the printer's code page can't show is drawn on a
  canvas and sent as a raster image.
- The drawer is kicked through the printer, behind a `DrawerTransport`
  interface, so an Android WebView bridge can replace it on units wired
  differently.
- **Requires HTTPS** in production, and a one-time USB pairing tap per device.

Details: [shagan-retail docs/hardware.md](https://github.com/thiri-kyaw-khaing/shagan-retail/blob/main/docs/hardware.md).

## 11. Internationalisation

UI strings are in typed en / mm / zh dictionaries in each frontend
(`lib/i18n/dictionaries`); a missing key is a compile error. Category names are
stored per language (`name_i18n`). Receipts print English text directly and
rasterise anything non-Latin.

## 12. Testing

| Layer | How |
|---|---|
| Backend services | Unit tests with mockery mocks (~740 tests in total, `go test ./...`) |
| Backend repositories / migrations | In-memory SQLite (needs CGO) |
| Route → permission map | `cmd/api/route_permissions_test.go` |
| Frontends | `tsc --noEmit` + ESLint; manual journeys against a seeded backend. **No automated frontend tests yet.** |
| End to end | Postman "UAT - Full User Flow" folder |

Repository tests run on SQLite while production runs on Postgres, so
Postgres-specific behaviour (`jsonb`, `text[]`, locking) isn't covered by the
test suite.
