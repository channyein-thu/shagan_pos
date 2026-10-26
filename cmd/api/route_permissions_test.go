package api

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"

	"shagan_pos/internal/authtoken"
	"shagan_pos/internal/identity"
	"shagan_pos/internal/middleware"
)

// This is the route/permission audit for Role & Access Management: every
// authenticated route the API registers must be listed below with the guard
// that protects it, and the test drives each guard with real tokens. Adding a
// route without deciding who may call it fails TestEveryRouteHasADeclaredGuard,
// which is the point - nothing becomes reachable by accident.
//
// No database is wired: a request that gets through its guard reaches a
// handler that fails on the missing DB (recovered as a 500) or on its empty
// body (400). Both mean "the guard let it in"; only 401/403 mean it was refused.

type guardKind int

const (
	guardOpen           guardKind = iota // any authenticated caller (plain till/back office reads, own-account routes)
	guardOrgAdmin                        // Owner / Service Center only
	guardStaffToken                      // any valid staff token (the handler then checks a permission inline), or an Owner where the route says so
	guardStaffPerm                       // the staff token must carry the permission
	guardStaffOrAdmin                    // an Owner passes; a till's staff token must carry one of the permissions
	guardBackOfficePerm                  // an Owner passes; a till needs access_backoffice AND the permission
	guardView                            // restricts only a caller using Back Office; plain till reads unchanged
)

type routeRule struct {
	kind  guardKind
	perms []string
}

func open() routeRule                    { return routeRule{kind: guardOpen} }
func orgAdmin() routeRule                { return routeRule{kind: guardOrgAdmin} }
func staffToken() routeRule              { return routeRule{kind: guardStaffToken} }
func staffPerm(p string) routeRule       { return routeRule{guardStaffPerm, []string{p}} }
func staffOrAdmin(p ...string) routeRule { return routeRule{guardStaffOrAdmin, p} }
func backOfficePerm(p string) routeRule  { return routeRule{guardBackOfficePerm, []string{p}} }
func view(p string) routeRule            { return routeRule{guardView, []string{p}} }

var routePermissions = map[string]routeRule{
	// identity
	"POST /api/v1/auth/logout":                  open(),
	"GET /api/v1/me":                            open(),
	"PATCH /api/v1/me":                          open(),
	"POST /api/v1/staff/:id/manager-pin/verify": open(),
	"POST /api/v1/staff/:id/pin/verify":         open(),
	"POST /api/v1/devices":                      orgAdmin(),
	"GET /api/v1/devices":                       open(),
	"PATCH /api/v1/devices/:id":                 orgAdmin(),
	"GET /api/v1/branches":                      open(),
	"POST /api/v1/branches":                     orgAdmin(),
	"GET /api/v1/branches/:id":                  open(),
	"PATCH /api/v1/branches/:id":                orgAdmin(),
	"GET /api/v1/branches/:id/staff":            view(identity.PermViewStaff),
	"GET /api/v1/branches/:id/managers":         open(),
	"GET /api/v1/staff":                         view(identity.PermViewStaff),
	"POST /api/v1/staff":                        backOfficePerm(identity.PermManageStaff),
	"GET /api/v1/staff/:id":                     view(identity.PermViewStaff),
	"PATCH /api/v1/staff/:id":                   backOfficePerm(identity.PermManageStaff),
	"GET /api/v1/roles":                         orgAdmin(),
	"POST /api/v1/roles":                        orgAdmin(),
	"PATCH /api/v1/roles/:id":                   orgAdmin(),
	"DELETE /api/v1/roles/:id":                  orgAdmin(),
	"GET /api/v1/permissions":                   orgAdmin(),
	"GET /api/v1/roles/:id/permissions":         orgAdmin(),
	// customers
	"GET /api/v1/customers":               view(identity.PermViewCustomers),
	"POST /api/v1/customers":              staffOrAdmin(identity.PermAddCustomers, identity.PermManageCustomers),
	"GET /api/v1/customers/:id":           view(identity.PermViewCustomers),
	"GET /api/v1/customers/:id/consents":  view(identity.PermViewCustomers),
	"POST /api/v1/customers/:id/consents": staffOrAdmin(identity.PermAddCustomers, identity.PermManageCustomers),
	// platform
	"GET /api/v1/receipt-settings":                       open(),
	"PUT /api/v1/receipt-settings":                       backOfficePerm(identity.PermManageReceiptSettings),
	"POST /api/v1/printers/test":                         open(),
	"POST /api/v1/branches/:id/payment-qr-codes":         orgAdmin(),
	"GET /api/v1/branches/:id/payment-qr-codes":          open(),
	"DELETE /api/v1/branches/:id/payment-qr-codes/:qrId": orgAdmin(),
	// catalog
	"GET /api/v1/products":               open(),
	"GET /api/v1/products/:id":           open(),
	"GET /api/v1/products/barcode/:code": open(),
	"POST /api/v1/products":              backOfficePerm(identity.PermManageCatalog),
	"PATCH /api/v1/products/:id":         backOfficePerm(identity.PermManageCatalog),
	"DELETE /api/v1/products/:id":        backOfficePerm(identity.PermManageCatalog),
	// branch price overrides: reading needs Back Office + manage_catalog (a till's
	// manager sees only their own branch); writing needs manage_branch_prices (a
	// till's manager may write only their own branch - the service checks it)
	"GET /api/v1/products/:id/branch-prices":              backOfficePerm(identity.PermManageCatalog),
	"PUT /api/v1/products/:id/branch-prices/:branchId":    backOfficePerm(identity.PermManageBranchPrices),
	"DELETE /api/v1/products/:id/branch-prices/:branchId": backOfficePerm(identity.PermManageBranchPrices),
	"GET /api/v1/categories":                              open(),
	"POST /api/v1/categories":                             backOfficePerm(identity.PermManageCatalog),
	"PATCH /api/v1/categories/:id":                        backOfficePerm(identity.PermManageCatalog),
	"DELETE /api/v1/categories/:id":                       backOfficePerm(identity.PermManageCatalog),
	"GET /api/v1/combos":                                  open(),
	"POST /api/v1/combos":                                 backOfficePerm(identity.PermManageCatalog),
	"PATCH /api/v1/combos/:id":                            backOfficePerm(identity.PermManageCatalog),
	"DELETE /api/v1/combos/:id":                           backOfficePerm(identity.PermManageCatalog),
	// procurement
	"GET /api/v1/suppliers":                     open(),
	"POST /api/v1/suppliers":                    backOfficePerm(identity.PermManageSuppliers),
	"PATCH /api/v1/suppliers/:id":               backOfficePerm(identity.PermManageSuppliers),
	"DELETE /api/v1/suppliers/:id":              backOfficePerm(identity.PermManageSuppliers),
	"GET /api/v1/purchase-orders":               open(),
	"POST /api/v1/purchase-orders":              backOfficePerm(identity.PermManagePurchaseOrders),
	"GET /api/v1/purchase-orders/:id":           open(),
	"PATCH /api/v1/purchase-orders/:id":         backOfficePerm(identity.PermManagePurchaseOrders),
	"POST /api/v1/purchase-orders/:id/receipts": backOfficePerm(identity.PermReceiveStock),
	// inventory
	"GET /api/v1/stock-levels":           view(identity.PermViewInventory),
	"GET /api/v1/inventory/low-stock":    view(identity.PermViewInventory),
	"GET /api/v1/inventory/ledger":       view(identity.PermViewInventory),
	"POST /api/v1/inventory/adjustments": backOfficePerm(identity.PermManageStock),
	"GET /api/v1/stock-transfers":        view(identity.PermViewInventory),
	"POST /api/v1/stock-transfers":       backOfficePerm(identity.PermManageStock),
	"PATCH /api/v1/stock-transfers/:id":  backOfficePerm(identity.PermManageStock),
	// sales
	"POST /api/v1/sales":                          staffPerm(identity.PermAcceptPayments),
	"POST /api/v1/sales/discount-approvals":       staffPerm(identity.PermApplyManualDiscount),
	"DELETE /api/v1/sales/discount-approvals/:id": staffToken(),
	"GET /api/v1/sales":                           view(identity.PermViewSales),
	"GET /api/v1/sales/:id":                       view(identity.PermViewSales),
	"GET /api/v1/sales/:id/receipt":               view(identity.PermViewSales),
	"POST /api/v1/sales/:id/reprint":              staffOrAdmin(identity.PermReprintReceipt),
	"POST /api/v1/held-sales":                     staffPerm(identity.PermHoldResumeSales),
	"GET /api/v1/held-sales":                      open(),
	"DELETE /api/v1/held-sales/:id":               staffPerm(identity.PermHoldResumeSales),
	// returns / voids / exchanges: any staff token, the handler checks the
	// approve_* permission itself (own, or via X-Manager-Approval-Token)
	"POST /api/v1/sales/:id/void": staffToken(),
	"GET /api/v1/voids":           open(),
	"POST /api/v1/returns":        staffToken(),
	"GET /api/v1/returns":         open(),
	"GET /api/v1/returns/:id":     open(),
	"POST /api/v1/exchanges":      staffToken(),
	"GET /api/v1/exchanges":       open(),
	"GET /api/v1/exchanges/:id":   open(),
	// shifts, drawer, expenses
	"POST /api/v1/shifts":                    staffPerm(identity.PermManageOwnShift),
	"GET /api/v1/shifts/current":             open(),
	"GET /api/v1/shifts/:id":                 open(),
	"POST /api/v1/shifts/:id/close":          staffPerm(identity.PermManageOwnShift),
	"POST /api/v1/shifts/:id/force-close":    staffPerm(identity.PermForceCloseShift),
	"GET /api/v1/shifts/:id/summary":         open(),
	"GET /api/v1/shifts/:id/reconciliations": open(),
	"POST /api/v1/drawer-events":             staffToken(),
	"GET /api/v1/drawer-events":              open(),
	"GET /api/v1/expenses":                   open(),
	"POST /api/v1/expenses":                  staffOrAdmin(identity.PermLogTillExpenses),
	"PATCH /api/v1/expenses/:id":             staffToken(),
	"DELETE /api/v1/expenses/:id":            staffToken(),
	// offline sync
	"GET /api/v1/sync/catalog":                 open(),
	"POST /api/v1/sync/sales":                  open(),
	"GET /api/v1/sync/status":                  open(),
	"GET /api/v1/sync/conflicts":               open(),
	"PATCH /api/v1/sync/conflicts/:id/resolve": backOfficePerm(identity.PermResolveSyncConflicts),
	// audit and reports
	"GET /api/v1/audit-log":               view(identity.PermViewAuditLog),
	"GET /api/v1/reports/home-summary":    view(identity.PermViewReports),
	"GET /api/v1/reports/stock-overview":  view(identity.PermViewReports),
	"GET /api/v1/reports/today":           view(identity.PermViewReports),
	"GET /api/v1/reports/revenue-trend":   view(identity.PermViewReports),
	"GET /api/v1/reports/sales-summary":   view(identity.PermViewReports),
	"GET /api/v1/reports/sales-trend":     view(identity.PermViewReports),
	"GET /api/v1/reports/payment-methods": view(identity.PermViewReports),
	"GET /api/v1/reports/discounts":       view(identity.PermViewReports),
	"GET /api/v1/reports/transactions":    view(identity.PermViewReports),
	"GET /api/v1/reports/product-sales":   view(identity.PermViewReports),
	"GET /api/v1/reports/top-products":    view(identity.PermViewReports),
	"GET /api/v1/reports/profit-loss":     view(identity.PermViewReports),
	"POST /api/v1/reports/export":         view(identity.PermViewReports),
}

var routeTestSecret = []byte("route-permissions-test-secret")

func newRouteTestEngine() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(gin.Recovery())
	v1 := r.Group("/api/v1")
	v1.Use(middleware.Auth(routeTestSecret))

	// No database and no object store: see the note at the top of the file.
	identityAPI := NewIdentityAPI(nil, routeTestSecret, time.Hour, time.Hour, time.Hour, time.Minute)
	identityAPI.RegisterRoutes(v1)
	NewCustomerAPI(nil, routeTestSecret).RegisterRoutes(v1)
	NewPlatformAPI(nil, nil, routeTestSecret).RegisterRoutes(v1)
	NewCatalogAPI(nil, nil, routeTestSecret).RegisterRoutes(v1)
	NewProcurementAPI(nil, routeTestSecret).RegisterRoutes(v1)
	NewInventoryAPI(nil, routeTestSecret).RegisterRoutes(v1)
	NewSalesAPI(nil, routeTestSecret).RegisterRoutes(v1)
	NewReturnsAPI(nil, routeTestSecret).RegisterRoutes(v1)
	NewShiftAPI(nil, routeTestSecret).RegisterRoutes(v1)
	NewSyncAPI(nil, nil, routeTestSecret).RegisterRoutes(v1)
	NewAuditAPI(nil, routeTestSecret).RegisterRoutes(v1)
	NewReportsAPI(nil, routeTestSecret).RegisterRoutes(v1)
	return r
}

func routeKey(method, path string) string { return method + " " + path }

func TestEveryRouteHasADeclaredGuard(t *testing.T) {
	registered := map[string]bool{}
	for _, route := range newRouteTestEngine().Routes() {
		registered[routeKey(route.Method, route.Path)] = true
	}

	for key := range registered {
		_, declared := routePermissions[key]
		require.True(t, declared, "route %s is registered but has no guard declared in routePermissions - decide who may call it", key)
	}
	for key := range routePermissions {
		require.True(t, registered[key], "routePermissions lists %s, which is not a registered route (stale entry or typo)", key)
	}
}

func accessTokenFor(t *testing.T, accountType string) string {
	t.Helper()
	var branch *uint
	if accountType == middleware.AccountTypePos {
		b := uint(5)
		branch = &b
	}
	tok, err := authtoken.GenerateAccessToken(routeTestSecret, 1, 1, accountType, branch, time.Minute)
	require.NoError(t, err)
	return tok
}

func staffTokenWith(t *testing.T, perms ...string) string {
	t.Helper()
	tok, err := authtoken.GenerateStaffToken(routeTestSecret, 7, 5, 3, perms, time.Minute)
	require.NoError(t, err)
	return tok
}

// call sends one request and reports whether a guard refused it.
func call(t *testing.T, r *gin.Engine, key, bearer, staff string) (status int, refused bool) {
	t.Helper()
	method, pattern, _ := strings.Cut(key, " ")
	// Every path parameter becomes 5: the till under test is branch 5, so a
	// branch-scoped route (/branches/:id/...) is its own branch, and the other
	// handlers just parse a number.
	path := pattern
	for _, seg := range strings.Split(pattern, "/") {
		if strings.HasPrefix(seg, ":") {
			path = strings.Replace(path, seg, "5", 1)
		}
	}
	req := httptest.NewRequest(method, path, strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+bearer)
	if staff != "" {
		req.Header.Set("X-Staff-Token", staff)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w.Code, w.Code == http.StatusUnauthorized || w.Code == http.StatusForbidden
}

func TestRouteGuards(t *testing.T) {
	r := newRouteTestEngine()
	owner := accessTokenFor(t, middleware.AccountTypeOwner)
	serviceCenter := accessTokenFor(t, middleware.AccountTypeServiceCenter)
	pos := accessTokenFor(t, middleware.AccountTypePos)

	keys := make([]string, 0, len(routePermissions))
	for k := range routePermissions {
		keys = append(keys, k)
	}
	slices.Sort(keys)

	for _, key := range keys {
		rule := routePermissions[key]
		t.Run(key, func(t *testing.T) {
			passes := func(bearer, staff string) {
				t.Helper()
				status, refused := call(t, r, key, bearer, staff)
				require.False(t, refused, "expected %s to be let through, got %d", key, status)
			}
			refusedWith := func(want int, bearer, staff string) {
				t.Helper()
				status, _ := call(t, r, key, bearer, staff)
				require.Equal(t, want, status, "%s", key)
			}
			// a till's token with just the POS switch: a plain cashier
			cashier := staffTokenWith(t, identity.PermAccessPOSPortal)

			switch rule.kind {
			case guardOpen:
				// a till is enough; some of these need a branch, which an
				// Owner's token has none of, so only the till is driven
				passes(pos, "")

			case guardOrgAdmin:
				passes(owner, "")
				passes(serviceCenter, "")
				// a till is refused even with a manager's staff token
				refusedWith(http.StatusForbidden, pos, staffTokenWith(t, identity.PermAccessBackOffice, identity.PermManageStaff))

			case guardStaffToken:
				passes(pos, staffTokenWith(t, identity.PermAccessPOSPortal))
				refusedWith(http.StatusUnauthorized, pos, "")

			case guardStaffPerm:
				perm := rule.perms[0]
				passes(pos, staffTokenWith(t, identity.PermAccessPOSPortal, perm))
				refusedWith(http.StatusForbidden, pos, cashier)
				refusedWith(http.StatusUnauthorized, pos, "")

			case guardStaffOrAdmin:
				for _, perm := range rule.perms {
					passes(pos, staffTokenWith(t, identity.PermAccessPOSPortal, perm))
				}
				passes(owner, "")
				passes(serviceCenter, "")
				refusedWith(http.StatusForbidden, pos, cashier)
				refusedWith(http.StatusUnauthorized, pos, "")

			case guardBackOfficePerm:
				perm := rule.perms[0]
				passes(owner, "")
				passes(serviceCenter, "")
				passes(pos, staffTokenWith(t, identity.PermAccessBackOffice, perm))
				// the permission without the Back office switch, and the switch without the permission
				refusedWith(http.StatusForbidden, pos, staffTokenWith(t, identity.PermAccessPOSPortal, perm))
				refusedWith(http.StatusForbidden, pos, staffTokenWith(t, identity.PermAccessBackOffice))
				refusedWith(http.StatusUnauthorized, pos, "")

			case guardView:
				perm := rule.perms[0]
				passes(owner, "")
				passes(serviceCenter, "")
				passes(pos, "")      // a plain till read, as before
				passes(pos, cashier) // a cashier's token is a plain till read too
				passes(pos, staffTokenWith(t, identity.PermAccessBackOffice, perm))
				// but a caller using Back Office needs the view permission
				refusedWith(http.StatusForbidden, pos, staffTokenWith(t, identity.PermAccessBackOffice))
			}
		})
	}
}

// Tokens minted before per-org roles hold only the seven legacy codes. They
// must keep opening what they opened, until they expire - the deploy must not
// log every till out mid-shift.
func TestLegacyStaffTokensKeepWorkingAcrossTheDeploy(t *testing.T) {
	r := newRouteTestEngine()
	pos := accessTokenFor(t, middleware.AccountTypePos)
	legacy := func(perms ...string) string {
		tok, err := legacyTokenForTest(perms...)
		require.NoError(t, err)
		return tok
	}
	cashier := legacy("access_pos_portal")
	manager := legacy("access_pos_portal", "access_backoffice", "approve_void")

	for _, key := range []string{
		"POST /api/v1/sales", "POST /api/v1/held-sales", "DELETE /api/v1/held-sales/:id",
		"POST /api/v1/shifts", "POST /api/v1/shifts/:id/close", "POST /api/v1/expenses",
		"POST /api/v1/customers", "POST /api/v1/sales/:id/reprint",
	} {
		_, refused := call(t, r, key, pos, cashier)
		require.False(t, refused, "a pre-migration cashier token must still reach %s", key)
	}
	for _, key := range []string{
		"POST /api/v1/products", "POST /api/v1/suppliers", "POST /api/v1/purchase-orders",
		"POST /api/v1/purchase-orders/:id/receipts", "POST /api/v1/inventory/adjustments",
		"PUT /api/v1/receipt-settings", "PATCH /api/v1/sync/conflicts/:id/resolve",
		"POST /api/v1/shifts/:id/force-close",
	} {
		_, refused := call(t, r, key, pos, manager)
		require.False(t, refused, "a pre-migration manager token must still reach %s", key)
	}
	// ...and a legacy cashier still can't do a manager's work
	for _, key := range []string{"POST /api/v1/products", "POST /api/v1/shifts/:id/force-close", "PUT /api/v1/receipt-settings"} {
		status, _ := call(t, r, key, pos, cashier)
		require.Equal(t, http.StatusForbidden, status, key)
	}
	// manage_staff did not exist, so no legacy token gains it
	status, _ := call(t, r, "POST /api/v1/staff", pos, manager)
	require.Equal(t, http.StatusForbidden, status)
}

// legacyTokenForTest mints a staff token the way the API did before per-org
// roles: no access_version claim, only the seven original permission codes.
func legacyTokenForTest(perms ...string) (string, error) {
	claims := authtoken.StaffClaims{
		StaffID: 7, BranchID: 5, RoleID: 3, Permissions: perms,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Minute)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(routeTestSecret)
}
