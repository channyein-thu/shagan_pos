package identity

import "slices"

// The permission catalogue and the default role grants - the single source
// of truth for what an Owner can tick under "Access rights" (Role & Access
// Management V1, modelled on Loyverse). Everything that seeds, migrates or
// provisions permissions reads from here, so the lists below can't drift
// from each other.

// Permission categories. A permission's category says which switch it sits
// under in the Owner's checklist: `pos` permissions only mean anything when
// access_pos_portal is on, `backoffice` ones only when access_backoffice is.
const (
	CategoryPOS        = "pos"
	CategoryBackOffice = "backoffice"
)

// Permission codes. The first group is POS (category `pos`), the second Back
// office (category `backoffice`).
const (
	PermAccessPOSPortal       = "access_pos_portal"
	PermAcceptPayments        = "accept_payments"
	PermHoldResumeSales       = "hold_resume_sales"
	PermManageOwnShift        = "manage_own_shift"
	PermForceCloseShift       = "force_close_shift"
	PermLogTillExpenses       = "log_till_expenses"
	PermAddCustomers          = "add_customers"
	PermViewAllReceipts       = "view_all_receipts"
	PermReprintReceipt        = "reprint_receipt"
	PermApplyManualDiscount   = "apply_manual_discount"
	PermApproveManualDiscount = "approve_manual_discount"
	PermApproveOwnDiscount    = "approve_own_discount"
	PermApproveVoid           = "approve_void"
	PermApproveReturn         = "approve_return"
	PermApproveExchange       = "approve_exchange"
	PermOpenDrawerNoSale      = "open_drawer_no_sale"
	PermAccessBackOffice      = "access_backoffice"
	PermViewReports           = "view_reports"
	PermViewItemCost          = "view_item_cost"
	PermViewSales             = "view_sales"
	PermViewAuditLog          = "view_audit_log"
	PermManageCatalog         = "manage_catalog"
	PermManageBranchPrices    = "manage_branch_prices"
	PermViewInventory         = "view_inventory"
	PermManageStock           = "manage_stock"
	PermManageSuppliers       = "manage_suppliers"
	PermManagePurchaseOrders  = "manage_purchase_orders"
	PermReceiveStock          = "receive_stock"
	PermViewCustomers         = "view_customers"
	PermManageCustomers       = "manage_customers"
	PermViewStaff             = "view_staff"
	PermManageStaff           = "manage_staff"
	PermManageExpenses        = "manage_expenses"
	PermManageReceiptSettings = "manage_receipt_settings"
	PermResolveSyncConflicts  = "resolve_sync_conflicts"
)

// PermissionDef is one row of the catalogue.
type PermissionDef struct {
	Code     string
	Name     string
	Category string
}

// PermissionCatalogue is every grantable permission, in the order the Owner's
// checklist shows them (POS first, then Back office).
var PermissionCatalogue = []PermissionDef{
	{PermAccessPOSPortal, "POS switch: sign in to the till with a PIN", CategoryPOS},
	{PermAcceptPayments, "Ring up sales and take payment", CategoryPOS},
	{PermHoldResumeSales, "Hold and resume sales", CategoryPOS},
	{PermManageOwnShift, "Open and close their own shift", CategoryPOS},
	{PermForceCloseShift, "Force-close another staff's shift", CategoryPOS},
	{PermLogTillExpenses, "Log an expense at the till", CategoryPOS},
	{PermAddCustomers, "Add new customers at the till", CategoryPOS},
	{PermViewAllReceipts, "See every receipt at the till (off = only their own)", CategoryPOS},
	{PermReprintReceipt, "Reprint receipts", CategoryPOS},
	{PermApplyManualDiscount, "Ask for a custom discount (needs approval)", CategoryPOS},
	{PermApproveManualDiscount, "Approve manual discounts with their PIN", CategoryPOS},
	{PermApproveOwnDiscount, "Approve a discount on their own sale", CategoryPOS},
	{PermApproveVoid, "Cancel (void) receipts", CategoryPOS},
	{PermApproveReturn, "Refunds (returns)", CategoryPOS},
	{PermApproveExchange, "Exchanges", CategoryPOS},
	{PermOpenDrawerNoSale, "Open the cash drawer without a sale", CategoryPOS},

	{PermAccessBackOffice, "Back office switch: open the Back Office at the till with a PIN", CategoryBackOffice},
	{PermViewReports, "Dashboard and reports", CategoryBackOffice},
	{PermViewItemCost, "See cost prices and profit (P&L, margins)", CategoryBackOffice},
	{PermViewSales, "Sales history and receipts", CategoryBackOffice},
	{PermViewAuditLog, "Audit log", CategoryBackOffice},
	{PermManageCatalog, "Products, categories and combos", CategoryBackOffice},
	{PermManageBranchPrices, "Set their branch's prices", CategoryBackOffice},
	{PermViewInventory, "Stock levels and inventory ledger", CategoryBackOffice},
	{PermManageStock, "Stock adjustments and transfers", CategoryBackOffice},
	{PermManageSuppliers, "Suppliers", CategoryBackOffice},
	{PermManagePurchaseOrders, "Create and edit purchase orders", CategoryBackOffice},
	{PermReceiveStock, "Receive purchase orders (goods receipts)", CategoryBackOffice},
	{PermViewCustomers, "Customer list", CategoryBackOffice},
	{PermManageCustomers, "Edit customers and consents", CategoryBackOffice},
	{PermViewStaff, "Staff list (read only)", CategoryBackOffice},
	{PermManageStaff, "Add and edit staff, PINs, statuses (own branch)", CategoryBackOffice},
	{PermManageExpenses, "Edit or delete any expense at the branch", CategoryBackOffice},
	{PermManageReceiptSettings, "Receipt settings (own branch)", CategoryBackOffice},
	{PermResolveSyncConflicts, "Resolve offline sync conflicts", CategoryBackOffice},
}

// PermissionCategoryOf returns code's catalogue category, or "" for a code
// that isn't in the catalogue.
func PermissionCategoryOf(code string) string {
	for _, def := range PermissionCatalogue {
		if def.Code == code {
			return def.Category
		}
	}
	return ""
}

// PermissionCodes lists every catalogue code, in catalogue order.
func PermissionCodes() []string {
	codes := make([]string, len(PermissionCatalogue))
	for i, def := range PermissionCatalogue {
		codes[i] = def.Code
	}
	return codes
}

// LegacyPermissionCodes are the seven permissions that existed before the
// catalogue grew. A pre-migration database has grants for exactly these, and
// the migration carries those over verbatim - see MigrateAccessRights.
var LegacyPermissionCodes = []string{
	PermAccessPOSPortal, PermAccessBackOffice, PermOpenDrawerNoSale, PermApplyManualDiscount,
	PermApproveVoid, PermApproveReturn, PermApproveExchange,
}

// Default role codes. Manager and Cashier exist in every org and can't be
// deleted; Super Staff is only ever migrated into orgs that already used it.
const (
	RoleCodeManager    = "manager"
	RoleCodeCashier    = "cashier"
	RoleCodeSuperStaff = "super_staff"
	// RoleCodeOwner is the synthetic, fixed row shown first in GET /roles.
	// The Owner is the org's own login account, not a staff role.
	RoleCodeOwner = "owner"
)

// legacyRoleCodeToOrgRoleCode maps the old global role codes onto the codes
// of the per-org roles that replace them: `staff` became `cashier`.
var legacyRoleCodeToOrgRoleCode = map[string]string{
	"staff":       RoleCodeCashier,
	"manager":     RoleCodeManager,
	"super_staff": RoleCodeSuperStaff,
}

// DefaultRole is one role every org starts with (or, for Super Staff, a
// migrated custom role), and its default grants.
type DefaultRole struct {
	Code   string
	Name   string
	System bool
	Grants []string
}

var cashierGrants = []string{
	PermAccessPOSPortal, PermAcceptPayments, PermHoldResumeSales, PermManageOwnShift,
	PermLogTillExpenses, PermAddCustomers, PermViewAllReceipts, PermReprintReceipt,
	// A cashier may ASK for a custom discount: it opens the dialog. It never
	// gets one alone - the approval needs someone holding
	// approve_manual_discount, which a Cashier doesn't - and an Owner can take
	// this off any role to stop it from even asking.
	PermApplyManualDiscount,
}

// DefaultRoles are the roles a new org gets. Manager holds everything in the
// catalogue (manage_branch_prices included) except manage_staff, which stays Owner-only until an Owner grants
// it (staff are provisioned by the Owner today).
var DefaultRoles = []DefaultRole{
	{
		Code: RoleCodeManager, Name: "Manager", System: true,
		Grants: slices.DeleteFunc(PermissionCodes(), func(code string) bool { return code == PermManageStaff }),
	},
	{Code: RoleCodeCashier, Name: "Cashier", System: true, Grants: cashierGrants},
}

// SuperStaffRole is the custom role migrated into orgs whose staff used the
// old global `super_staff`. Same grants it had, plus the new till permissions
// that every till role holds by default.
var SuperStaffRole = DefaultRole{
	Code: RoleCodeSuperStaff, Name: "Super Staff", System: false,
	Grants: []string{
		PermAccessPOSPortal, PermAcceptPayments, PermHoldResumeSales, PermManageOwnShift,
		PermLogTillExpenses, PermAddCustomers, PermViewAllReceipts, PermReprintReceipt,
		PermApplyManualDiscount, PermApproveManualDiscount, PermOpenDrawerNoSale,
	},
}

// defaultRoleForLegacyCode returns the default role replacing the old global
// role code, if there is one.
func defaultRoleForLegacyCode(legacyCode string) (DefaultRole, bool) {
	newCode, ok := legacyRoleCodeToOrgRoleCode[legacyCode]
	if !ok {
		return DefaultRole{}, false
	}
	switch newCode {
	case RoleCodeManager:
		return DefaultRoles[0], true
	case RoleCodeCashier:
		return DefaultRoles[1], true
	case RoleCodeSuperStaff:
		return SuperStaffRole, true
	}
	return DefaultRole{}, false
}

// isLegacyPermission reports whether code is one of the seven pre-catalogue
// permissions.
func isLegacyPermission(code string) bool {
	return slices.Contains(LegacyPermissionCodes, code)
}
