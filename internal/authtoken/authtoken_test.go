package authtoken

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAccessToken_CarriesAccountType(t *testing.T) {
	secret := []byte("s")
	branch := uint(9)

	tok, err := GenerateAccessToken(secret, 1, 2, "pos", &branch, time.Minute)
	require.NoError(t, err)

	claims, err := ParseAccessToken(secret, tok)
	require.NoError(t, err)
	assert.Equal(t, "pos", claims.AccountType)
	assert.Equal(t, uint(1), claims.UserID)
	assert.Equal(t, uint(2), claims.OrgID)
	require.NotNil(t, claims.BranchID)
	assert.Equal(t, branch, *claims.BranchID)
}

func TestStaffToken_NewTokensCarryTheCurrentAccessVersion(t *testing.T) {
	secret := []byte("s")
	tok, err := GenerateStaffToken(secret, 1, 2, 3, []string{"access_pos_portal"}, time.Minute)
	require.NoError(t, err)

	claims, err := ParseStaffToken(secret, tok)

	require.NoError(t, err)
	assert.Equal(t, CurrentAccessVersion, claims.AccessVersion)
	// a current token is trusted exactly as issued: no expansion
	assert.Equal(t, []string{"access_pos_portal"}, claims.EffectivePermissions())
}

func legacyClaims(perms ...string) *StaffClaims {
	return &StaffClaims{StaffID: 1, Permissions: perms} // AccessVersion 0: minted before per-org roles
}

// A token minted before the migration lists only the seven legacy
// permissions. Until it expires it must keep opening what it opened, even
// though the till routes now check finer-grained permissions.
func TestEffectivePermissions_LegacyCashierTokenKeepsTheTillBasics(t *testing.T) {
	got := legacyClaims("access_pos_portal").EffectivePermissions()

	assert.ElementsMatch(t, []string{
		"access_pos_portal", "accept_payments", "hold_resume_sales", "manage_own_shift",
		"log_till_expenses", "add_customers", "view_all_receipts", "reprint_receipt", "apply_manual_discount",
	}, got)
}

func TestEffectivePermissions_LegacyManagerTokenKeepsBackOfficeAndForceClose(t *testing.T) {
	got := legacyClaims("access_pos_portal", "access_backoffice", "approve_void").EffectivePermissions()

	for _, want := range []string{
		"access_backoffice", "force_close_shift", "view_reports", "view_item_cost", "view_sales", "view_audit_log",
		"manage_catalog", "view_inventory", "manage_stock", "manage_suppliers", "manage_purchase_orders",
		"receive_stock", "view_customers", "manage_customers", "view_staff", "manage_expenses",
		"manage_receipt_settings", "resolve_sync_conflicts", "accept_payments", "approve_void",
	} {
		assert.Contains(t, got, want)
	}
	// staff management was Owner-only before and stays so
	assert.NotContains(t, got, "manage_staff")
}

// Super Staff had no back office: its legacy token must not gain any.
func TestEffectivePermissions_LegacySuperStaffTokenGainsNoBackOffice(t *testing.T) {
	got := legacyClaims("access_pos_portal", "open_drawer_no_sale", "apply_manual_discount").EffectivePermissions()

	assert.NotContains(t, got, "access_backoffice")
	assert.NotContains(t, got, "force_close_shift")
	assert.NotContains(t, got, "view_reports")
	assert.Contains(t, got, "open_drawer_no_sale")
	assert.Contains(t, got, "accept_payments")
}

// A legacy token never gains an approval it didn't have.
func TestEffectivePermissions_LegacyTokenNeverGainsApprovals(t *testing.T) {
	got := legacyClaims("access_pos_portal", "access_backoffice").EffectivePermissions()

	// (it may now ASK for a discount, but approving one is another matter)
	for _, notWanted := range []string{"approve_void", "approve_return", "approve_exchange", "open_drawer_no_sale", "approve_manual_discount", "approve_own_discount"} {
		assert.NotContains(t, got, notWanted)
	}
}

func TestEffectivePermissions_CurrentTokenWithoutPosSwitchIsNotExpanded(t *testing.T) {
	claims := &StaffClaims{AccessVersion: CurrentAccessVersion, Permissions: []string{"view_reports"}}

	assert.Equal(t, []string{"view_reports"}, claims.EffectivePermissions())
}

func TestEffectivePermissions_DoesNotMutateTheClaimsOrDuplicate(t *testing.T) {
	claims := legacyClaims("access_pos_portal", "accept_payments")

	got := claims.EffectivePermissions()

	assert.Equal(t, []string{"access_pos_portal", "accept_payments"}, claims.Permissions)
	seen := map[string]bool{}
	for _, p := range got {
		assert.False(t, seen[p], "duplicate %s", p)
		seen[p] = true
	}
}
