# Status

As of **2026-10-23**. "Retail" = shagan-retail, "Team" = shagan_team.

## Summary

| Area | State |
|---|---|
| Backend API (all domains) | ✅ Built. ~125 routes, ~740 tests. One stub: `POST /reports/export` (501) |
| Team: tenant provisioning | ✅ Works end to end against `/internal/*`; polish and security gaps open |
| Retail: Back Office | ✅ Integrated for the owner and for a manager at a till |
| Retail: till, online | ✅ Full shift: PIN → open shift → sell → payments → after-sale → close |
| Retail: receipt printer + cash drawer | ✅ Built; **not yet verified on the real device** |
| Retail: offline mode | ❌ Not started (backend side is ready) |
| Retail: customer display (second screen) | 📝 Designed, not built |
| Retail: Service Center portal | ❌ Stub |
| Automated frontend tests | ❌ None |

## Done

**Backend (shagan_pos)**
- Identity and access:
  - Owner, service-center and POS-device logins.
  - Cashier PIN with lockout.
  - Manager approvals.
  - Per-organisation roles and permissions.
  - Organisation timezone.
- Catalog: products with images, categories, combos, catalog discount
  percent, discount approvals.
- Sales, held sales, reprint.
- Void, return, exchange with manager approval.
- Shifts:
  - Open, close, force-close.
  - Cash reconciliation.
  - Drawer events.
  - Expenses (owners too).
- Inventory: stock levels, adjustments, transfers, append-only ledger.
- Procurement: suppliers, purchase orders, goods receipts.
- Customers and consents; receipt settings; payment QR codes.
- Offline sync APIs (catalog with ETag, batch sales, conflicts).
- Audit log and reports.

**Retail (shagan-retail)**, integrated in phases:

| Phase | Scope |
|---|---|
| 0 | API client, BFF session cookies, token refresh in `proxy.ts` |
| 1 | Real login, routing by account type, products list |
| 2 | Back Office reads: catalog, staff, customers, suppliers / POs, sales history, audit log, stock, dashboard, reports, branch selector |
| 3 | Back Office writes: products (multipart + image), categories, combos, staff with PIN, customers, suppliers, POs + receiving, adjustments, transfers, receipt settings, QR codes |
| 4 | Till happy path: device login → staff PIN → open / resume shift → sell (exact-cent totals, tax, discount) → close shift |
| 5 | Till exceptions: manager approvals, held sales, reprint, whole-sale void, return, exchange, force-close |
| 6 | Manager Back Office at a till (PIN), scoped to the till's branch; roles and access management |
| - | Receipt printing (WebUSB, ESC/POS, 80 / 58 mm, Myanmar / Chinese as raster) and cash drawer (configurable kick, Android-bridge fallback) |

**Team (shagan_team):** buyers (CRM); create an organisation with owner and
service-center logins; branches, devices, POS logins; suspend; reset
passwords; mock school / course products.

## In progress / next

1. **Verify the till hardware on a real Duotu C5:**
   - The Print test page checks raster (`GS v 0`) for Myanmar / Chinese.
   - Check the drawer kick.
   - Serve the app over HTTPS for production.
2. **Catch the retail app up with the latest backend changes (2026-10-22/23).**
   They have not been adopted yet:
   - access-permissions middleware
   - catalog `discount_percent` / `sale_price`
   - discount approvals
   - cash adjustments in shifts

   Re-run the till and Back Office journeys against the current backend.
3. **Customer display** (second screen): a `/customer-display` page fed by the
   till over `BroadcastChannel`. Waiting on how the device puts a page on its
   second screen (launcher setting vs. a small Android wrapper app).
4. **Offline mode (retail Phase 7):**
   - IndexedDB catalog cache and sales outbox; service worker.
   - A sync status widget, and a conflicts screen for the owner.
   - Strict offline rules: no PIN sign-ins, no approvals, no manual discounts.
   - Block close-shift until the outbox is empty.
5. **Team polish:**
   - `auth()` checks in server actions.
   - An organisations list.
   - Show owner / service-center accounts.
   - Fix `.env.example`.
   - Stop committing `dev.db`.
6. **Service Center portal** - scope not decided.

## Blocked on / waiting for the backend

Full text in [backend-recommendations.md](backend-recommendations.md).

| # | Item | State |
|---|---|---|
| 2 | Authorization by account type on reads | Partial. **May be addressed** by the 2026-10-22 access-permissions change - re-check |
| 11 | `/internal` extras (get org by id, ...) | Partly done |
| 16 | Receipt show/hide toggles | Dropped for now |
| 17 | Catalog discounts counted as manual | **Likely addressed** by catalog discount percent + discount approvals - re-check |
| 18 | Expose org timezone to tenants | Timezone exists on the org; check whether `/me` returns it |
| 19 | Owner drawer events | Open - owner drawer opens aren't recorded |
| 20 | Wrong table name in a procurement migration | Open (found 2026-10-23) |
| 21 | No unique index on `stock_levels (product, branch)` | Open (found 2026-10-23) |
| 22 | No database foreign keys | Open - design decision to revisit |
| 23 | Branch-specific product prices | **Built 2026-10-23** (branch price overrides; API in WORKFLOWS §8) - Back Office screens follow |
| 24 | Subscription tiers (Basic / Pro / Enterprise) | **Built 2026-10-27** (limits + trial + /internal; WORKFLOWS §2 "Subscription tiers") - no feature is gated yet; shagan_team / retail screens follow |
| 25 | Shift history (list shifts + `view_shift_history`) | **Built 2026-10-27** (`GET /shifts`, `shift_id` filters; WORKFLOWS §11 "Shift history") - Back Office screens follow |

## Risks

| Risk | Impact | Mitigation |
|---|---|---|
| Hardware untested on the device | Receipts or drawer may not work at go-live | Device test pass (next item 1) |
| Frontend behind the backend | Recent backend changes may break till flows (e.g. new permission checks) | Re-run the journeys; adopt the changes |
| No DB foreign keys / AutoMigrate only | Orphaned rows go unnoticed; no rollback path for schema changes | Versioned migrations + key FKs before production |
| No frontend tests | Regressions found by hand | Playwright on the main till journey |
| Repo tests on SQLite, production on Postgres | Postgres-specific bugs slip through | Run repository tests against Postgres in CI |
| Offline not built | A network drop stops sales at the till | Phase 7 |
| Owner actions partly unaudited | Owner drawer opens leave no record | Recommendation #19 |
| Team server actions rely on `proxy.ts` only | A routing mistake exposes tenant admin actions | Add `auth()` in every action |
| WebUSB needs HTTPS + Chrome | Wrong browser or plain HTTP means no printing | Deployment checklist |
