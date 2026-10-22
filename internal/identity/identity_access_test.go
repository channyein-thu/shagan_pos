package identity

import (
	"testing"

	"github.com/stretchr/testify/require"

	"shagan_pos/internal/authtoken"
)

// authtoken expands a pre-migration token with permission codes it spells out
// as plain strings (it can't import identity). This keeps those strings
// honest: every code it adds exists in the catalogue, and a legacy Manager /
// Cashier token ends up with no more than the default Manager / Cashier role
// has - so a signed-in till never gains anything from the expansion that the
// migrated role itself wouldn't grant.
func TestLegacyTokenExpansion_StaysWithinTheDefaultRolesAndTheCatalogue(t *testing.T) {
	legacyCashier := (&authtoken.StaffClaims{Permissions: []string{PermAccessPOSPortal}}).EffectivePermissions()
	legacyManager := (&authtoken.StaffClaims{Permissions: []string{
		PermAccessPOSPortal, PermAccessBackOffice, PermOpenDrawerNoSale, PermApplyManualDiscount,
		PermApproveVoid, PermApproveReturn, PermApproveExchange,
	}}).EffectivePermissions()

	for _, code := range legacyCashier {
		require.Contains(t, DefaultRoles[1].Grants, code, "legacy cashier expansion adds %s which the default Cashier lacks", code)
	}
	for _, code := range legacyManager {
		require.Contains(t, PermissionCodes(), code, "expansion adds %s which is not in the catalogue", code)
		require.Contains(t, DefaultRoles[0].Grants, code, "legacy manager expansion adds %s which the default Manager lacks", code)
	}
	// and it covers every permission the till routes now enforce for those roles
	for _, code := range DefaultRoles[1].Grants {
		require.Contains(t, legacyCashier, code)
	}
	for _, code := range []string{
		PermForceCloseShift, PermViewReports, PermViewItemCost, PermViewSales, PermViewAuditLog, PermManageCatalog,
		PermViewInventory, PermManageStock, PermManageSuppliers, PermManagePurchaseOrders, PermReceiveStock,
		PermViewCustomers, PermManageCustomers, PermViewStaff, PermManageExpenses, PermManageReceiptSettings,
		PermResolveSyncConflicts,
	} {
		require.Contains(t, legacyManager, code)
	}
}
