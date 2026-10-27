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
| 23 | Branch-specific product prices | **Feature (decided)** | Selling one catalog at different prices per branch |
| 24 | Subscription tiers (Basic / Pro / Enterprise) | **Feature (decided)** | Charging by shop size; gating the next features |

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

## 23. Branch-specific product prices (decided 2026-10-23)
**Status: built 2026-10-23** - see WORKFLOWS.md Section 8 ("Branch prices") for the rules as built, including the few places they differ from the defaults below.

**Need:** the same catalog is sold at different prices in different branches
(e.g. Mandalay costs more than Yangon). Today a product has one org-wide
`price` / `discount_percent`, and an online sale whose `unit_price` differs from
it is rejected with **409 "price changed, refresh the catalog"**
(`internal/sales/sales_discount_service_impl.go`, the `default:` case). So this
can't be solved in the frontend.

**Chosen design: branch price overrides** (option B of the four considered;
rejected: duplicating products per branch, price lists, per-branch
multipliers). The catalog price stays the default; a branch may override it for
any product. No data migration: with no override rows, every branch keeps
today's behaviour.

> **Effective price at branch B** = B's override for the product if one exists,
> otherwise the catalog price. Each override field falls back on its own.

### Defaults assumed (change before building if wrong)
| Question | Default |
|---|---|
| What can differ per branch | **Selling `price`** and the catalog **`discount_percent`**. `tax_percent` and `cost_price` stay org-wide. |
| Combos | Out of scope; combo prices stay org-wide (possible follow-up). |
| Scheduled price changes ("from 1 Nov") | Out of scope; an override applies immediately. |
| Per-branch "not sold here" | Out of scope (`is_active` stays org-wide). |
| Bulk edit / CSV import | Out of scope for v1. |

### Schema
```go
// internal/catalog/catalog_model.go
type ProductBranchPrice struct {
    ID              uint             `gorm:"primaryKey;autoIncrement" json:"id"`
    OrgID           uint             `gorm:"not null;index" json:"org_id"`
    ProductID       uint             `gorm:"not null;uniqueIndex:ux_product_branch_prices" json:"product_id"`
    BranchID        uint             `gorm:"not null;uniqueIndex:ux_product_branch_prices;index" json:"branch_id"`
    Price           *decimal.Decimal `gorm:"type:decimal(10,2)" json:"price"`             // nil = catalog price
    DiscountPercent *decimal.Decimal `gorm:"type:decimal(5,2)" json:"discount_percent"`   // nil = catalog %
    SalePrice       decimal.Decimal  `gorm:"type:decimal(10,2);not null" json:"sale_price"` // computed on write, like Product.SalePrice
    UpdatedByUserID *uint            `json:"updated_by_user_id"`
    UpdatedAt       time.Time        `gorm:"autoUpdateTime;not null" json:"updated_at"`
}
```
- Unique on `(product_id, branch_id)`. Writes are upserts (`ON CONFLICT`).
- `sale_price` is computed and rounded on write with the same rule as
  `Product.SalePrice` (`common.DiscountedUnitPrice`), so a sale and an exchange
  at that branch can never disagree by a rounding step. When the catalog
  `price` or `discount_percent` changes, recompute the `sale_price` of
  overrides that inherit the changed field.
- At least one of `price` / `discount_percent` must be set. A row with both
  nil is deleted instead.
- Validation matches the product's: `price > 0`, `0 <= discount_percent <= 100`.
  The branch must be active and in the caller's org.
- Deleting a product deletes its overrides (in the same transaction, since
  there are no DB foreign keys - see #22).

### One resolver, used everywhere a product is priced
Add to catalog something like
`EffectivePricing(ctx, orgID, branchID, product) (price, discountPercent)`, or
a batch form for lists. It returns the override's fields where set, otherwise
the product's. The existing `Product.EffectiveSalePrice()`
(`catalog_model.go:67`) becomes "sale price **at a branch**".

Every path that prices a product today reads the org-wide fields and must use
the resolver instead:

| Path | Where | Change |
|---|---|---|
| Create sale, online | `sales_discount_service_impl.go` (`original := product.Price`; the `default:` case's 409 check and `catalogSalePrice(product)`) | Compare and price against the **selling branch's** effective price. The branch comes from the POS token. |
| Create sale, offline queued | Same file, `queued` case: the `PriceMismatches` check | Compare the till's snapshot with the branch's effective price, not the catalog's. |
| Discount approvals | Same file: approval creation (`GetProduct` at ~:238; `UnitPrice: product.Price` at ~:294) | Base the approved percentage on the branch price. |
| Exchange "out" items | `internal/returns/returns_service_impl.go` ~:512 (`product.EffectiveSalePrice()`) | Price the replacement at the **till's branch** (where the exchange happens). |
| Product reads | `catalog_service_impl.go` `ListProducts` / `GetProduct`; `cmd/api/catalog.go` `GET /products`, `/products/:id`, `/products/barcode/:code` | For a POS token, return the token's branch's effective `price`, `discount_percent` and `sale_price`. For an owner, accept `?branch_id=` and do the same. Without it, return catalog prices. |
| Offline catalog | `internal/datasync/datasync_service_impl.go` `GetCatalogSnapshot` (ignores `branch_id` today) | Build the snapshot for the caller's branch, and include the branch in the **ETag**. Otherwise a till keeps another branch's cached prices (`304 Not Modified`). |

Unchanged, because sale lines already snapshot the price they were sold at:
receipts, reprint, returns, voids, the "in" side of exchanges, and reports.

### API for the Owner / Service Center
```
GET    /products/:id/branch-prices            → [{branch_id, branch_name, price|null, discount_percent|null, effective_price, effective_sale_price}]
PUT    /products/:id/branch-prices/:branchId  → { "price": "1900.00", "discount_percent": null }   upsert
DELETE /products/:id/branch-prices/:branchId  → back to the catalog price
```
- Product responses gain `"price_source": "catalog" | "branch"` when resolved
  for a branch, so Back Office can mark overridden prices.
- **Permissions:** writes are Owner / Service Center only, like other catalog
  writes. A manager in Back Office at a till can read their own branch's
  overrides. Whether they can edit them is a business decision; default no.
- **Audit:** every upsert or delete writes `audit_log` with before / after and
  the branch.

### Tests to add
- Effective-price resolution: no override, price only, % only, both.
- An online sale at the catalog price is rejected at a branch that has an
  override (409), and accepted at the override price. The other branch is
  unaffected.
- A queued (offline) sale flags a `PriceMismatch` against the branch price.
- A discount approval and an exchange "out" item use the branch price.
- `/sync/catalog`: different branches get different prices and ETags.
- Deleting a product removes its overrides. A cross-org branch id is rejected.

### Frontend follow-up (shagan-retail, once the API exists)
- Back Office product form: a "Prices by branch" table showing each branch's
  default and an optional override (blank = default).
- The product list shows the selected branch's price with a "branch price"
  badge.
- **The till needs no change.** It shows whatever price the API returns for its
  branch.

## 24. Subscription tiers (decided 2026-10-27)
**Status: built 2026-10-27** - see WORKFLOWS.md Section 2 ("Subscription tiers") for the rules as built and where they differ from the defaults below.

**Need:** Shagan sells the POS in tiers. Everything built so far is the
**Basic** tier; higher tiers add size (more branches and tills) and features
that aren't built yet. The Shagan team (shagan_team, over `/internal`) sets
each organization's tier, alongside the suspend it already controls.

**The tier belongs to the organization**, not to a POS account: every login of
that org (owner, service center, POS devices, staff) gets it.

> **Status and tier are separate.** `status` (active / suspended, already
> built) decides whether the org can use Shagan at all. `plan` decides which
> features and how many branches / devices. An expired or unpaid tier never
> suspends an org or stops a till from selling - it drops the org to Basic.

### The tiers
| | **Basic** | **Pro** | **Enterprise** |
|---|---|---|---|
| Active branches | 1 | 5 | unlimited |
| Active devices | 2 | 10 | unlimited |
| Everything built today | yes | yes | yes |
| Offline mode (when built) | yes | yes | yes |
| `customer_display` | - | yes | yes |
| `advanced_reports`, `report_export` | - | yes | yes |
| `promotions` (incl. near-expiry markdowns) | - | yes | yes |
| `loyalty`, `sms_receipts` | - | - | yes |
| `api_access` | - | - | yes |

Only the limits apply today: none of the gated features exists yet
(`POST /reports/export` is still a 501 stub). Each one is gated when it's built.

### Defaults assumed (change before building if wrong)
| Question | Default |
|---|---|
| Current features | All in every tier, including branch pricing and custom roles. (Moving either to Pro is easiest **before** the first paying customer.) |
| New organizations | Start on a **14-day Pro trial**, then drop to Basic. |
| Expiry | A paid plan with `plan_expires_at` keeps working for a **7-day grace period**, then the org drops to Basic. No suspension. |
| Over the limit after a downgrade | Nothing is deleted or switched off. Existing branches and devices keep working; creating or reactivating more is blocked. shagan_team shows the org as "over limit". |
| Who the limits apply to | Everyone, including `/internal` creates: the team raises the tier or adds extras first. One rule, no hidden bypass. |
| Extra branches / devices | Per-org add-ons (`extra_branches`, `extra_devices`) on top of the tier, so a 6-branch shop needn't jump to Enterprise. |
| Billing | Not in the POS backend. Payments and invoices live in shagan_team; the team sets the tier by hand. |
| Existing organizations | Set to Basic, no trial. |

### Schema
```go
// internal/identity/identity_model.go - Organization gains:
Plan           string     `gorm:"size:30;not null;default:'basic'" json:"plan"`
PlanExpiresAt  *time.Time `json:"plan_expires_at"` // nil = no end date
PlanTrial      bool       `gorm:"not null;default:false" json:"plan_trial"`
ExtraBranches  int        `gorm:"not null;default:0" json:"extra_branches"`
ExtraDevices   int        `gorm:"not null;default:0" json:"extra_devices"`

// internal/subscription/subscription_model.go
type OrgFeatureOverride struct { // a deal or beta: switch one feature on/off for one org
    ID        uint      `gorm:"primaryKey;autoIncrement" json:"id"`
    OrgID     uint      `gorm:"not null;uniqueIndex:ux_org_feature" json:"org_id"`
    Feature   string    `gorm:"size:50;not null;uniqueIndex:ux_org_feature" json:"feature"`
    Enabled   bool      `gorm:"not null" json:"enabled"`
    UpdatedAt time.Time `gorm:"autoUpdateTime;not null" json:"updated_at"`
}

type OrgPlanChange struct { // append-only history, shown in shagan_team
    ID            uint       `gorm:"primaryKey;autoIncrement" json:"id"`
    OrgID         uint       `gorm:"not null;index" json:"org_id"`
    FromPlan      string     `gorm:"size:30;not null" json:"from_plan"`
    ToPlan        string     `gorm:"size:30;not null" json:"to_plan"`
    ExpiresAt     *time.Time `json:"expires_at"`
    Trial         bool       `gorm:"not null" json:"trial"`
    ExtraBranches int        `gorm:"not null" json:"extra_branches"`
    ExtraDevices  int        `gorm:"not null" json:"extra_devices"`
    Note          string     `gorm:"size:500" json:"note"`
    ChangedBy     string     `gorm:"size:255;not null" json:"changed_by"` // shagan_team admin email (internal auth has no user)
    CreatedAt     time.Time  `gorm:"autoCreateTime;not null" json:"created_at"`
}
```
- The tiers themselves are **defined in code** (a new `internal/subscription`
  package): plan code → features and limits. Features are code anyway, and
  there are only three tiers.
- Plan codes are validated against that list on write.

### One resolver
`subscription.Entitlements(ctx, orgID, now)` returns the **effective** plan,
features and limits:
1. Start from `org.Plan`.
2. If `plan_expires_at` has passed: within 7 days → keep the plan with
   `in_grace: true`; after that → `basic`. Worked out on read, so **no cron
   job** is needed.
3. Apply `org_feature_overrides`.
4. Limits = the plan's limit + `extra_branches` / `extra_devices`
   (unlimited stays unlimited).

Cache it per org for a short time (e.g. 60 s), and clear the cache when a
`/internal` subscription write happens. **Don't put the plan in the JWT** as
the thing that's enforced: staff tokens live 12 h and device refresh tokens
30 days.

### Enforcement
- **Features:** a `RequireFeature(code)` middleware, next to the ones in
  `internal/middleware/access_permissions.go`. It's added to each gated route
  as that feature is built. It responds 403 with
  `{ "success": false, "code": "feature_not_in_plan", "feature": "...", "plan": "basic", "message": "..." }`.
- **Limits:** these checks count **active** branches / devices of the org:
  - Branches: `POST /branches` (owner), `POST /internal/branches`, and
    reactivating a branch (`PATCH /internal/branches/:id/status` to active).
  - Devices: `POST /devices`, `POST /internal/devices`, and reactivating a
    device.

  Over the limit, they respond 403 with
  `{ "code": "plan_limit_reached", "limit": "branches", "max": 1, "current": 1 }`.
  Do the count in the same transaction as the insert, so two parallel creates
  can't both pass.
- **Never blocked by the plan:** login, selling (`POST /sales`,
  `/sync/sales`), shifts, receipts, returns. A downgrade must not stop a till.

### Exposed to the apps
- `GET /me` gains:
  ```json
  "subscription": {
    "plan": "pro", "trial": true, "expires_at": "...", "in_grace": false,
    "features": ["customer_display", "..."],
    "limits": { "branches": 5, "devices": 10 },
    "usage":  { "branches": 2, "devices": 3 }
  }
  ```
  `null` in a limit means unlimited. This will need a response DTO for `/me`,
  which today returns the `User` model.
- `GET /sync/catalog` includes the same `features`, so an offline till knows
  them. Include the plan in the **ETag**.

### `/internal` API for shagan_team
```
GET    /internal/plans                              → the tier catalog (codes, names, features, limits)
GET    /internal/organizations/:id/subscription     → effective entitlements + usage + raw fields + overrides + history
PATCH  /internal/organizations/:id/subscription     → { "plan", "expires_at"|null, "trial", "extra_branches", "extra_devices", "note", "changed_by" }
PUT    /internal/organizations/:id/features/:code   → { "enabled": true }      override one feature
DELETE /internal/organizations/:id/features/:code   → back to the plan's default
```
- `GET /internal/organizations` (and `/:id`) also return `plan`,
  `plan_expires_at`, `plan_trial` and an `over_limit` flag, so the team can
  filter by tier.
- `POST /internal/accounts` (create organization) starts the 14-day Pro trial.
- Every subscription write appends an `OrgPlanChange` row, and also writes
  `audit_log` (before / after).

### Migration
A named data migration (like `2026-10-24-manager-manage-branch-prices`) adds
the columns with their defaults. Existing orgs become `basic` with no trial.

### Tests to add
- Entitlements: each tier; expiry inside and after the grace period; a trial
  ending; a feature override on and off; extras added to a limit; unlimited.
- Limits: create branch / device at the limit is refused (owner and
  `/internal`); reactivation is refused at the limit; suspended ones don't
  count; two parallel creates can't both pass.
- A downgrade leaves existing branches and devices working, and a sale at an
  over-limit branch still succeeds.
- `RequireFeature` returns `feature_not_in_plan` (test it on a dummy route
  until a real gated feature exists).
- `/me` and `/sync/catalog` show the entitlements; the ETag changes with the
  plan.
- A new org starts on the Pro trial; every subscription change writes history.

### Frontend follow-up (once the API exists)
- **shagan_team:**
  - A tier badge, trial / expiry and an "over limit" warning on the buyer and
    organization pages.
  - A "Change tier" dialog (plan, expiry date, extras, note) and the change
    history.
  - A tier filter on the buyers list.
- **shagan-retail:**
  - Read `subscription` from `/me`.
  - Gated items show a "Pro" badge and an upgrade message instead of
    disappearing.
  - At a limit, "Add branch / device" explains why it's blocked.
