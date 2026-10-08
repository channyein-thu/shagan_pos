# Shagan POS — Workflows & Business Rules

This is the single source of truth for confirmed business rules and
end-to-end workflows. If you're about to build or change behavior in any
domain, read the relevant section here first. Where something is genuinely
undecided, it's listed in **Section 12 — Open Questions** instead of guessed
at. If you resolve one of those, move it into the confirmed sections and
delete it from Section 12.

Last confirmed: 2026-09-27.

---

## 1. Roles & Accounts

Two different identity mechanisms exist, and they are not interchangeable:

### Org-level accounts (`identity.User`, email + password)
- **Owner** — org-wide, no branch. Full email/password login, own session.
- **Service Center** — org-wide, no branch. Same login shape as Owner.
- **POS device account** — bound to exactly one branch + one device. This is
  what a physical till itself logs in as (`account_type: "pos"`).

### Branch-level Staff (`identity.Staff`, 6-digit PIN)
Every Staff record belongs to exactly one branch and has a `Role`. Manager
and Staff (cashier) are **the same login mechanism** — a Manager is not a
separate account type, just a Staff record whose role grants more
permissions. They are told apart only by which PIN is entered at the same
POS device/session.

Seeded roles and their granted permissions (`internal/seed/seed.go`):

| Role code | Permissions granted |
|---|---|
| `staff` | `access_pos_portal` |
| `super_staff` | `access_pos_portal`, `open_drawer_no_sale`, `apply_manual_discount` |
| `manager` | `access_pos_portal`, `access_backoffice`, `apply_manual_discount`, `approve_void`, `approve_return`, `approve_exchange` |

Note `open_drawer_no_sale` is granted to `super_staff` but **not**
`manager` by default — a manager who needs to open the drawer without a
sale still goes through the same approval flow as anyone else lacking it
(see Section 2's manager-approval mechanism; `VerifyManagerPIN` isn't
restricted to role=`manager`, it's granted to whichever staff's role
actually has the requested permission).

> **Role naming — deferred on purpose.** The live back-office prototype's
> "Add Staff" form shows 4 roles (`Cashier` / `Senior Cashier` / `Supervisor`
> / `Manager`) that don't match these 3 seeded role codes in name or count.
> Confirmed decision: leave as-is for now, revisit in a future version. Do
> not build a mapping between them without asking first.

---

## 2. Authentication Mechanisms

Three distinct tokens exist, often needed together on one request:

1. **Bearer access token** (`Authorization: Bearer ...`, `identity.Service.Login`)
   Identifies which org/device is calling. Carries `org_id`, and `branch_id`
   only for a pos-device token (`nil` for Owner/Service Center — this is how
   org-wide scope "just falls out" for Owner without any role-specific
   branching in the handler).

2. **`X-Staff-Token`** (`VerifyStaffPIN`)
   Identifies *which staff member* is signed in for a shift — the PIN a
   cashier or manager enters at a till. Carries `StaffID`, `RoleID`,
   `Permissions` (the role's full permission list at issuance time — a
   later grant/revoke only takes effect on the next PIN verify, not
   immediately). Validated by `middleware.RequireStaffToken` when a handler
   just needs to know *who*, not gate on one specific permission.

3. **`X-Manager-Approval-Token`** (also issued by `VerifyManagerPIN`, same
   token shape as #2, but a separate header)
   Short-lived (2 minutes), single-action elevation: proves "someone whose
   role grants permission X approved this one action just now" — not "who's
   signed in for the shift" (that's still #2, unchanged, on the same
   request). Checked via `middleware.ManagerApproved(c, jwtSecret,
   permission)`. **Now wired in** (see below) — a cashier lacking a
   permission themselves can proceed if this header carries a valid
   approval for it instead.

### Which middleware/helper, when
- `middleware.RequireStaffToken(jwtSecret)` — used on: `OpenShift`,
  `CloseShift`, `CreateDrawerEvent`, `CreateSale`. (`CreateExpense`,
  `UpdateExpense`, `DeleteExpense` and `VoidSale` use
  `RequireStaffTokenOrOrgAdmin` instead — same, but an Owner / Service Center
  bearer token may pass with no staff token; see "Owner acts as owner" below.)
  These need to know who's acting, but don't
  hard-gate on one fixed permission via middleware — the handler makes its
  own permission decision inline (see below).
- `middleware.RequirePermission(jwtSecret, "access_backoffice")` — used on:
  `ForceCloseShift` (Manager-only, hard gate, no exceptions below it, and
  no manager-approval escape hatch either - this one has no fallback).
- `middleware.ManagerApproved(c, jwtSecret, permission)` — an
  **OR-fallback**, not a gate: a handler checks
  `StaffHasPermission(c, X) || ManagerApproved(c, jwtSecret, X)` wherever a
  staff member without their own permission should still be able to proceed
  with someone else's approval. Currently wired into:
  - `CreateSale` → `apply_manual_discount` (any item discount)
  - `CreateDrawerEvent` → `open_drawer_no_sale` (only when `sale_id` is
    omitted - a drawer event tied to a real sale needs no such check)
  - **Not yet wired** (the domains don't exist yet): void/return/exchange
    approval, once Returns is built - use the exact same
    `StaffHasPermission || ManagerApproved` pattern against
    `approve_void`/`approve_return`/`approve_exchange`, don't invent a
    different shape.

### Route authorization by account type (confirmed 2026-10-08)

The access JWT carries an `account_type` claim (`owner` / `pos` /
`service_center`), and `POST /auth/login` / `POST /auth/refresh` return
`account_type` and `access_expires_at` (the access token's expiry;
`expires_at` is still the *refresh* token's). A token issued before the claim
existed gets **401** on any gated route (re-login / refresh), never 403.

| Gate (`internal/middleware/access.go`) | Who passes | Routes |
|---|---|---|
| `RequireOrgAdmin` | Owner, Service Center (never a till, manager PIN or not) | `POST /staff`, `PATCH /staff/:id`, `POST/PATCH /branches`, `POST/PATCH /devices` |
| `RequireBackOffice` | Owner, Service Center, **or** a POS-device token + `X-Staff-Token` whose role has `access_backoffice` (a manager who unlocked Back Office at the till) | catalog writes (products/categories/combos), supplier writes, `PUT /receipt-settings`, payment-QR upload/delete, `POST /inventory/adjustments`, `POST/PATCH /stock-transfers`, `POST/PATCH /purchase-orders`, `POST /purchase-orders/:id/receipts` |
| (none beyond `Auth`) | any authenticated caller | all reads the till needs to sell (products, stock levels, receipt settings, QR codes, ...), plus the staff-token routes in the list above |

- **Service Center = same as Owner** on every admin route (decision, 2026-10-08).

#### Sales history list (built 2026-10-08)
`GET /sales` is `{sales, page, page_size, total_count}` (the same envelope and
`page`/`page_size` rules as `/reports/transactions`: default 20, max 100), newest
first, filterable by `branch_id`, `from` and `to` (`YYYY-MM-DD`, inclusive, UTC
days). Unlike the reports there is **no default date window** — no filters
means the whole history, paged. A branch-bound POS-device token is locked to its
own branch (a `branch_id` it sends is ignored), exactly like the reports. Every
row, here and in `/reports/transactions`, carries `payment_methods`: the
distinct methods used on the sale, sorted (`["cash"]`, `["qr"]`,
`["cash","qr"]` for a split), never null. Voided sales appear in `/sales`
(status shows it) but not in the reports.

#### Owner acts as owner — expenses and void (confirmed 2026-10-06, built 2026-10-08)
The Owner has no `Staff` record (and can't — `Staff.branch_id` is required),
so these four routes — `POST/PATCH/DELETE /expenses/:id` and
`POST /sales/:id/void` — accept an **Owner or Service Center bearer token
with no `X-Staff-Token`** (`RequireStaffTokenOrOrgAdmin`). A POS-device token
still needs a staff token exactly as before. For the owner:
- **No PIN, no manager approval.** The owner is treated as holding
  `access_backoffice` and `approve_void`; they may edit/delete any expense in
  their org. Any `X-Staff-Token` they send is ignored.
- **Attribution is explicit, never a fake staff id.** `expenses.created_by`
  and `voids.approved_by` are now nullable, with new `created_by_user_id` /
  `approved_by_user_id` columns: exactly one of each pair is set. An
  owner-logged expense reassigned to a staff member (`PATCH created_by`)
  becomes staff-logged. The audit log gets `actor_user_id` (with `actor_id`
  null) for an owner void; the void's inventory-ledger rows leave `actor_id`
  null and point at the void via `reference_id`.
- **Expenses**: `branch_id` is required in the body (an org-wide account has
  no branch of its own); it must be an active branch of the owner's org.
- **Void keeps its rules**: the sale's shift must still be open, no existing
  return/exchange, not already voided (all 409).
- Return and Exchange are **not** opened to the owner (no decision yet).
- **A manager at the till is branch-bound** on top of the gate: stock
  adjustments, purchase orders and transfer *sources* must be the till's own
  branch (403 `cannot act on a different branch`); a purchase order or
  transfer belonging to another branch reads as **404**, and
  `GET /purchase-orders` returns only the till's branch. A manager editing
  products/categories/combos/suppliers affects the whole org - those are
  org-wide records (confirmed).
- `GET /branches/:id/staff` and `/managers` are limited to the till's own
  branch for a POS-device token (403 otherwise).
- `credential_hash` / `pin_hash` are never serialized in any response.
- **Not yet gated (flagged, not decided):** reads of reports, audit log,
  org-wide `GET /staff`, `/sync/conflicts` resolve, `POST /printers/test`
  - a POS token can still call them.

**Rule of thumb:** if a client-supplied `staff_id`/`created_by` field still
exists on a request DTO, it's for wire compatibility only — the server
always overwrites it from the verified token. Never trust it, and never
re-introduce a `binding:"required"` tag on one (a real bug already caused by
this twice: Shift and Sales both originally rejected a client correctly
omitting the field, before the override ever ran).

---

## 3. Owner Journey

1. Shagan team provisions the org (internal-only, `/internal/*` +
   `X-Internal-Key`, no owner login involved) — creates the Organization,
   Owner + Service Center accounts, at least one Branch, at least one
   Device.
2. Owner logs in (email + password) — **no portal picker, no Manager PIN
   gate**. Lands directly on their own dedicated frontend route.
3. That route looks like "Back Office" (Staff Management, Catalog,
   Inventory, Sales History, Reports, etc.) but operates at
   **organization scope** — every branch, not locked to one.
4. Owner can: manage branches/devices, hire/fire staff and assign
   roles+PINs, edit the product catalog, manage suppliers and purchase
   orders, configure receipt/printer/payment-QR settings, view org-wide
   reports and the full audit log.

---

## 4. Manager Journey

A Manager is a Staff record with role `manager` (see Section 1) — not a
separate account. Everything below is **branch-scoped** to wherever that
Staff record belongs.

**Getting into Back Office at a terminal** (confirmed): there are two
frontend routes — POS and Back Office. Entering the Back Office route at a
shared terminal requires a Manager PIN verify to unlock it (this is
`VerifyManagerPIN` with `permission: "access_backoffice"`, or an
equivalent flow built on the same mechanism) — distinct from Owner's own
dedicated web login, which has no such gate (Section 3).

Confirmed capabilities:
- **Staff**: view-only (cannot edit roles/PINs, hire/fire).
- **Sales / Inventory (stock) / Reports**: view, scoped to their own branch.
  (Stock visibility is already schema-supported: `StockLevel` is keyed by
  `{ProductID, BranchID}`.)
- **Expenses**: can log new ones for their branch, and can edit/delete
  **any** staff's expense at their branch (gated by `access_backoffice` —
  see `shift.ExpenseActor.CanManageAny`).
- **Suppliers / Purchase Orders**: suppliers are shared org-wide (not
  branch-owned); Manager creates/manages Purchase Orders and receiving *for
  their own branch* against that shared supplier list. **Not yet
  implemented** (Procurement, Kit's domain) — `PurchaseOrder.OrgID` has
  been added as schema prep (also fixed a real bug: its PO-number unique
  index was silently global instead of per-org), but `BranchID` still
  needs adding before the branch-scoping itself is real.
- **Approvals**: approves another staff's action they lack permission for —
  manual discount, drawer-without-sale, and (once built) void/return/
  exchange — via `VerifyManagerPIN`, which issues the
  `X-Manager-Approval-Token` the other staff's own request then carries
  (see Section 2). Not restricted to role=`manager` specifically - whoever's
  role grants the target permission can approve it.
- **Emergency shift override**: can force-close another staff's shift via
  `POST /shifts/:id/force-close` when that staff is genuinely unavailable
  (e.g. called in sick) — always requires a `reason`, regardless of
  whether the counted cash matches. Recorded distinctly
  (`Shift.ClosedByStaffID` differs from `Shift.StaffID`) from a normal
  close. This one has no manager-approval fallback of its own - it *is*
  the escape hatch, gated by `access_backoffice` directly.
- **Manual discounts**: `apply_manual_discount` permission lets them apply
  an item discount on `CreateSale` directly, without needing anyone else's
  approval.

---

## 5. Staff (Cashier) Journey

Role `staff` or `super_staff` (see Section 1's permission table for exactly
what each grants).

1. PIN sign-in at a POS device (`VerifyStaffPIN`, scoped to that device's
   branch) — issues the `X-Staff-Token` used for everything below.
2. **Opens their own shift** with an opening cash float
   (`POST /shifts`) — the shift always belongs to whichever staff the
   token identifies, never a client-supplied id.
3. Rings sales (`POST /sales`): add items (optional per-item discount,
   optional combo grouping — see below), optional customer lookup/create,
   one or more payments (split payment supported across cash/QR — the only
   2 methods accepted in this version; QR codes are managed per-branch via
   Platform, up to 5 per branch, one per bank). Server derives
   `Subtotal`/`Discount`/`Tax`/`Total`
   from the items — never trusts client-computed totals — and rejects if
   payments don't sum to the derived total.
4. Can see current stock for their branch while ringing up a sale (same
   login/UI as Manager, just gated by which PIN was entered — see Section
   1).
5. **Manual discount rule**: any item discount `> 0` requires the calling
   staff's role to grant `apply_manual_discount`, **or** a valid
   `X-Manager-Approval-Token` granting it instead (a Manager/super_staff
   PIN-approves it at the terminal - see Section 2). Item discounts and
   item tax must both be `>= 0` (400 on negative).
6. **Per-item tax**: each line item snapshots its own tax amount (from
   `Product.Tax`, same offline-first client-caching reasoning as
   `unit_price`/`name_snapshot`) - there's no separate sale-level tax
   input, `Sale.Tax` is the sum across items.
7. **Combos**: ringing up a Combo expands it into one `CreateSaleItemRequest`
   per real product component (each with its own price/tax/a proportional
   share of the discount), tagged with a shared `combo_id` purely for
   receipt/report grouping - see Section 8's design writeup for why.
8. Opens the drawer without a sale — only if their role grants
   `open_drawer_no_sale` (`super_staff` by default, not `manager`), **or**
   a valid manager approval for it; otherwise 403.
9. Void/return/exchange (once Returns exists) will need the equivalent
   `approve_void`/`approve_return`/`approve_exchange` permission or a
   manager approval, same pattern - see Section 9 for the researched
   standard business rules these three should follow.
10. **Closes their own shift** (`POST /shifts/:id/close`) when their stretch
    ends — **only the same staff who opened it may close it**, no
    exception in the normal path. Counts the physical cash drawer; a
    mismatch from the system's expected cash total requires a `reason`.
    Only cash is physically counted — QR payments are trusted to match
    what the system recorded.
11. **Rotation, not a whole-day lock**: a shift is a per-person session, not
    an all-day device lock. Multiple staff share one till across a day by
    each closing their own stretch before the next person opens a new one
    — no code changes needed for this, it already works via the normal
    close/reopen cycle. The one exception that needs Section 4's manager
    override is when a staff member can't come back to close their own
    shift at all.

---

## 6. End-to-End System Workflow

1. **Tenant onboarding** (Shagan-team-only) → Organization → Owner +
   Service Center accounts → Branch(es) → Device(s) → POS account(s) tied
   to each device.
2. **Setup** (Owner) → add Staff (PIN + role + branch), build the Product
   Catalog, add Suppliers, place/receive Purchase Orders (receiving is
   meant to feed the Inventory Ledger as `purchase_receipt` entries —
   Inventory itself isn't built yet).
3. **Daily floor operation** (Staff/Manager) → PIN sign-in → open shift →
   ring sales (cart, payments, optional customer, combos, per-item tax) →
   occasional drawer events / manager-approved exceptions → close shift
   with a real cash count.
4. **Money & stock bookkeeping** → every event lands in one place: the
   Inventory Ledger for stock movement (sale/return/adjustment/
   transfer/receipt — Inventory not built yet, so only manual
   adjustments/transfers are real right now; once built, a completed sale
   decrements stock in the same transaction as the sale itself and is
   blocked outright on insufficient stock - no backorder/negative stock,
   see Section 9), `ShiftReconciliation` for cash, `Expense` for operating
   costs (rent, staff meals, etc., logged per branch).
5. **Back-office oversight** (Owner) → Sales History (with return/
   exchange/void), Reports (confirmed spec, see Section 11), Audit Log
   (who-did-what system-wide), and eventually real profit/loss once
   Sales + a Product cost basis (`CostPrice`, added but unused so far) +
   Expenses (already real) all connect through Reports.

---

## 7. Confirmed Business Rules (Quick Reference)

- **Shift**: opener == closer, no exception, except a Manager's
  `ForceCloseShift` (mandatory `reason` always, even on a cash match).
  One open shift per device at a time; rotation happens via normal
  close/reopen, not a shared open shift.
- **Cash reconciliation**: only cash is physically counted at close;
  `reason` required only when counted cash differs from expected.
- **Expenses**: creator, a Manager (`access_backoffice`), or the Owner /
  Service Center acting directly may edit/delete;
  anyone signed in may create one (always attributed to themselves).
- **Sales discount**: any item discount requires `apply_manual_discount`
  (own, or via manager approval); item discounts and item tax must both be
  `>= 0`.
- **Per-item tax**: snapshotted per line item, summed into `Sale.Tax` - no
  sale-level tax input.
- **Combos**: expanded client-side into normal per-product line items
  tagged with a shared `combo_id` - not a special server-side pricing path.
- **Stock decrement** (once Inventory exists): same transaction as
  `CreateSale`; blocked outright on insufficient stock, no backorder.
- **Procurement** (agreed, not yet built): suppliers are shared/org-wide;
  Purchase Orders and receiving are branch-scoped.
- **Stock visibility**: per-branch, visible to both Staff and Manager
  (same login, gated by PIN only).
- **`PurchaseOrder` numbers**: server-generated (confirmed 2026-10-05), not
  client-supplied - `"PO-%04d"`, sequential per org (`org_id` + `po_number`
  stays unique, not globally unique). Never editable afterward, including
  via `UpdatePurchaseOrder` - letting it change later would reopen the
  collision risk auto-generation exists to close.
- **Products are org-wide, not branch-scoped** (confirmed 2026-10-05,
  overriding an earlier unconfirmed guess baked into the code - a product
  belongs to the whole organization, sellable at any of its branches; the
  same real-world item never needs two separate `Product` rows for two
  branches). Barcode is unique per org (`ux_products_org_barcode`), not
  per branch. Per-branch stock quantity lives separately in
  `StockLevel` (`{ProductID, BranchID}`), independent of the product
  itself. `CostPrice` is likewise one shared, org-wide weighted-average
  figure per product, blended across every branch's receipts/adjustments
  combined - not tracked separately per branch. A branch-specific
  operation (`CreateStockAdjustment`, `CreateStockTransfer`,
  `CreatePurchaseOrder`/`CreateGoodsReceipt`) always takes an explicit,
  client-supplied `branch_id`, verified against the caller's own org -
  never derived from the product.

---

## 8. Combo Pricing at Checkout (Design)

`Combo`/`ComboItem` (Catalog) describe a bundle: several products sold
together at one lump-sum price. The question was how ringing one up
interacts with `SaleItem`, which is always keyed to one real `ProductID`.

**Decision: expand client-side, don't add server-side combo pricing
logic.** When a POS device rings up a Combo, it expands it into one
`CreateSaleItemRequest` per real component product — each carrying its own
`UnitPrice` and `Tax` (snapshotted the same way any standalone product sale
already works), plus a proportional share of the combo's bundle discount
(allocated by each component's normal price × qty, so the line items'
combined total lands exactly on the combo's advertised price), and a shared
`ComboID` tag purely for grouping on receipts/reports.

Why this shape, not a dedicated combo-aware code path:
- **No schema change to pricing/stock mechanics.** Every line item is
  structurally identical to a standalone sale of that product — stock
  decrement (once Inventory exists) and reporting both work per real
  product with zero special-casing.
- **Matches the existing offline-first pattern.** The POS device already
  computes `NameSnapshot`/`UnitPrice` client-side from its local cache
  (it may be ringing up while offline) - expanding a combo the device
  already has cached locally is the same kind of client-side work, not a
  new category of logic.
- `ComboID` is the only schema addition (`SaleItem.ComboID *uint`,
  `CreateSaleItemRequest.ComboID *uint`) — a label, not a pricing
  mechanism.

---

## 9. Returns / Void / Exchange — Standard Business Rules

Researched from how these three concepts work across mainstream POS
systems (Square, Shopify POS, Lightspeed, Clover, etc.) — these are
industry-standard definitions, not invented for this project. Specific
policy numbers (time windows, cash-vs-credit default, whether partial is
allowed at all) are still Shagan's own call — flagged below where that's
true.

### Void
Cancels a sale **before** it's considered final/settled — in practice,
almost always restricted to the **same shift** it was rung up in, since
voiding erases a transaction that may already be reflected in that shift's
totals. A void reverses the **entire** transaction, as if it never
happened — there is no such thing as a "partial void" of a completed sale
(that's what a Return is for). Requires `approve_void` (own or manager
approval) because it retroactively erases a completed sale record.

### Return
Customer brings back item(s) from an **already-completed, settled** sale —
often a different day than the original purchase. Unlike a Void, a Return
does **not** erase the original sale — it creates a new, separate
transaction record that **references** the original, so the original stays
in history exactly as it happened. Standard POS systems support both:
- **Full return** — every item from the original sale.
- **Partial return** — some items, not all (the common case in practice).

A returned item that's resellable typically goes back into stock (ties
directly into Section 7's stock-decrement rule, in reverse). Requires
`approve_return`.

*Still Shagan's call, not a "standard": is there a return time window
(e.g. 7/14/30 days), and is the default refund method cash, original
payment method, or store credit?*

### Exchange
Customer returns item(s) **and** takes different item(s) in the same
visit. Functionally, virtually every POS models this as a Return + a new
Sale happening together, net-settling any price difference — if the new
item(s) cost more, the customer pays the difference; if less, they get a
partial refund or store credit for the difference. Some systems store this
as one combined "Exchange" transaction referencing both the original sale
and the new items; others just chain a Return immediately followed by a
new Sale. Requires `approve_exchange`.

*Still Shagan's call: combined transaction type, or chained
Return-then-Sale? Either is standard practice — pick whichever is simpler
to build against the existing Sale/SaleItem/Payment shape when Returns
gets designed.*

---

## 10. Datasync / Offline Support (Architecture Recommendation)

Confirmed as a real requirement, not just leftover ERD scaffolding — a POS
till needs to keep working (and selling) through a spotty or dropped
connection, and catch up once it's back online. Below is a standard,
proven shape for this, reusing what's already in place rather than
inventing something new.

### What's already compatible
- `Sale.ID` is a **client-generated UUID** — the device can create a sale's
  identity the instant it's rung up, offline or not, with no server
  round-trip needed first. This also makes re-submission naturally
  idempotent: retrying a POST with the same ID is safe.
- `NameSnapshot`/`UnitPrice`/`Tax` on each `SaleItem` are already
  client-cached values, not server lookups at sale time — the device
  doesn't need to be online to know what it's selling.
- `Sale.SyncedAt` exists to mark when a locally-created record was
  confirmed received by the server.

### Implemented rules (confirmed 2026-10-08, from the 2026-10-06 offline decisions)
- **`POST /sales` is idempotent by `id`.** A retry of a sale this same branch
  and device already recorded returns the original sale with **200** (not 201);
  nothing is re-validated or re-written, so no stock moves twice and no fresh
  manager approval is needed. The same id from a different branch/device is
  **409**. (Two retries racing is handled the same way, never a 500.)
- **`POST /sync/sales` trusts a queued sale with nothing it can't prove.** A
  queued sale carries no staff token and an offline till can't get a manager
  approval (no new PIN sign-ins offline), so: any item discount fails that
  sale, and its `staff_id` must be a staff member of the calling device's own
  branch. The already-stored check runs *first*, so retrying an ingested sale
  stays a success even if the staff member has since moved or left; an id
  already stored by another branch/device is a failed item, never a silent
  success. Failures are per-item in the response; the rest of the batch still
  lands.
- **Known gap:** a discount sale rung up *online* whose response was lost
  and which then gets queued offline will be rejected on sync (discount) -
  retry it through `POST /sales` instead, which now succeeds idempotently.
  `price_override` on a sale item has no permission gate on either path
  (unconfirmed whether it should).

### Recommended shape
1. **Local write-ahead queue on the device.** Every mutation created while
   offline (a completed sale, a drawer event, etc.) is appended to a local
   queue with its already-final client-generated ID, not held in memory
   only.
2. **One-way, idempotent batch upload, device → server.** A dedicated
   `POST /sync` (the `datasync` domain, still 0% built) accepts an array of
   queued operations. Each is processed by its own existing idempotent
   create path (e.g. the same `CreateSale` logic) keyed by its client
   UUID — a duplicate submission (retry after a flaky response) is a safe
   no-op, not a duplicate row. The response reports per-item success/
   failure so the device can drop synced items from its local queue and
   retry only the failures.
3. **Periodic reference-data download, server → device.** For a device to
   ring up sales offline at all, it needs a reasonably fresh local cache
   of: Catalog (products/prices/tax), current stock levels (informational
   - see below), customer lookups, and its own staff PIN/permission list.
   This is a **read-sync**, refreshed opportunistically whenever the
   device has connectivity — not a live/real-time subscription.
4. **Conflicts are rarer than they look, for Sales specifically.** Each
   sale has exactly one origin device/staff and is never concurrently
   edited by two parties — so this isn't a classic multi-writer conflict
   problem. The one real cross-device conflict is **stock**: two offline
   devices could both sell the last unit of something. Recommendation:
   don't try to prevent this at sync time — let both sales go through (the
   customer already has their receipt), and let stock go negative
   *specifically as a result of a sync-time conflict* (distinct from the
   "block on insufficient stock" rule in Section 7, which is about a
   single device's own real-time state) with the discrepancy surfaced to
   the Manager/Owner to resolve manually (e.g. via the Inventory Ledger).
   Trying to solve this with distributed locking or reservations is
   over-engineering for a small-retail POS.
5. **Failure mode while offline stays simple.** If the device can't reach
   Catalog/stock for something it's never cached, that's a UX problem for
   the frontend to handle (e.g. warn the cashier), not a backend design
   problem.

This scopes `datasync` as: one batch-upload endpoint, one or more
read-sync/cache endpoints, and no attempt at real-time bidirectional sync
or distributed conflict resolution beyond "let it go negative, flag it."

---

## 11. Reports Metrics (Confirmed Spec)

- **Top products**: ranked by **revenue** (not quantity sold).
- **Sales Total**: **nets out** returns/voids — the headline figure is Net
  Sales (Gross − Returns/Voids), not raw gross. Gross and the
  return/void amount should still be visible as their own line items
  alongside Net, same as the prototype UI's Dashboard breakdown
  (Gross Sales / Discounts / Returns / Net Sales), just don't make Gross
  the headline number.
- **Breakdowns needed for v1**: by branch, by payment method, by
  product/category. (Explicitly **not** by staff/cashier for v1 - can be
  added later if needed, just not in scope now.)
- **Time granularity**: daily, weekly, and monthly views all supported —
  matches the prototype UI's Hourly/Daily/Weekly trend toggle.

---

## 12. Open Questions — Do Not Assume, Confirm First

1. **Return/Exchange policy specifics** — time window, default refund
   method, combined-transaction vs. chained-Return-then-Sale for Exchange
   (see Section 9's "still Shagan's call" notes).
2. **`datasync` implementation timing** — Section 10 gives the
   architecture; building it hasn't been scheduled yet.
3. **Customer `Tags`/`Consent`** are fully built in the backend but not
   surfaced anywhere in the Back Office UI — deferred, to be discussed
   later.
4. **Manager's exact frontend route shape** — confirmed there's a
   Back-Office/POS split gated by Manager PIN (Section 4), but the
   specific screens/scope of that branch-level Back Office beyond what's
   already listed under Manager capabilities hasn't been walked through
   screen-by-screen the way Owner's was.

---

## 13. Team Ownership (as of 2026-09-24 — re-verify before trusting)

- **Chan**: `internal/identity`, `internal/customer`, `internal/shift`,
  `internal/sales`, `internal/audit`, `internal/datasync`,
  `internal/reports`
- **Kit**: `internal/catalog`, `internal/inventory`, `internal/returns`,
  `internal/procurement`, `internal/platform`
- **`internal/common`**: shared, all three

Domain completion moves fast in this repo — re-run
`grep -c ErrNotImplemented internal/<domain>/*repository_impl.go` per
domain rather than trusting a stale percentage from memory or from this
file.
