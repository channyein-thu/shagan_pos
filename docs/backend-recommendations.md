# shagan_pos - backend change requests from frontend integration

Raised while integrating shagan-retail and shagan_team (first draft
2026-10-08, updated 2026-10-23). Each item is written so it can be pasted as a
GitHub issue. Paths are relative to the repo root. See the status section and
[STATUS.md](STATUS.md) for what has been done since.

| # | Title | Severity | Blocks |
|---|---|---|---|
| 1 | Stop serializing password/PIN hashes | **Security** | — (live leak) |
| 2 | Enforce authorization by account type / permission | **Security** | — |
| 3 | Return combo items from `/combos` and `/sync/catalog` | Functional | POS combos (Phase 4), offline (Phase 7) |
| 6 | Owner path for expenses and void | Functional (decided) | Owner "Add expense" / "Void" buttons |
| 4 | Make `POST /sales` idempotent on a duplicate id | Functional | Safe retries (Phase 4) |
| 5 | `/sync/sales` trusts body `staff_id` and skips discount check | Security | Offline (Phase 7) |
| 9 | Reports "today" uses the UTC day | Correctness | Dashboard accuracy for Myanmar (UTC+6:30) |
| 7 | Put `account_type` in the login response or JWT | Nice to have | — |
| 8 | Tighten CORS | Hardening | — |
| 10 | Update WORKFLOWS.md | Docs | — |
| 11 | `/internal` extras for shagan_team | Nice to have | shagan_team org list / owner account mgmt |
| 12 | Payment method on sale lists | Functional | Sales history / report "Method" column |
| 13 | Filters and pagination on `GET /sales` | Scale | Sales history once volume grows |
| 14 | Product delete orphans sales and stock rows | Data integrity | — |
| 15 | Stock transfers: return items, accept a note | Functional | Transfers list detail; transfer reason |
| 16 | Receipt show/hide toggles | Nice to have | Receipt layout options (decided to drop for now) |
| 17 | Catalog discounts count as manual discounts | Functional | Plain cashiers need a manager for every discounted product |
| 18 | Expose the org timezone to tenants | Nice to have | Frontend hardcodes Asia/Yangon |
| 19 | Owner path for drawer events | Should fix | Owner/Back Office drawer opens go unrecorded |
| 20 | Wrong table name in a procurement migration | Bug | Old databases being migrated |
| 21 | No unique index on `stock_levels (product_id, branch_id)` | Data integrity | — |
| 22 | No database foreign keys | Data integrity (design) | — |

---

## 1. Stop serializing password and PIN hashes
**Problem.** `User.CredentialHash` (`json:"credential_hash"`) and `Staff.PinHash`
(`json:"pin_hash"`, `internal/identity/identity_model.go:139`) are serialized in
every response that returns those models: `GET /me`, `GET /staff`,
`GET /branches/:id/staff`, both PIN-verify responses, and the `/internal/*`
account responses. Any client — including a POS device — receives bcrypt hashes.
A 6-digit PIN hash is brute-forceable offline in seconds.

**Fix.** Change both tags to `json:"-"` (or return response DTOs). No frontend
depends on these fields; shagan-retail strips them as a stopgap
(`lib/api/backend.ts`).

## 2. Enforce authorization by account type / permission
**Problem.** Every `/api/v1` route except login/refresh only runs
`middleware.Auth` (`cmd/main.go:61-64`). There is no account-type check, so a
**POS device** token can call owner-only admin routes: create/edit products,
`POST /staff` / `PATCH /staff/:id` (no permission gate at all — contradicts
WORKFLOWS §4 "Manager: staff view-only"), `POST /branches`, `POST /devices`, etc.
Separately, `GET /branches/:id/staff` checks the branch is in the org but not
that it matches a POS token's own branch
(`internal/identity/identity_repository_impl.go:260-271`).

**Fix.** Add an account-type middleware (e.g. `RequireAccountType("owner")`) on
admin routes, and restrict branch-scoped reads to the token's `branch_id` when
present. Needs an `account_type` claim in the access JWT (see #7) to avoid a DB
read per request.

## 3. Return combo items
**Problem.** `ComboResult` (`internal/catalog/catalog_dto.go:159-162`) has no
`items`, and neither does `GET /sync/catalog`. WORKFLOWS §8 says the POS expands
a combo client-side into one line per component product — it can't without the
component list.

**Fix.** Add `items: [{product_id, qty}]` to `ComboResult` (list, get, and the
sync catalog snapshot). Note this changes the catalog ETag, which is fine.

## 4. Make `POST /sales` idempotent
**Problem.** WORKFLOWS §10 says re-submitting a sale with the same client UUID is
safe, but the live path does a plain insert; a duplicate id becomes a 500
(`internal/sales/sales_repository_impl.go:44-46`). Only `POST /sync/sales`
skips duplicates. A till that times out after the server committed can't safely
retry the live call.

**Fix.** On a duplicate primary key, return the existing sale (200) if it
belongs to the same org/device, else 409.

## 5. `/sync/sales` trusts the client's `staff_id` and skips the discount check
**Problem.** `POST /sync/sales` takes `staff_id` from each sale's body without
verification and always allows manual discounts
(`internal/datasync/datasync_service_impl.go:135`). Any POS token can attribute
sales to any staff member and apply unauthorized discounts via the sync path.
Product decision already made (2026-10-06): offline tills allow **no** manual
discounts and no new PIN sign-ins.

**Fix (one of).** (a) Require each queued sale to carry the staff token that was
valid when it was rung up (verify signature; accept an expired-but-valid-at-
`completed_at` token), or (b) at minimum reject queued sales with any discount
> 0, matching the offline rule, and validate `staff_id` belongs to the device's
branch.

## 6. Owner path for expenses and void (decided 2026-10-06)
**Problem.** `POST /expenses`, `PATCH/DELETE /expenses/:id` and
`POST /sales/:id/void` require `X-Staff-Token` (`cmd/api/shift.go:316-328`,
`cmd/api/returns.go:56-85`). The Owner has no Staff record and can't have one
without a branch (`Staff.branch_id` is `not null`,
`internal/identity/identity_model.go:136`). Decision: the owner acts **as the
owner**, with no PIN.

**Fix.** On those four routes, accept an **owner** bearer token in place of
`X-Staff-Token`: treat the owner as holding `approve_void` and
`access_backoffice`; take `branch_id` from the body (expenses already do when the
token has no branch, `shift.go:325`); record the owner user as the actor in the
audit log. `Expense.created_by` is a staff id today, so it needs an owner-user
actor column or nullable `created_by` + `created_by_user_id`. Void keeps its
existing "shift must still be open" rule
(`internal/returns/returns_service_impl.go:98`).

## 7. Put `account_type` in the login response or JWT
`POST /auth/login` returns only `{access_token, refresh_token, expires_at}`
(`internal/identity/identity_dto.go:190-194`) and the access JWT has no
`account_type` claim (`internal/authtoken/authtoken.go:17-22`), so every client
must call `/me` after login to know where to route. Adding the claim also
enables #2. Also note `expires_at` is the *refresh* token's expiry; consider
adding `access_expires_at`.

## 8. Tighten CORS
`internal/middleware/cors.go:6-19` allows origin `*` and only the headers
`Authorization, Content-Type`. shagan-retail doesn't need CORS (it proxies
server-side), so: replace `*` with an explicit origin list from env. Only if a
browser client will call the API directly, also allow `X-Staff-Token`,
`X-Manager-Approval-Token`, `If-None-Match` and expose `ETag`.

## 9. Reports "today" is the UTC day
`internal/reports/reports_service_impl.go:56-57` computes "today" in UTC. For
Myanmar (UTC+6:30) the dashboard's "today" rolls over at 06:30 local time.
Same question for `from`/`to` date ranges. **Fix:** use an org (or branch)
timezone setting, defaulting to `Asia/Yangon`.

## 10. Update WORKFLOWS.md
Line ~99 says the Returns domain "doesn't exist yet" and line ~418 says
datasync is "still 0% built"; both are fully implemented with tests. Move the
void/return/exchange wiring into the confirmed sections and record the decisions
made on 2026-10-06 (owner acts as owner — #6; strict offline rules — #5; manager
Back Office = owner screens scoped to the till's branch).

## 11. `/internal` extras for shagan_team
- `GET /internal/organizations/:id` (the portal currently lists all orgs and filters).
- List / reset-password for owner and service_center accounts (only POS accounts are covered).
- Branch and device status updates (no internal endpoint for either).

## 12. Payment method on sale lists
`GET /sales` and `GET /reports/transactions` return each sale's total and
status but not how it was paid; that's only in `GET /sales/:id/receipt`
(`internal/sales/sales_service_impl.go:236-250`). The Back Office sales
history and the report's transaction list both had a Method column (cash / QR /
split) in the design, now replaced by Status. **Fix:** add
`payment_methods: ["cash","qr"]` (or the per-method amounts) to each row of both
responses.

## 13. Filters and pagination on `GET /sales`
`GET /sales` takes no query params at all — no date range, no branch, no
pagination — and returns the org's entire history
(`internal/sales/sales_repository_impl.go:59-65`). The Back Office filters it
client-side today. **Fix:** accept `branch_id`, `from`, `to`, `page`,
`page_size` (same conventions as `/reports/transactions`).

## 14. Product delete orphans sales and stock rows
`DELETE /products/:id` is a hard delete that only checks combos
(`internal/catalog/catalog_service_impl.go:366-401`). Sale items, stock levels,
ledger entries and PO lines that reference the product are left pointing at a
row that no longer exists, so history screens lose the product name. **Fix:**
block the delete (409) when the product has any sales, stock or PO history, and
offer deactivation (`is_active=false`) instead - or make it a soft delete.

## 15. Stock transfers: return items, accept a note
`GET /stock-transfers` (and the create/PATCH responses) omit the transfer's
items, so the Back Office's open-transfers list can show branches and status
but not what is being moved. The create body also has no reason/note field, so
the Transfer form's "Reason" was dropped. **Fix:** include
`items: [{product_id, qty}]` in transfer responses and accept an optional
`note` on create.

## 16. Receipt show/hide toggles
The receipt design had "show address / phone / thank-you message" toggles; the
backend has no columns for them and requires all four text fields, so every
receipt prints all four (decided 2026-10-08). If the layout options are wanted
later: add `show_address`, `show_phone`, `show_thank_you` booleans to
`receipt_settings` and relax the required tags accordingly.

## Status update (2026-10-08, after the backend changes)

Done: #1, #3 (list + sync), #4, #5, #6 (expenses + void), #7, #8, #9 (backend),
#10, #12, #13, #14, #15. Partial: #2 (reads still open to POS tokens -
WORKFLOWS §12.6). Not done: #11 partly (org-wide accounts now listable), #16.

## 17. Catalog discounts count as manual discounts
`Product.discount` is a catalog field the owner sets, and the till applies it
automatically (decided 2026-10-08). But `CreateSale` treats *any* line discount
> 0 as a manual discount needing `apply_manual_discount`
(`internal/sales/sales_service_impl.go:99`), so a plain cashier (`staff` role)
needs a manager's PIN for every sale containing a discounted product, and such
sales can never be queued offline (`/sync/sales` rejects any discount).
**Fix:** let the server recognise the catalog part - e.g. compute it from
`Product.discount × qty` and only require the permission for the amount above
that (or add a separate `catalog_discount` field on the sale item).

## 18. Expose the org timezone to tenants
Reports now use the org's timezone (#9), but no tenant endpoint returns it, so
the frontend pins `Asia/Yangon` for "today" and for formatting timestamps.
**Fix:** include `timezone` in `GET /me` (or a small `GET /organization`).

## 19. Owner path for drawer events
The Back Office "Open Drawer" button now really opens the drawer (through the
till's receipt printer). A manager who unlocked Back Office at a till is
recorded as usual - `POST /drawer-events` with their own staff token, on the
till's open shift. The Owner can't be: the route requires `X-Staff-Token`
(`cmd/api/shift.go:40`) and an open `shift_id`, and an owner login has
neither, so the frontend opens the drawer **unrecorded** for the Owner.
**Fix:** same shape as #6 - accept an Owner / Service Center bearer token on
`POST /drawer-events` (`RequireStaffTokenOrOrgAdmin`), record the owner user
as the actor (nullable `staff_id` + a `user_id`), and let `shift_id` be
optional for them (with a `branch_id` instead), since an owner may open a
drawer while no shift is running.

## 20. Wrong table name in a procurement migration
`internal/procurement/procurement_migrate.go:57` runs
`DELETE FROM goods_receipt_items ...`, but the table is `goods_receipts_items`
(`procurement_model.go:82`, `TableName()`). The statement only runs on an old
database whose `purchase_orders` has no `branch_id` column and has rows; there
it fails with "relation does not exist" and aborts start-up.
**Fix:** use `goods_receipts_items` (or `db.Migrator()` / the model's table name).

## 21. No unique index on `stock_levels (product_id, branch_id)`
`StockLevel` (`internal/inventory/inventory_model.go:55`) indexes `product_id`
and `branch_id` separately. Two concurrent first movements for a product at a
branch can create two stock rows, and quantities then split between them.
**Fix:** a composite unique index on `(product_id, branch_id)`, with upserts
(`ON CONFLICT`) where stock rows are created.

## 22. No database foreign keys
No model declares associations, and `internal/migrate/migrate.go` documents
that no FK constraints are created. Referential integrity depends on every
code path (and every manual fix in the database) being right.
**Fix (suggested):** add FKs at least for the money tables (`sale_items`,
`payments`, `voids`, `returns`, `exchanges` → `sales`), ideally as part of
moving to versioned SQL migrations.
