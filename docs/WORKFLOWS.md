# Shagan POS — Workflows & Business Rules

This is the single source of truth for confirmed business rules and
end-to-end workflows. If you're about to build or change behavior in any
domain, read the relevant section here first. Where something is genuinely
undecided, it's listed in **Section 12 — Open Questions** instead of guessed
at. If you resolve one of those, move it into the confirmed sections and
delete it from Section 12.

Last confirmed: 2026-10-08. Every domain is built; sections 2, 4, 5, 6, 7, 9,
10, 12 and 13 were reconciled with the code on that date (nothing below should
still say a domain "doesn't exist yet"). Decisions are dated where they were
made, so a stale line is easy to spot.

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

### Roles & access rights (per organization — built 2026-10-22)

Modelled on Loyverse's "Access rights". **Roles and their grants belong to an
organization** (`roles.org_id`, `org_role_permission`): the Owner creates,
renames, re-grants and deletes roles in the Back Office, and two orgs' roles
never affect each other. Every org starts with two built-in roles
(`is_system`, can be renamed and re-granted but never deleted):

- **Manager** (`manager`) — everything in the catalogue below **except**
  `manage_staff`.
- **Cashier** (`cashier`; this is the old global `staff` role) — the till
  basics: `access_pos_portal`, `accept_payments`, `hold_resume_sales`,
  `manage_own_shift`, `log_till_expenses`, `add_customers`,
  `view_all_receipts`, `reprint_receipt`, `apply_manual_discount` (may *ask* for
  a custom discount - needs a manager's approval to get one).

An org that was already using the old global `super_staff` role got a custom
**Super Staff** role with its old grants (`open_drawer_no_sale`,
`apply_manual_discount`) plus the till basics and `approve_manual_discount`.
Anything else is a custom role the Owner builds. The **Owner** is not a staff
role (it's the org's own login), but `GET /roles` shows it as a fixed,
non-editable first row.

Each role has two switches, like Loyverse: **POS** (`access_pos_portal`: PIN
sign-in at the till) and **Back office** (`access_backoffice`: open the Back
Office at the till with that PIN), and a checklist under each. A permission
under a switch that is off is accepted but has no effect — the API says so in
a `warnings` list.

**POS** permissions (category `pos`)

| code | what it allows | Manager | Cashier | enforced on |
|---|---|---|---|---|
| `access_pos_portal` | **POS switch** — sign in to the till with a PIN | ✔ | ✔ | PIN verify |
| `accept_payments` | ring up sales and take payment | ✔ | ✔ | `POST /sales` |
| `hold_resume_sales` | hold and resume sales | ✔ | ✔ | `POST /held-sales`, `DELETE /held-sales/:id` |
| `manage_own_shift` | open and close their own shift | ✔ | ✔ | `POST /shifts`, `POST /shifts/:id/close` |
| `force_close_shift` | force-close another staff's shift | ✔ | | `POST /shifts/:id/force-close` |
| `log_till_expenses` | log an expense at the till | ✔ | ✔ | `POST /expenses` |
| `add_customers` | add customers at the till | ✔ | ✔ | `POST /customers`, `POST /customers/:id/consents` |
| `view_all_receipts` | see every receipt at the till (off = only their own) | ✔ | ✔ | `GET /sales` |
| `reprint_receipt` | reprint receipts | ✔ | ✔ | `POST /sales/:id/reprint` |
| `apply_manual_discount` | **ask** for a custom discount (opens the dialog; needs an approval to get one) | ✔ | ✔ | `POST /sales/discount-approvals` |
| `approve_manual_discount` | approve another staff member's custom discount with their PIN | ✔ | | PIN check in `POST /sales/discount-approvals` |
| `approve_own_discount` | approve a custom discount they asked for themselves (with `approve_manual_discount`) | ✔ | | same |
| `approve_void` | cancel (void) receipts | ✔ | | `POST /sales/:id/void` |
| `approve_return` | refunds | ✔ | | `POST /returns` |
| `approve_exchange` | exchanges | ✔ | | `POST /exchanges` |
| `open_drawer_no_sale` | open the drawer without a sale | ✔ | | `POST /drawer-events` |

**Back office** permissions (category `backoffice`; only meaningful with `access_backoffice` on)

| code | what it allows | Manager | enforced on |
|---|---|---|---|
| `access_backoffice` | **Back office switch** | ✔ | every back office route below |
| `view_reports` | dashboard and reports | ✔ | `GET /reports/*` |
| `view_item_cost` | see cost and profit | ✔ | `cogs`, `gross_profit`, `net_profit` of `GET /reports/profit-loss` |
| `view_sales` | sales history and receipts | ✔ | `GET /sales`, `/sales/:id`, `/sales/:id/receipt` |
| `view_audit_log` | audit log | ✔ | `GET /audit-log` |
| `manage_catalog` | products, categories, combos | ✔ | catalog writes, `GET /products/:id/branch-prices` |
| `manage_branch_prices` | set their branch's prices | ✔ | `PUT/DELETE /products/:id/branch-prices/:branchId` (own branch only at a till) |
| `view_inventory` | stock levels, ledger, transfers | ✔ | `GET /stock-levels`, `/inventory/*`, `/stock-transfers` |
| `manage_stock` | adjustments and transfers | ✔ | `POST /inventory/adjustments`, `POST/PATCH /stock-transfers` |
| `manage_suppliers` | suppliers | ✔ | supplier writes |
| `manage_purchase_orders` | create and edit purchase orders | ✔ | `POST/PATCH /purchase-orders` |
| `receive_stock` | receive purchase orders | ✔ | `POST /purchase-orders/:id/receipts` |
| `view_customers` | customer list | ✔ | `GET /customers*` |
| `manage_customers` | edit customers and consents | ✔ | `POST /customers*` (also satisfies `add_customers`) |
| `view_staff` | staff list (read only) | ✔ | `GET /staff*`, `GET /branches/:id/staff` |
| `manage_staff` | add and edit staff, PINs, statuses (own branch) | | `POST /staff`, `PATCH /staff/:id` |
| `manage_expenses` | edit or delete any expense at the branch | ✔ | `PATCH/DELETE /expenses/:id` |
| `manage_receipt_settings` | receipt settings (own branch) | ✔ | `PUT /receipt-settings` |
| `resolve_sync_conflicts` | resolve offline sync conflicts | ✔ | `PATCH /sync/conflicts/:id/resolve` |

**Owner-only, never grantable** (`RequireOrgAdmin`; no permission rows exist for
them): roles and access rights, branches, devices and POS accounts, **payment
QR codes** (upload/delete), the org-wide receipt default, other branches'
data, organization settings and timezone.

Rules worth knowing:
- **Changes reach staff at their next PIN sign-in.** A staff token already
  issued keeps the permissions it was minted with (up to its 12-hour life).
- **Tokens issued before the 2026-10-22 migration keep working until they
  expire**: a token with no `access_version` claim lists only the seven legacy
  codes and is expanded (`authtoken.StaffClaims.EffectivePermissions`) —
  `access_pos_portal` implies the till basics, `access_backoffice` implies the
  Manager's back office reach and `force_close_shift`. It never gains an
  approval, the drawer, discounts or `manage_staff`.
- **At least one role must keep the POS switch on** (409 otherwise).
- **A till that holds `manage_staff` is confined**: its own branch only, and
  only roles whose every permission it holds itself (no minting a Manager, no
  resetting a Manager's PIN from a lesser role).
- **Roles in staff requests must be the org's own** — another org's role id is
  a 404.
- **`apply_manual_discount` means "may ask for a custom discount"** (Discount
  V1, 2026-10-23) and is on for Cashier: it opens the dialog, it never gives a
  discount by itself - the approval needs someone holding
  `approve_manual_discount`, which Cashier doesn't. An Owner can take it off a
  role to stop that role from even asking. Existing orgs' built-in Cashier got
  it once, by a one-time data migration (`data_migrations` table), so an Owner
  who later removes it is not overridden on the next restart. See "Discounts"
  in Section 8.

**Migration** (`identity.MigrateAccessRights`, run on every start by
`migrate.Run` and by `go run ./cmd/seed`; idempotent): for each org with no
roles of its own it creates Manager + Cashier (+ Super Staff if used), copies
each legacy role's grants **verbatim** for the seven legacy permissions and
gives the new permissions their defaults, then repoints every `staffs.role`
at the org's own role. A legacy role that isn't one of the three seeded ones
is cloned as a custom role. The legacy global rows stay as the migration
source and are never read at runtime. An org that already has roles is left
alone, so an Owner's edits survive restarts.

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
- `middleware.RequireStaffToken(jwtSecret)` — used on: `CreateDrawerEvent`,
  `CreateReturn`, `CreateExchange`. (`UpdateExpense`,
  `DeleteExpense` and `VoidSale` use
  `RequireStaffTokenOrOrgAdmin` instead — same, but an Owner / Service Center
  bearer token may pass with no staff token; see "Owner acts as owner" below.)
  These need to know who's acting, but don't
  hard-gate on one fixed permission via middleware — the handler makes its
  own permission decision inline (see below).
- `middleware.RequireStaffPermission(jwtSecret, code)` (= `RequirePermission`)
  — the staff token must carry `code`; no manager-approval escape hatch.
  Used on: `POST /sales` (`accept_payments`), held sales (`hold_resume_sales`),
  `POST /shifts` and `/shifts/:id/close` (`manage_own_shift`),
  `ForceCloseShift` (`force_close_shift`).
- `middleware.RequireStaffPermissionOrOrgAdmin(jwtSecret, codes...)` — same,
  but an Owner / Service Center passes with no staff token, and any one of
  several codes is enough. Used on: `POST /expenses` (`log_till_expenses`),
  `POST /customers` + consents (`add_customers` or `manage_customers`),
  `POST /sales/:id/reprint` (`reprint_receipt`).
- `middleware.RequireBackOfficePermission(jwtSecret, code)` — Owner / Service
  Center always pass; a till needs a staff token carrying **both**
  `access_backoffice` and `code`. Used on every back office write (table below).
- `middleware.RequireFeature(checker, code)` — a **plan** gate, not a permission
  one: 403 `feature_not_in_plan` unless the org's current plan includes the
  feature (see "Subscription tiers"). Nothing built today uses it; each
  Pro / Enterprise feature gets it when it's built.
- `middleware.RequireViewPermission(jwtSecret, code)` — for reads the till also
  uses. Restricts **only a caller using Back Office** (a staff token with
  `access_backoffice`), who then needs `code`; a plain till read (no staff
  token, or a cashier's) is unchanged, and an Owner always passes.
  `middleware.CallerHasPermission(c, code)` is the inline check for fields
  (e.g. `view_item_cost`).
- `middleware.ManagerApproved(c, jwtSecret, permission)` — an
  **OR-fallback**, not a gate: a handler checks
  `StaffHasPermission(c, X) || ManagerApproved(c, jwtSecret, X)` wherever a
  staff member without their own permission should still be able to proceed
  with someone else's approval. Currently wired into:
  - `CreateDrawerEvent` → `open_drawer_no_sale` (only when `sale_id` is
    omitted - a drawer event tied to a real sale needs no such check)
  - `VoidSale` → `approve_void`, `CreateReturn` → `approve_return`,
    `CreateExchange` → `approve_exchange` (all in `cmd/api/returns.go`) -
    the exact same `StaffHasPermission || ManagerApproved` shape. Any new
    approval-gated action must reuse it, not invent a different one. (`VoidSale`
    alone also accepts an Owner / Service Center acting directly - see
    "Owner acts as owner" below; Return/Exchange do not.)
  - **Not discounts any more.** A custom discount is approved when it is
    *added* (`POST /sales/discount-approvals`), so `POST /sales` no longer
    reads `apply_manual_discount` or `X-Manager-Approval-Token` at all.

### Route authorization by account type (confirmed 2026-10-08)

The access JWT carries an `account_type` claim (`owner` / `pos` /
`service_center`), and `POST /auth/login` / `POST /auth/refresh` return
`account_type` and `access_expires_at` (the access token's expiry;
`expires_at` is still the *refresh* token's). A token issued before the claim
existed gets **401** on any gated route (re-login / refresh), never 403.

| Gate (`internal/middleware`) | Who passes | Routes |
|---|---|---|
| `RequireOrgAdmin` | Owner, Service Center (never a till, manager PIN or not) | `POST/PATCH /branches`, `POST/PATCH /devices`, **all role routes** (`GET/POST /roles`, `PATCH/DELETE /roles/:id`, `GET /roles/:id/permissions`, `GET /permissions`), **payment-QR upload/delete** |
| `RequireBackOfficePermission(code)` | Owner, Service Center, **or** a POS token + `X-Staff-Token` carrying `access_backoffice` **and** `code` | catalog writes (`manage_catalog`), `GET /products/:id/branch-prices` (`manage_catalog`; a till sees only its own branch), supplier writes (`manage_suppliers`), `POST/PATCH /purchase-orders` (`manage_purchase_orders`), `POST /purchase-orders/:id/receipts` (`receive_stock`), `POST /inventory/adjustments` + `POST/PATCH /stock-transfers` (`manage_stock`), `PUT/DELETE /products/:id/branch-prices/:branchId` (`manage_branch_prices`; a till's manager only for **their own branch**, else 403), `PUT /receipt-settings` (`manage_receipt_settings`), `PATCH /sync/conflicts/:id/resolve` (`resolve_sync_conflicts`), `POST /staff` + `PATCH /staff/:id` (`manage_staff`) |
| `RequireViewPermission(code)` | everyone, except a staff token with `access_backoffice` lacking `code` | `GET /reports/*` (`view_reports`), `GET /audit-log` (`view_audit_log`), `GET /sales*` (`view_sales`), `GET /stock-levels`, `/inventory/*`, `/stock-transfers` (`view_inventory`), `GET /customers*` (`view_customers`), `GET /staff*`, `GET /branches/:id/staff` (`view_staff`) |
| `RequireStaffPermission` / `...OrOrgAdmin` | see "Which middleware/helper, when" | the till actions listed there |
| (none beyond `Auth`) | any authenticated caller | the reads the till needs to sell (products, receipt settings, QR codes, held sales, current shift, ...), `GET /branches*`, `GET /devices`, own-account routes, PIN verify |

- **Service Center = same as Owner** on every admin route (decision, 2026-10-08).

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
- **Still open to a POS token with no staff token** (flagged, not decided):
  the *reads* above (reports, audit log, sales, inventory, customers, staff) —
  they're what the till itself reads, so a till that never presents a Back
  Office staff token is not restricted — and `GET /suppliers`,
  `GET /purchase-orders`, `GET /sync/conflicts`, `POST /printers/test`.
  Cost fields: `cost_price` is never serialized on any product response, and
  P&L `cogs`/`gross_profit`/`net_profit` are dropped unless the caller has
  `view_item_cost` (or is the Owner).

**Rule of thumb:** if a client-supplied `staff_id`/`created_by` field still
exists on a request DTO, it's for wire compatibility only — the server
always overwrites it from the verified token. Never trust it, and never
re-introduce a `binding:"required"` tag on one (a real bug already caused by
this twice: Shift and Sales both originally rejected a client correctly
omitting the field, before the override ever ran).

### Browser access (CORS) — built 2026-10-08
`CORS_ALLOWED_ORIGINS` (comma-separated, exact `scheme://host[:port]`) lists the
browser origins that may call the API cross-origin; any other origin gets no
`Access-Control-*` headers, so the browser blocks it. **Empty (the default)
means no browser origin is allowed** — correct today, because the Next.js
frontends proxy the API server-side and Postman/curl send no `Origin`. The
allowed request headers include `X-Staff-Token`, `X-Manager-Approval-Token` and
`If-None-Match`, and `ETag` is exposed, so a browser client of the offline
catalog works once its origin is listed. No credentials/cookies are ever
allowed (bearer tokens only). A lone `*` re-opens it to any origin — local
development only, never production.

### Shagan-team portal endpoints — `/internal/*` (`X-Internal-Key`)
Used by the shagan_team portal; a tenant token can never reach them. Beyond
provisioning (`POST /internal/accounts`, `/branches`, `/devices`,
`/accounts/pos`) and the org status/timezone levers:
- `GET /internal/organizations` and `GET /internal/organizations/:id` (one org);
- `GET /internal/accounts/org-wide?org_id=` lists an org's **owner and
  service_center** logins; `POST /internal/accounts/org-wide/:id/reset-password`
  resets one (404 for any other account type, so a pos login can't be reached
  this way). Pos logins keep their own `/internal/accounts/pos...` routes;
- `PATCH /internal/branches/:id/status` (`active|inactive`) and
  `PATCH /internal/devices/:id/status` (`active|inactive|revoked`). A branch or
  device that isn't active can't have a **new** shift opened (409); a shift
  already open is not interrupted, and a revoked device's pos account can still
  log in unless it is also suspended (`PATCH /internal/accounts/pos/:id/status`).
- **Any password reset revokes the account's refresh tokens** (pos and
  org-wide alike) in the same step, so whoever knew the old password can't keep
  a session alive; an access token already issued lives out its TTL (60 min).
- Not built (not requested): suspending an individual owner/service_center
  account — suspending the whole org (`PATCH /internal/organizations/:id/status`)
  already blocks every login.
- Subscription tiers: `GET /internal/plans`, `GET|PATCH
  /internal/organizations/:id/subscription`, `PUT|DELETE
  /internal/organizations/:id/features/:code`; the organization list and
  get-by-id also carry the plan fields - see "Subscription tiers" below.

### Subscription tiers — Basic / Pro / Enterprise (built 2026-10-27)

An organization has a **plan**: `basic` (default) / `pro` / `enterprise`. It
belongs to the **organization** - every login of the org (owner, service center,
POS devices, staff) gets it. It is separate from `status`: `status` decides
whether the org can use Shagan at all; the plan decides features and how many
branches / devices. **A plan never suspends an org or stops a till from
selling** - an expired or unpaid plan only drops the org to Basic. The shagan_team
portal sets it by hand over `/internal`; billing lives in shagan_team, not here.

| | Basic | Pro | Enterprise |
|---|---|---|---|
| Active branches | 1 | 5 | unlimited |
| Active devices | 2 | 10 | unlimited |
| Everything built today (incl. branch prices, custom roles, offline mode) | yes | yes | yes |
| `customer_display`, `advanced_reports`, `report_export`, `promotions` | - | yes | yes |
| `loyalty`, `sms_receipts`, `api_access` | - | - | yes |

The tiers live in code (`internal/subscription/subscription_plans.go`). **None
of the gated features exists yet** (and no existing route is gated): only the
limits apply today. Each feature gets `RequireFeature(code)` on its routes when
it is built.

**Stored fields** (on `organizations`): `plan`, `plan_expires_at` (null = no end
date), `plan_trial`, `extra_branches`, `extra_devices` (per-org add-ons on top
of the plan's limits, so a 6-branch shop needn't buy Enterprise). Two tables:
`org_feature_overrides` (one feature on/off for one org, unique per org+feature -
a deal or a beta) and `org_plan_changes` (append-only history; `changed_by` is
the shagan_team admin's **email**, sent in the request body since `/internal` has
no user). A named data migration (`2026-10-27-organization-plans`) left every
existing org on `basic`, no trial, with one history row (`changed_by:
"migration"`). A **new** org starts on a **14-day Pro trial**
(`POST /internal/accounts`: `plan: pro`, `plan_trial: true`, history row +
audit entry; an optional `changed_by` in that body, else `"system"`).

**The one resolver** - `subscription.Resolve` (pure) behind
`Entitlements(orgID, now)`:
1. start from the stored plan (an unknown code reads as Basic);
2. if `plan_expires_at` has passed: a **paid** plan keeps working for a
   **7-day grace period** (`in_grace: true`), then drops to Basic
   (`expired: true`); a **trial has no grace** - it drops at its end date. All
   worked out on read, so there is **no cron job**;
3. apply the feature overrides (on adds, off removes; they still apply after a
   drop to Basic);
4. limits = the effective plan's limit + `extra_branches` / `extra_devices`
   (unlimited stays unlimited; extras survive a drop to Basic).

It is cached **60 s per org, in the process**, and every `/internal`
subscription write clears that org's entry, so a change shows at once on the
instance that took it. **The plan is not in the JWT** (staff tokens live 12 h,
device refresh tokens 30 days), so it is never a stale claim. The cache is
used for features (`RequireFeature`, `/me`, `/sync/catalog`); the **limit check
never uses it** (below).

**Limits** count **active** rows only (`status = "active"`; inactive / revoked
ones don't count). Adding or **reactivating** one more active branch / device
than the limit is **403**
`{"success":false,"code":"plan_limit_reached","limit":"branches"|"devices","max":1,"current":1,"message":"..."}`.
It applies to **everyone, no bypass**: `POST /branches`, `POST /devices`,
`POST /internal/branches`, `POST /internal/devices`, and reactivation through
`PATCH /branches/:id` / `PATCH /devices/:id` (owner) and
`PATCH /internal/branches/:id/status` / `/devices/:id/status`. Creating an
*inactive* branch/device is never blocked; re-sending "active" to one that is
already active, a rename or a branch move never counts. The check runs in the
**same transaction as the write**, after taking a row lock on the organization
and reading the stored plan fresh, so two parallel creates (owner and `/internal`
alike) can't both pass the same count. **A downgrade deletes and switches off
nothing**: an org over its new limits keeps what it has and just can't add more;
the portal shows it as `over_limit`. **Never blocked by the plan**: login,
selling (`POST /sales`, `/sync/sales`), shifts, receipts, returns.

**`RequireFeature(code)`**: 403
`{"success":false,"code":"feature_not_in_plan","feature":"promotions","plan":"basic","message":"..."}`
(`plan` is the effective plan). A lookup failure is a 500/404, never mistaken for
"not in plan".

**Exposed to the apps**:
- `GET /me` = the user (as before, flattened) plus `subscription`:
  `{plan, trial, expires_at, in_grace, expired, features[], limits:{branches,devices},
  usage:{branches,devices}}`; a `null` limit is unlimited; `usage` is live (never
  cached). (`PATCH /me` still returns the plain user.)
- `GET /sync/catalog` adds top-level `plan` and `features`; they are in the body
  the ETag hashes, so a plan or override change gives the till a fresh snapshot,
  not a 304.

**`/internal` (shagan_team)**:
- `GET /internal/plans` -> `{plans: [{code, name, features[], limits}], features: [every code]}`.
- `GET /internal/organizations/:id/subscription` -> `{organization_id, effective:
  <the /me object>, raw: {plan, expires_at, trial, extra_branches, extra_devices},
  over_limit, overrides: [{id, org_id, feature, enabled, updated_at}], history:
  [newest first, at most 100]}`.
- `PATCH` the same path: `{plan?, expires_at? (null clears), trial?, extra_branches?,
  extra_devices?, note?, changed_by}` - **partial** (an omitted field is left as
  is); 400 for an unknown plan, a negative extra, a bad date or a missing
  `changed_by`; a PATCH that changes nothing writes nothing. Returns the detail.
- `PUT /internal/organizations/:id/features/:code` `{enabled, note?, changed_by}`
  and `DELETE` (`changed_by` / `note` in an optional body or as `?changed_by=` /
  `?note=`): override one feature / go back to the plan's default (DELETE of
  nothing is a no-op). 400 for an unknown feature code. Return the detail.
- `GET /internal/organizations` and `/:id` carry `plan`, `plan_expires_at`,
  `plan_trial`, `extra_branches`, `extra_devices` plus **`effective_plan`** and
  **`over_limit`** (more active branches or devices than the effective plan +
  extras allows).
- **Every** subscription write (plan change, feature override set / cleared,
  the trial) appends an `org_plan_changes` row (an override change repeats the
  unchanged plan fields and says what happened in `note`) **and** writes
  `audit_log` (`entity: organization_subscription`, `entity_id` = org id, action
  `subscription_updated` / `feature_override_set` / `feature_override_cleared` /
  `subscription_trial_started`, before / after, `changed_by` in `after`).
- The demo seed (`db/demo_seed.sql`) puts its org on Enterprise.

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

A Manager is a Staff record whose role carries the Back office switch (by
default the built-in `manager` role; an Owner can build others - see Section 1's
"Roles & access rights") — not a separate account. Everything below is
**branch-scoped** to wherever that Staff record belongs, and **every Back Office
action needs its own permission** on top of `access_backoffice`.

**Getting into Back Office at a terminal** (confirmed): there are two
frontend routes — POS and Back Office. Entering the Back Office route at a
shared terminal requires a Manager PIN verify to unlock it (this is
`VerifyManagerPIN` with `permission: "access_backoffice"`, or an
equivalent flow built on the same mechanism) — distinct from Owner's own
dedicated web login, which has no such gate (Section 3).

**Decision (2026-10-06):** the manager's Back Office is the *owner's* Back
Office screens, scoped to the till's own branch — not a separate, smaller UI.
What the manager can actually *do* there is decided server-side: see "Route
authorization by account type" in Section 2 (catalog/supplier/settings/stock/PO
writes allowed with their permission; branch, device, role and payment-QR
management owner-only; staff management only with `manage_staff`, off by
default).

Confirmed capabilities:
- **Staff**: view-only by default (`view_staff`); adding/editing staff needs
  `manage_staff`, which no built-in role has (own branch, and only roles the
  caller itself holds).
- **Sales / Inventory (stock) / Reports**: view, scoped to their own branch.
  (Stock visibility is already schema-supported: `StockLevel` is keyed by
  `{ProductID, BranchID}`.)
- **Expenses**: can log new ones for their branch, and can edit/delete
  **any** staff's expense at their branch (gated by `access_backoffice` +
  `manage_expenses` — see `shift.ExpenseActor.CanManageAny`).
- **Suppliers / Purchase Orders**: suppliers are shared org-wide (not
  branch-owned); Manager creates/manages Purchase Orders and receiving *for
  their own branch* against that shared supplier list. **Built:** a
  `PurchaseOrder` carries `org_id` and `branch_id`; a manager at the till can
  only raise/read/update/receive orders of the till's own branch (another
  branch's order reads as 404). PO numbers are server-generated (Section 7).
- **Approvals**: approves another staff's action they lack permission for —
  manual discount, drawer-without-sale, void, return and exchange — via
  `VerifyManagerPIN`, which issues the
  `X-Manager-Approval-Token` the other staff's own request then carries
  (see Section 2). Not restricted to role=`manager` specifically - whoever's
  role grants the target permission can approve it.
- **Emergency shift override**: can force-close another staff's shift via
  `POST /shifts/:id/force-close` when that staff is genuinely unavailable
  (e.g. called in sick) — always requires a `reason`, regardless of
  whether the counted cash matches. Recorded distinctly
  (`Shift.ClosedByStaffID` differs from `Shift.StaffID`) from a normal
  close. This one has no manager-approval fallback of its own - it *is*
  the escape hatch, gated by the `force_close_shift` permission directly.
- **Custom discounts**: approves another staff member's discount by typing
  their PIN on the till when the discount is added (needs
  `approve_manual_discount`); can approve their own with
  `approve_own_discount` too, no PIN pad. See "Discounts" in Section 8. A
  manager never enters a PIN at payment.

---

## 5. Staff (Cashier) Journey

A Staff record whose role has the POS switch — by default the built-in
`cashier` role (or a custom role such as the migrated `Super Staff`); see
Section 1's permission tables for exactly what each grants. Each step below is
enforced by its own permission: a role the Owner has stripped of, say,
`accept_payments` gets 403 at that step.

1. PIN sign-in at a POS device (`VerifyStaffPIN`, scoped to that device's
   branch) — issues the `X-Staff-Token` used for everything below.
2. **Opens their own shift** with an opening cash float
   (`POST /shifts`) — the shift always belongs to whichever staff the
   token identifies, never a client-supplied id.
3. Rings sales (`POST /sales`, needs `accept_payments`; holding/resuming a cart
   needs `hold_resume_sales`; reprinting needs `reprint_receipt`; the receipt
   list shows every sale only with `view_all_receipts`, otherwise just their
   own): add items (optional per-item discount,
   optional combo grouping — see below), optional customer lookup/create,
   one or more payments (split payment supported across cash/QR — the only
   2 methods accepted in this version; QR codes are managed per-branch via
   Platform, up to 5 per branch, one per bank). Server derives
   `Subtotal`/`Discount`/`Tax`/`Total`
   from the items — never trusts client-computed totals — and rejects if
   payments don't sum to the derived total (`Total = Subtotal − Discount`:
   prices already include tax, it is never added).
4. Can see current stock for their branch while ringing up a sale (same
   login/UI as Manager, just gated by which PIN was entered — see Section
   1).
5. **Discounts**: a product's catalog % is automatic (no permission, no
   approval). A custom % on a line needs an approval obtained *when it is added*
   (`POST /sales/discount-approvals`: a manager's PIN, or self-approval for a
   seller who holds both approve permissions) - payment never asks for a PIN.
   See "Discounts" in Section 8.
6. **Tax is inside the price** (see "Tax" in Section 8): each product has a
   `tax_percent` (0-100, set by the Owner); the shelf price already contains
   it, so a customer pays exactly the (discounted) price and the tax is **never
   added**. The server works out every line's tax - `round(line_total x p /
   (100 + p), 2)` on the line total **after** its catalog / custom / combo
   discount - and stores the rate on the line. There is no tax input: a `tax`
   a till sends is ignored. `Sale.Tax` is the sum across lines, informational.
7. **Combos**: ringing up a Combo expands it into one `CreateSaleItemRequest`
   per real product component (each with its own price and a proportional
   share of the discount; the server adds each component's own tax %), tagged with a shared `combo_id` purely for
   receipt/report grouping - see Section 8's design writeup for why.
8. Opens the drawer without a sale — only if their role grants
   `open_drawer_no_sale` (Manager and the migrated Super Staff by default), **or**
   a valid manager approval for it; otherwise 403.
9. Void/return/exchange need the equivalent
   `approve_void`/`approve_return`/`approve_exchange` permission or a
   manager approval, same pattern - see Section 9 for how each is built.
10. **Closes their own shift** (`POST /shifts/:id/close`) when their stretch
    ends — **only the same staff who opened it may close it**, no
    exception in the normal path. Counts the physical cash drawer; a
    mismatch from the system's expected cash total requires a `reason`.
    Only cash is physically counted — QR payments are trusted to match
    what the system recorded. Expected cash is opening cash + cash sales
    − cash refunds + cash an exchange collected − cash an exchange paid
    out (see Section 9, "Cash drawer effect").
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
   feeds the Inventory Ledger as `purchase_receipt` entries and raises stock
   at the PO's branch).
3. **Daily floor operation** (Staff/Manager) → PIN sign-in → open shift →
   ring sales (cart, payments, optional customer, combos; tax is inside the prices) →
   occasional drawer events / manager-approved exceptions → close shift
   with a real cash count.
4. **Money & stock bookkeeping** → every event lands in one place: the
   Inventory Ledger for stock movement (sale/void/return/exchange/
   adjustment/transfer/receipt — a completed sale decrements stock in the
   same transaction as the sale itself and is blocked outright on
   insufficient stock, no backorder, except an offline-queued sale at sync
   time, Section 10), `ShiftReconciliation` for cash, `Expense` for operating
   costs (rent, staff meals, etc., logged per branch).
5. **Back-office oversight** (Owner) → Sales History (with return/
   exchange/void), Reports (confirmed spec, Section 11, including profit/loss
   from Sales + the product cost basis `CostPrice` + Expenses), Audit Log
   (who-did-what system-wide).

---

## 7. Confirmed Business Rules (Quick Reference)

- **Shift**: opener == closer, no exception, except a force-close by someone
  with `force_close_shift` (a Manager by default; mandatory `reason` always,
  even on a cash match).
  One open shift per device at a time; rotation happens via normal
  close/reopen, not a shared open shift.
- **Cash reconciliation**: only cash is physically counted at close;
  `reason` required only when counted cash differs from expected. Expected
  cash = opening cash + cash sales − cash-method refunds of this shift's
  Returns + cash net difference collected on its Exchanges − cash net
  difference paid out on them. `GET /shifts/:id/summary` reports the three
  adjustments as `cash_refunds`, `exchange_cash_in`, `exchange_cash_out`.
- **Expenses**: creator, a staff member with Back Office open and
  `manage_expenses` (a Manager by default), or the Owner /
  Service Center acting directly may edit/delete;
  anyone signed in may create one (always attributed to themselves).
- **Sales discount**: catalog % automatic; custom % only with a discount
  approval got at add time (Section 8, "Discounts"); `price_override` is
  rejected.
- **Tax**: a % per product, already inside its price, computed by the server
  per line (after discounts) and summed into `Sale.Tax`. The total never adds
  it, and a client `tax` is ignored. See Section 8, "Tax".
- **Combos**: expanded client-side into normal per-product line items
  tagged with a shared `combo_id` - not a special server-side pricing path.
- **Stock decrement**: same transaction as `CreateSale`; blocked outright on
  insufficient stock (409), no backorder - the one exception is a sale
  ingested from an offline queue, which is let through and flagged
  (Section 10).
- **Procurement**: suppliers are shared/org-wide; Purchase Orders and
  receiving are branch-scoped.
- **Stock visibility**: per-branch, visible to both Staff and Manager
  (same login, gated by PIN only).
- **`PurchaseOrder` numbers**: server-generated (confirmed 2026-10-05), not
  client-supplied - `"PO-%04d"`, sequential per org (`org_id` + `po_number`
  stays unique, not globally unique). Never editable afterward, including
  via `UpdatePurchaseOrder` - letting it change later would reopen the
  collision risk auto-generation exists to close.
- **A product with history can't be hard-deleted** (built 2026-10-08):
  `DELETE /products/:id` is 409 if any combo bundles it, or if anything still
  references it — a sale or exchange line, stock (even at zero), a stock
  adjustment or transfer line, an inventory-ledger entry, or a purchase-order
  line. Those tables have no DB-level FK, so a delete would orphan them and
  history screens would lose the product's name. Retire it with
  `PATCH is_active=false` instead; only a never-used product (e.g. created by
  mistake) can be deleted.
- **Stock transfers carry their lines and a note** (built 2026-10-08):
  `GET /stock-transfers`, `POST /stock-transfers` and `PATCH
  /stock-transfers/:id` all return the transfer plus `items: [{product_id,
  qty}]` (creation order, never empty). `POST` accepts an optional `note`
  (why the stock is moving) - trimmed, max 500 characters, stored as `""`
  when omitted, never edited afterwards.
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
  never derived from the product. The one thing that can differ per branch
  is the **selling price and catalog discount %**: a branch may override
  them (see "Branch prices", Section 8). `tax_percent`, `cost_price`, combo
  prices and `is_active` stay org-wide.

---

## 8. Combo Pricing at Checkout (Design)

`Combo`/`ComboItem` (Catalog) describe a bundle: several products sold
together at one lump-sum price. The question was how ringing one up
interacts with `SaleItem`, which is always keyed to one real `ProductID`.

**Decision: expand client-side, don't add server-side combo pricing
logic.** When a POS device rings up a Combo, it expands it into one
`CreateSaleItemRequest` per real component product — each priced from its
product like any standalone product sale (the server also works out its tax
from the product's own tax %, on the line total after its share), plus a
proportional share of the combo's bundle discount
(allocated by each component's normal price × qty, so the line items'
combined total lands exactly on the combo's advertised price), and a shared
`ComboID` tag purely for grouping on receipts/reports.

**What the device is given to do that (built 2026-10-08):** each combo in
`GET /combos` and in `GET /sync/catalog` carries `items: [{product_id, qty}]`
(creation order, never empty) alongside its price and images. Editing a
combo's items changes the snapshot ETag, so a cached till re-downloads rather
than expanding the old product list.

Why this shape, not a dedicated combo-aware code path:
- **No schema change to pricing/stock mechanics.** Every line item is
  structurally identical to a standalone sale of that product — stock
  decrement and reporting both work per real
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

### Discounts — catalog and custom (built 2026-10-23)

Two kinds, never mixed on a line. Every computed unit price is rounded the same
way: **truncate to 2 decimals, then round UP to the next K 100** (an exact
multiple stays) - `common.RoundUpKyat100`, applied through
`common.DiscountedUnitPrice(original, percent)`, which also never lets the
result exceed the original (a K 150 item at 10% stays K 150, not K 200).

**1. Catalog discount** - a % the Owner sets on the product (`discount_percent`,
0-100). The server computes and stores `sale_price` and the per-unit `discount`
(`price - sale_price`) and recomputes them whenever price or % changes; a client
`discount` amount is ignored. It is **automatic: no approval, no permission,
online or offline.** Existing products were backfilled exactly
(`sale_price = price - discount`, `discount_percent = discount / price`, not
re-rounded), so no price changed.

**2. Custom (manual) discount** - a % the till adds to one sale **line**
(percent only; no fixed amount; `price_override` is rejected). It comes off the
**original** price and **replaces** the catalog % (smaller than the catalog % is
allowed; the till only warns). It is **approved the moment it is added, one
approval per discount - payment never asks for a PIN:**

- The till calls `POST /sales/discount-approvals {product_id, percent, qty,
  reason?, approver_staff_id?, pin?}` with the seller's `X-Staff-Token`.
- The seller needs `apply_manual_discount` (403 otherwise) and an open shift on
  the till (409). The product must exist in the org.
- **Another staff member approves** with `approver_staff_id` + `pin`: checked
  like `VerifyManagerPIN` (same lockout counter, same branch rule, generic 401),
  and the approver needs `approve_manual_discount` (403) - plus
  `approve_own_discount` if they are also the seller. **Or the seller
  self-approves** (no PIN pad) by sending neither, if they hold
  `approve_manual_discount` **and** `approve_own_discount`.
- The response carries the prices the backend will charge (`unit_price`,
  `unit_sale_price`), so the till shows exactly them.
- An approval is for **one product, one %, up to N units, one seller, one
  till's open shift**, and is spent by **one sale line, once**. It stays valid
  until used, cancelled (`DELETE /sales/discount-approvals/:id`, 409 if already
  used), or its **shift closes** (swept to `cancelled` in the close's own
  transaction), at most 12 h.
- `POST /sales` takes a `discount_approval_id` on the line and re-checks
  everything (same org and branch, requested by this sale's staff, status
  `approved`, not expired, same shift, same product, `qty <= max_qty`, not on
  two lines of this request) - each failure is a 409 with a message. The
  approval is spent in the sale's transaction by a conditional update, so two
  sales racing for one approval can't both win. A retried sale that already
  spent its approval returns the original (200) without re-checking.

**How a line is priced** (`POST /sales`): everything comes from the product, not
the client. `unit_price` is the product's price **at the selling branch** (see
"Branch prices" below; a different client value is a 409 `price changed,
refresh the catalog`); with no approval the unit sale price is the product's
sale price at that branch; with one it is `DiscountedUnitPrice(original,
approval %)`. A combo line keeps the client's bundle share as `combo_discount`
(needs no approval, is not a manual discount). The line stores
`catalog_discount_percent/_discount`, `manual_discount_percent/_discount/_reason`,
`combo_discount`, `discount_approval_id` and `manual_approved_by`; `discount` is
still their sum, so totals, returns and reports are unchanged. The line's tax
is worked out last, after all of these (see "Tax" below). The same product
may sit on several lines (1 of 2 coffees discounted). Payments must still equal
the derived total exactly. Audit: `discount_approved` / `discount_cancelled`
(entity `sale`, id of the approval) and `manual_discount` per spent approval.

**Offline** - custom discounts are impossible (no approval offline); catalog
discounts work. See Section 10.

**Reporting** - the receipt/reprint items carry `unit_price` (original),
`unit_sale_price`, both percents, the reason, `combo_discount`,
`manual_approved_by` and the approver's name. `GET /reports/discounts`
(`view_reports`) returns catalog vs manual totals, `manual_count`, the lists by
seller and by approver, and how many approvals were cancelled.

### Tax — a % per product, inside the price (built 2026-10-23)

Shops here sell at shelf prices that **already include** tax, so tax is never
added to a total, and the till no longer tells the backend what the tax is.

- **`tax_percent`** (0-100, default 0) is set by the Owner in the product form
  (`POST/PATCH /products`; outside 0-100 is a 400). There is no org-wide rate.
  A client `tax` *amount* on a product request is ignored.
- **`product.tax`** is kept for old clients but is now computed and
  informational: the tax inside one unit at the sale price,
  `round(sale_price x p / (100 + p), 2)`, recomputed whenever price, discount %
  or tax % changes. `/products/barcode/:code` and `/sync/catalog` carry
  `tax_percent` (so the snapshot ETag changes with it).
- **A line's tax** (`POST /sales`) = `round(line_total x p / (100 + p), 2)`
  (half-up, per line), where `line_total` is **after** the catalog / custom /
  combo discount - so a discount also lowers the tax. K 10,500 at 5% -> K 500 of
  tax, and the customer pays K 10,500. A combo line uses its own component
  product's %. The rate is stored on the line (`sale_items.tax_percent`), so a
  receipt or return never depends on today's product. Tax **never changes a
  price**: the K 100 rounding rule is for prices only; tax is rounded to 2 dp and
  is for receipts and reports.
- **Totals**: `Total = Subtotal - Discount`; `Sale.Tax` is the sum of the line
  taxes and `Sale.TaxInclusive = true`. Payments must equal `Total` exactly, so
  a till still paying price + tax gets a 400. The request's per-line `tax` is
  not read.
- **Past sales never change.** A sale from before has `tax_inclusive = false`:
  its stored `total` already contains the tax that was added on top, and it is
  never recomputed. Its lines have `tax_percent = 0` (and the old amount in
  `tax`).
- **Migration**: on first start after this change each product with an old
  `tax > 0` gets `tax_percent = round(tax / price x 100, 2)` (capped at 100)
  and its **price is left alone** (recorded once in `data_migrations`; each
  converted product is logged as a warning). Its customers paid price + tax
  before and pay just the price now, so the Owner must review those products.
- **Returns**: the refund stays `line_total / qty x returned qty` (already
  tax-inclusive). `Return.tax_refunded` = per returned line
  `round(unit_refund x qty x p / (100 + p), 2)` at the sale line's stored rate.
- **Exchanges**: "in" lines credit as before. "Out" lines are priced **from the
  product** (its sale price, tax inside) - a `unit_price` in the request is not
  read; each line records its `tax_percent` and included `tax`, and the exchange
  records `tax_in` / `tax_out`. `net_difference` = out total - in credit.
- **Receipts**: items carry `tax_percent` and `tax` (the included amount); the
  receipt has `tax_inclusive: true` (print "Includes tax", don't add it). A
  sale from before has `false` - its tax was added on top.

### Branch prices — a branch may override price and discount % (built 2026-10-23)

The same catalog sells at different prices in different branches (Mandalay
costs more than Yangon). The product's `price` / `discount_percent` stay the
default for every branch; a branch may **override** either, per product.

> **Effective price at branch B** = B's override if it has one, else the
> catalog value. `price` and `discount_percent` fall back **independently**:
> an override of only the price keeps the catalog %, and one of only the %
> keeps the catalog price. With no override rows every branch behaves exactly
> as before - there is no data migration.

- **Table `product_branch_prices`**: `product_id`, `branch_id`, `org_id`,
  `price` (null = catalog), `discount_percent` (null = catalog), `sale_price`,
  `updated_by_user_id`, `updated_at`; unique on `(product_id, branch_id)`,
  writes are upserts. At least one of `price` / `discount_percent` is set - a
  row with neither is deleted instead. No DB foreign keys (as everywhere), so
  deleting a product deletes its overrides in the same transaction.
- **`sale_price` is stored** on the override, computed by the server on every
  write with the product's own rule (`common.DiscountedUnitPrice`, K 100
  rounding), from the override's price/% or the product's where null - so a
  sale and an exchange at that branch can never disagree by a rounding step.
  When the product's catalog `price` or `discount_percent` changes, the stored
  `sale_price` of every override that inherits the changed field is recomputed
  in the same transaction.
- **Validation**: `price > 0`, `0 <= discount_percent <= 100` (400); the branch
  must be in the caller's org (404) and **active** (400) to write. Clearing an
  override works for an inactive branch too.
- **One resolver** (`catalog.EffectivePricing` / `ResolveBranchPricing` /
  `ResolveBranchPricingBatch`) is the only place a product is priced; none of
  these read `Product.Price` / `DiscountPercent` / `EffectiveSalePrice()`
  directly any more:

  | Path | Priced at |
  |---|---|
  | `POST /sales` (online) - the 409 check, `unit_price`, catalog %, line total | the selling branch (the POS token's) |
  | `POST /sync/sales` (queued) - the `price_mismatch` conflict compares the till's snapshot with | the branch's effective price (the conflict's current price / percent are the branch's) |
  | `POST /sales/discount-approvals` - `unit_price`, `unit_sale_price` | the branch price; the approved % comes off it |
  | `POST /exchanges` - "out" lines | the sale's branch (where the exchange happens) |
  | `GET /products`, `/products/:id`, `/products/barcode/:code` | a POS token: its branch; an owner: `?branch_id=`; neither: the plain catalog prices |
  | `GET /sync/catalog` | the caller's branch (owner: `?branch_id=`) |

  Product responses resolved for a branch carry `"price_source": "branch" |
  "catalog"` (absent when no branch was given), with `price`,
  `discount_percent`, `sale_price` and the derived per-unit `discount` / `tax`
  already at the branch's values. A `?branch_id=` of another org is a 404.
- **Offline catalog**: the snapshot is built per branch and its **ETag includes
  the branch**, so a till never keeps another branch's prices on a 304.
- **Unchanged - they use the price snapshotted on the sale line**: receipts,
  reprint, returns, voids, the "in" side of exchanges, reports. Combo prices
  stay org-wide; a combo line's component is priced like any product line.
- **API**:
  - `GET /products/:id/branch-prices` - one row per branch: `branch_id`,
    `branch_name`, `price` / `discount_percent` (the override; null =
    inherits), `catalog_price` / `catalog_discount_percent` (the product's own
    values - what a blank field falls back to; a till token can't read them
    anywhere else), `effective_price`, `effective_discount_percent`,
    `effective_sale_price`, `price_source`, `updated_at`. Owner / Service
    Center see every branch (or `?branch_id=`); a manager unlocked into Back
    Office at a till (`access_backoffice` + `manage_catalog`) sees **only their
    own branch**.
  - `PUT /products/:id/branch-prices/:branchId` `{ "price": "1900.00",
    "discount_percent": null }` - upsert; **replaces the whole override** (a
    null/omitted field inherits). Both null = clear. Same row shape as the GET
    (so it carries `catalog_price` / `catalog_discount_percent` too). Needs
    **`manage_branch_prices`** (below).
  - `DELETE /products/:id/branch-prices/:branchId` - back to the catalog
    price; 204, idempotent. Same permission.
- **Who may write** (decided 2026-10-24): the **Owner / Service Center** - any
  branch of the org, no permission needed - and a **manager at a till**: a POS
  token + `X-Staff-Token` carrying `access_backoffice` **and**
  `manage_branch_prices` (held by the default Manager role; the Owner can take
  it off or give it to another role under Roles). **Own-branch rule**: for a
  POS token `:branchId` must be the token's branch, else **403** "you can only
  change your own branch's prices" - decided in the service before anything is
  looked up, so another branch's (or another org's) id tells a manager nothing.
  A till reads only its own branch's rows. The permission was added to every
  existing org's built-in Manager role once, by a data migration
  (`2026-10-24-manager-manage-branch-prices`); other roles were not touched.
  Note a role with `manage_branch_prices` but without `manage_catalog` can
  write but not read the rows - grant both.
- **Audit**: every upsert / delete writes `audit_log` (entity
  `product_branch_price`, `entity_id` = product id, `branch_id`, action
  `branch_price_set` / `branch_price_removed`, `before` / `after` =
  `{price, discount_percent, sale_price}`; a manager is `actor_id` (staff), the
  Owner / Service Center `actor_user_id`). `product_branch_prices` records the
  same on the row: `updated_by_staff_id` or `updated_by_user_id`, exactly one.
- **Out of scope (v1)**: scheduled price changes, per-branch "not sold here",
  bulk edit / CSV import, per-branch tax or cost, combo prices.

## 9. Returns / Void / Exchange — Standard Business Rules

Researched from how these three concepts work across mainstream POS
systems (Square, Shopify POS, Lightspeed, Clover, etc.) — these are
industry-standard definitions, not invented for this project. Specific
policy numbers (time windows, cash-vs-credit default, whether partial is
allowed at all) are still Shagan's own call — flagged below where that's
true.

**Built (confirmed wiring, 2026-10-08):**
- **Void** reverses the whole sale: 409 if already voided, if it was fully
  returned (`refunded`), if its shift is no longer open, or if any
  Return/Exchange already references it; credits every
  item's stock back and marks the sale voided, atomically. Needs `approve_void`
  (own or manager approval); an Owner / Service Center may also void directly
  with no PIN (Section 2, "Owner acts as owner"). `reason` is one of
  `customer_request`, `price_error`, `item_error`, `staff_error`, `other`
  (owner back office) or `duplicate_transaction`, `wrong_order`,
  `incorrect_payment`, `cashier_mistake` (the till's screen); anything else
  is 400. `explanation` is optional (stored as `""`).
- **Return** is a new record referencing the sale (the sale stays in history),
  full or partial per item, with `RefundTotal` computed server-side from each
  returned line's own net price. Each item carries a condition (`sellable`,
  `damaged`, `opened`, `defective`, `expired`, `other`; anything else is 400);
  only `sellable` ones are restocked, at the sale's own branch. The return
  also stores an optional free-text `explanation` (the till's typed "reason for
  return"), next to the coarse `reason_code`. Needs
  `approve_return`. **`refund_method` is an explicit single choice per request,
  `cash` or `qr` — there is no default.**
- **Exchange** is one combined `Exchange` record with in/out lines and a
  server-computed `NetDifference` (not a chained Return-then-Sale); stock is
  credited for "in" items and decremented for "out" items at the sale's branch.
  Each "in" item carries an optional `condition` (same values as a Return);
  only `sellable` goes back on the shelf, and an omitted condition means
  `sellable`. Needs `approve_exchange`. *Built this way without an explicit sign-off — see
  Section 12.*
- **Sale status after a Return**: a Return that brings every line of the sale
  back **through returns alone** (return qty = sold qty on every line,
  counting earlier returns; exchange "in" qty does not count) sets the sale's
  status to `refunded`. Partial returns and any Exchange leave it `completed`.
  A `refunded` sale still counts its original payments in its shift's
  sales/expected-cash totals (the cash refund is subtracted separately), and
  reports keep counting it (they only exclude `voided`/`open`).
- **Return/exchange footprint on history and receipts**: every row of
  `GET /sales` and `sale` in `GET /sales/:id/receipt` (and reprint) carries
  `has_return`, `has_exchange` and `refunded_total` (sum of the sale's return
  refunds, `"0"` when none). Each receipt item carries `returned_qty` (return
  lines + exchange "in" lines) and `returnable_qty` (`qty - returned_qty`,
  never below 0) - the same quantities the over-return guards enforce, so the
  till can cap its steppers before asking for a manager PIN.
- **Write-offs in the Inventory Ledger**: goods that come back but are not
  restocked (a Return line, or an Exchange "in" line, in any condition but
  `sellable`) still get a ledger row so they don't vanish from the owner's
  view: `type: return_writeoff`, `qty: 0` (stock did not move),
  `balance_after` = the product's balance as it stands, same
  `reference_type`/`reference_id` as the return/exchange. Restocked lines keep
  their normal `return` / `exchange_in` rows.
- **Cash drawer effect**: every Return and Exchange is stamped with the shift
  open on the till (the paired POS account's open shift) it was rung up on;
  none open means no stamp and it counts toward no shift. A Return's
  `refund_method: cash` takes `refund_total` out of that shift's expected
  cash (QR does not). An Exchange with a non-zero net difference must say how
  it was settled with `method` (`cash`/`qr`, 400 if omitted; optional for an
  even swap): cash `net_difference > 0` is cash in, `< 0` is cash out, QR
  never touches the drawer.
- Return time window: **none is enforced** (open, Section 12).

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
(e.g. 7/14/30 days)? (The refund method is settled: explicit `cash` or `qr`
per return, no default, no store credit — Section 7's payment-method rule.)*

### Exchange
Customer returns item(s) **and** takes different item(s) in the same
visit. Functionally, virtually every POS models this as a Return + a new
Sale happening together, net-settling any price difference — if the new
item(s) cost more, the customer pays the difference; if less, they get a
partial refund or store credit for the difference. Some systems store this
as one combined "Exchange" transaction referencing both the original sale
and the new items; others just chain a Return immediately followed by a
new Sale. Requires `approve_exchange`.

*Built as a single combined `Exchange` record (see above). Confirm that is
the shape Shagan wants, or it can be re-modelled as a chained Return-then-Sale.*

---

## 10. Datasync / Offline Support (Built)

Confirmed as a real requirement, not just leftover ERD scaffolding — a POS
till needs to keep working (and selling) through a spotty or dropped
connection, and catch up once it's back online. The `datasync` domain is
built: `GET /sync/catalog` (ETag; products - priced for the caller's branch -,
categories and combos with their items), `POST /sync/sales` (idempotent batch upload), `GET /sync/status`,
`GET /sync/conflicts` and `PATCH /sync/conflicts/:id/resolve` (the latter needs
`resolve_sync_conflicts` with Back Office open, or an Owner / Service Center).
The shape below is the design it follows, reusing what was already in place.

### What's already compatible
- `Sale.ID` is a **client-generated UUID** — the device can create a sale's
  identity the instant it's rung up, offline or not, with no server
  round-trip needed first. This also makes re-submission naturally
  idempotent: retrying a POST with the same ID is safe.
- `NameSnapshot`/`UnitPrice` on each `SaleItem` are already
  client-cached values, not server lookups at sale time — the device
  doesn't need to be online to know what it's selling. (Tax is not one of
  them: the server works it out from the rate - see the snapshot rule below.)
- `Sale.SyncedAt` exists to mark when a locally-created record was
  confirmed received by the server.

### Implemented rules (confirmed 2026-10-08, from the 2026-10-06 offline decisions)
- **`POST /sales` is idempotent by `id`.** A retry of a sale this same branch
  and device already recorded returns the original sale with **200** (not 201);
  nothing is re-validated or re-written, so no stock moves twice and no fresh
  manager approval is needed. The same id from a different branch/device is
  **409**. (Two retries racing is handled the same way, never a 500.)
- **`POST /sync/sales` trusts a queued sale with nothing it can't prove.** A
  queued sale carries no staff token and an offline till can't get a discount
  approval (no PIN check offline), so: a line carrying a
  `discount_approval_id` fails that sale (catalog discounts are automatic and
  work offline), and its `staff_id` must be a staff member of the calling device's own
  branch. The already-stored check runs *first*, so retrying an ingested sale
  stays a success even if the staff member has since moved or left; an id
  already stored by another branch/device is a failed item, never a silent
  success. Failures are per-item in the response; the rest of the batch still
  lands.
- **A queued sale is priced from the till's cached catalog.** Its `unit_price`
  and `catalog_discount_percent` snapshot are taken as rung up (percent
  validated 0-100, line recomputed with the Kyat rounding rule), and so is its
  `tax_percent` (the tax rate in the till's cached catalog, 0-100; omitted = the
  product's current rate). The line's tax is worked out from that rate on the
  line total, like online. If the price, the catalog % or the tax % no longer
  matches the product, the sale is still accepted and a `SyncConflict` with
  reason `price_mismatch` is recorded per stale line (one conflict per line,
  whichever of the three went stale; payload: product, the till's
  price/percent/tax %, today's). The customer already paid what the till
  showed. No tax **amount** is ever taken from the till.
- **Known gap:** a sale with a *custom* discount rung up online whose response
  was lost and which then gets queued offline will be rejected on sync (its
  approval id) - retry it through `POST /sales` instead, which succeeds
  idempotently. `price_override` is rejected on both paths.

### Design it follows
1. **Local write-ahead queue on the device.** Every mutation created while
   offline (a completed sale, a drawer event, etc.) is appended to a local
   queue with its already-final client-generated ID, not held in memory
   only.
2. **One-way, idempotent batch upload, device → server.** `POST /sync/sales`
   accepts an array of queued sales. Each is processed by the same
   `CreateSale` logic as a live sale, keyed by its client
   UUID — a duplicate submission (retry after a flaky response) is a safe
   no-op, not a duplicate row. The response reports per-item success/
   failure so the device can drop synced items from its local queue and
   retry only the failures.
3. **Periodic reference-data download, server → device.** For a device to
   ring up sales offline at all, it needs a reasonably fresh local cache
   of: Catalog (products/prices/tax %, plus each combo's component items).
   This is a **read-sync** (`GET /sync/catalog`), refreshed opportunistically
   whenever the device has connectivity — not a live/real-time subscription.
   The snapshot does **not** include stock levels, customers or a staff PIN
   list: an offline till gets no new PIN sign-ins (see "Implemented rules"
   above), so staff must already be signed in when the connection drops.
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
- **Tax collected** (built 2026-10-23; `tax_collected` and
  `net_sales_excl_tax` on `/reports/sales-summary`, `/reports/today` and the
  other reports that carry the headline totals): `tax_collected` = the tax of
  the period's sales (voided sales excluded) - the tax inside what returns gave
  back (`tax_refunded`) + the tax difference of exchanges (`tax_out - tax_in`).
  **Net sales stays what the shop took** (subtotal - discount - returns), which
  for a sale since tax became inclusive **includes** the tax; `net_sales_excl_tax`
  = net sales - tax collected. A sale from before had its tax added on top of its
  total, so its net sales never contained it: it counts as tax collected but is
  not subtracted again. Profit & loss takes gross profit on `net_sales_excl_tax`
  (tax inside a price is not the shop's revenue). The lists in these reports are
  never null.

### Organization timezone — what "today" means (built 2026-10-08)
Every org has an IANA `timezone` (default `Asia/Yangon`, UTC+6:30), set at
provisioning (`POST /internal/accounts` `timezone`, optional) or changed later
with `PATCH /internal/organizations/:id/timezone` (Shagan team only; unknown
zone → 400, unknown org → 404). It defines the org's **calendar day**:
- the dashboard's "today" (`/reports/home-summary`, `/reports/today`) is the
  org's local day, not the UTC day — before this, "today" rolled over at 06:30
  Myanmar time;
- `from`/`to` (`YYYY-MM-DD`, inclusive) on every report **and** on
  `GET /sales` are days of the org's zone, and the default "last 30 days" ends
  today in that zone;
- daily/weekly/monthly buckets and the hourly trend are cut on the org's
  local clock (a sale at 00:15 Yangon time belongs to that local date even
  though it is the previous date in UTC);
- `expenses.date` is a plain DATE, so P&L compares it as calendar dates, never
  as instants.
Timestamps stay stored as instants (UTC), so changing a zone rewrites nothing —
it only moves where the day boundaries fall. One zone per org (not per branch);
a per-branch override is not built. The runtime embeds the tz database
(`time/tzdata`), so zones resolve in the alpine image too.

### Sales history list (built 2026-10-08)
`GET /sales` is `{sales, page, page_size, total_count}` (the same envelope and
`page`/`page_size` rules as `/reports/transactions`: default 20, max 100), newest
first, filterable by `branch_id`, `from` and `to` (`YYYY-MM-DD`, inclusive, days of
the org's timezone). Unlike the reports there is **no default date window** — no filters
means the whole history, paged. A branch-bound POS-device token is locked to its
own branch (a `branch_id` it sends is ignored), exactly like the reports. Every
row, here and in `/reports/transactions`, carries `payment_methods`: the
distinct methods used on the sale, sorted (`["cash"]`, `["qr"]`,
`["cash","qr"]` for a split), never null. Voided sales appear in `/sales`
(status shows it) but not in the reports.

---

## 12. Open Questions — Do Not Assume, Confirm First

1. **Return time window** — is there one (7/14/30 days)? None is enforced
   today. (Refund method is settled: explicit `cash`/`qr`, Section 9.)
2. **Exchange shape** — built as one combined `Exchange` record; confirm it, or
   re-model as a chained Return-then-Sale (Section 9).
3. **Customer `Tags`/`Consent`** are fully built in the backend but not
   surfaced anywhere in the Back Office UI — deferred, to be discussed
   later.
4. ~~`price_override` on a sale item~~ — settled by Discount V1: it is rejected
   (400) on every path; a price change is a product edit by the Owner.
5. **Return/Exchange by the Owner** — Void is open to an Owner acting directly
   (no PIN); Return and Exchange still need a staff token. Should the owner be
   able to do those too?
6. **Reads still open to a POS-device token with no staff token** — reports,
   audit log, sales, inventory, customers and staff reads only restrict a caller
   who presents a Back Office staff token (Section 2); a till that sends none is
   not restricted, because the till reads the same endpoints. Also still open:
   `GET /suppliers`, `GET /purchase-orders`, `GET /sync/conflicts` and
   `POST /printers/test`. Decide whether the Back Office should require its
   staff token on every read (it would break the till's own reads unless those
   move to separate endpoints), and whether suppliers/purchase orders get
   `view_*` permissions.
7. ~~Part B discounts~~ — replaced by Discount V1 (Section 8, "Discounts").
8. **Per-branch timezone** — an org has one timezone (Section 11); a branch in
   another zone isn't supported.
9. ~~Branch prices: may a manager edit their own branch's prices?~~ - settled
   2026-10-24: yes, with `manage_branch_prices`, own branch only (Section 8).
   Still deferred: combo prices per branch, scheduled price changes, "not sold
   at this branch", bulk edit.
10. **Subscription tiers** - built on the spec's defaults (all current features
   in every tier incl. branch prices and custom roles; 14-day Pro trial; 7-day
   grace; limits for everyone). Still the business's call before the first paying
   customer: moving branch prices or custom roles up to Pro.

---

## 13. Team Ownership (as of 2026-10-08 — re-verify before trusting)

- **Chan**: `internal/identity`, `internal/customer`, `internal/shift`,
  `internal/sales`, `internal/audit`, `internal/datasync`,
  `internal/reports`
- **Kit**: `internal/catalog`, `internal/inventory`, `internal/returns`,
  `internal/procurement`, `internal/platform`
- **`internal/common`**: shared, all three

Every domain is built (as of 2026-10-08 no `ErrNotImplemented` stub remains in
any `internal/<domain>/*repository_impl.go`) — re-run
`grep -c ErrNotImplemented internal/<domain>/*repository_impl.go` rather than
trusting a status written here. The module split is a convention, not a lock:
cross-module changes happen, but tell the owner.
