//go:build cgo

package identity

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func newAccessTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	name := strings.NewReplacer("/", "_", " ", "_").Replace(t.Name())
	db, err := gorm.Open(
		sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", name)),
		&gorm.Config{Logger: logger.Default.LogMode(logger.Silent)},
	)
	require.NoError(t, err)
	require.NoError(t, AutoMigrate(db))
	return db
}

// legacyWorld is a database as it was before per-org roles: three global
// roles, the seven original permissions, and the grant matrix of the last
// seed (staff: pos; super_staff: pos + drawer + discount; manager: all but
// nothing new).
type legacyWorld struct {
	roleIDs map[string]uint // legacy role code -> global role id
}

var legacyGrantMatrix = map[string][]string{
	"staff":       {"access_pos_portal"},
	"super_staff": {"access_pos_portal", "open_drawer_no_sale", "apply_manual_discount"},
	"manager": {
		"access_pos_portal", "access_backoffice", "open_drawer_no_sale", "apply_manual_discount",
		"approve_void", "approve_return", "approve_exchange",
	},
}

func seedLegacyWorld(t *testing.T, db *gorm.DB) legacyWorld {
	t.Helper()
	world := legacyWorld{roleIDs: map[string]uint{}}
	permIDs := map[string]uint{}
	for _, code := range LegacyPermissionCodes {
		p := Permission{Code: code, Name: "legacy " + code, Category: "sales"}
		require.NoError(t, db.Create(&p).Error)
		permIDs[code] = p.ID
	}
	for code, grants := range legacyGrantMatrix {
		role := Role{Code: code, Name: strings.ToUpper(code)}
		require.NoError(t, db.Create(&role).Error)
		world.roleIDs[code] = role.ID
		for _, g := range grants {
			require.NoError(t, db.Create(&RolePermission{RoleID: role.ID, PermissionID: permIDs[g]}).Error)
		}
	}
	return world
}

func seedOrgWithStaff(t *testing.T, db *gorm.DB, name string, roleIDs ...uint) (org Organization, staff []Staff) {
	t.Helper()
	org = Organization{Name: name, Timezone: "Asia/Yangon"}
	require.NoError(t, db.Create(&org).Error)
	branch := Branch{OrgID: org.ID, Name: name + " main", Status: BranchStatusActive}
	require.NoError(t, db.Create(&branch).Error)
	for i, roleID := range roleIDs {
		s := Staff{BranchID: branch.ID, Name: fmt.Sprintf("%s staff %d", name, i), RoleID: roleID, PinHash: "x", Phone: "1", Status: StaffStatusActive}
		require.NoError(t, db.Create(&s).Error)
		staff = append(staff, s)
	}
	return org, staff
}

// effectiveCodes is what a staff member's PIN token would carry after the
// migration: the codes of every grant on their (now per-org) role.
func effectiveCodes(t *testing.T, db *gorm.DB, staffID uint) []string {
	t.Helper()
	var s Staff
	require.NoError(t, db.First(&s, staffID).Error)
	var codes []string
	require.NoError(t, db.Table("org_role_permission").
		Select("permissions.code").
		Joins("JOIN permissions ON permissions.id = org_role_permission.permission_id").
		Where("org_role_permission.role_id = ?", s.RoleID).
		Order("permissions.code").
		Scan(&codes).Error)
	return codes
}

func legacyCodesOf(t *testing.T, db *gorm.DB, roleID uint) []string {
	t.Helper()
	var codes []string
	require.NoError(t, db.Table("role_permission").
		Select("permissions.code").
		Joins("JOIN permissions ON permissions.id = role_permission.permission_id").
		Where("role_permission.role_id = ?", roleID).
		Order("permissions.code").
		Scan(&codes).Error)
	return codes
}

func onlyLegacy(codes []string) []string {
	var out []string
	for _, c := range codes {
		if isLegacyPermission(c) {
			out = append(out, c)
		}
	}
	slices.Sort(out)
	return out
}

func withoutLegacy(codes []string) []string {
	var out []string
	for _, c := range codes {
		if !isLegacyPermission(c) {
			out = append(out, c)
		}
	}
	slices.Sort(out)
	return out
}

func sorted(codes []string) []string {
	out := slices.Clone(codes)
	slices.Sort(out)
	return out
}

// The migration's whole promise: nobody gains or loses anything they could
// do before. Every staff member's legacy permissions come out identical, and
// the new permissions follow the catalogue defaults for their role.
func TestMigrateAccessRights_EveryStaffKeepsExactlyTheirLegacyPermissions(t *testing.T) {
	db := newAccessTestDB(t)
	world := seedLegacyWorld(t, db)
	_, orgAStaff := seedOrgWithStaff(t, db, "Org A", world.roleIDs["manager"], world.roleIDs["super_staff"], world.roleIDs["staff"], world.roleIDs["staff"])
	_, orgBStaff := seedOrgWithStaff(t, db, "Org B", world.roleIDs["manager"], world.roleIDs["staff"])
	legacyRoleOf := map[uint]string{}
	for code, id := range world.roleIDs {
		legacyRoleOf[id] = code
	}
	before := map[uint][]string{}
	legacyCode := map[uint]string{}
	for _, s := range append(orgAStaff, orgBStaff...) {
		before[s.ID] = sorted(legacyCodesOf(t, db, s.RoleID))
		legacyCode[s.ID] = legacyRoleOf[s.RoleID]
	}

	require.NoError(t, MigrateAccessRights(db))

	for _, s := range append(orgAStaff, orgBStaff...) {
		after := effectiveCodes(t, db, s.ID)
		wantLegacy := before[s.ID]
		if legacyCode[s.ID] == "staff" {
			// The one deliberate change (Discount V1): a Cashier may now ASK for a
			// custom discount - it still needs someone's approval to get one.
			wantLegacy = sorted(append(slices.Clone(wantLegacy), PermApplyManualDiscount))
		}
		require.Equal(t, wantLegacy, onlyLegacy(after), "legacy permissions of staff %d (%s)", s.ID, legacyCode[s.ID])

		// and the new permissions are the catalogue defaults for that role
		def, ok := defaultRoleForLegacyCode(legacyCode[s.ID])
		require.True(t, ok)
		wantNew := withoutLegacy(def.Grants)
		require.Equal(t, wantNew, withoutLegacy(after), "new permissions of staff %d (%s)", s.ID, legacyCode[s.ID])
	}
}

func TestMigrateAccessRights_RemapsStaffToTheirOwnOrgsRoles(t *testing.T) {
	db := newAccessTestDB(t)
	world := seedLegacyWorld(t, db)
	orgA, staffA := seedOrgWithStaff(t, db, "Org A", world.roleIDs["manager"], world.roleIDs["staff"])
	orgB, staffB := seedOrgWithStaff(t, db, "Org B", world.roleIDs["manager"], world.roleIDs["staff"])

	require.NoError(t, MigrateAccessRights(db))

	roleOf := func(id uint) Role {
		var s Staff
		require.NoError(t, db.First(&s, id).Error)
		var r Role
		require.NoError(t, db.First(&r, s.RoleID).Error)
		return r
	}
	managerA, cashierA := roleOf(staffA[0].ID), roleOf(staffA[1].ID)
	managerB, cashierB := roleOf(staffB[0].ID), roleOf(staffB[1].ID)

	require.Equal(t, RoleCodeManager, managerA.Code)
	require.Equal(t, RoleCodeCashier, cashierA.Code, "legacy `staff` becomes `cashier`")
	require.NotNil(t, managerA.OrgID)
	require.Equal(t, orgA.ID, *managerA.OrgID)
	require.Equal(t, orgB.ID, *managerB.OrgID)
	require.NotEqual(t, managerA.ID, managerB.ID, "each org has its own role rows")
	require.NotEqual(t, cashierA.ID, cashierB.ID)
	require.True(t, managerA.IsSystem)
	require.True(t, cashierA.IsSystem)
	require.NotEqual(t, world.roleIDs["manager"], managerA.ID, "no staff still points at a global role")
}

// Super Staff only exists in orgs that were actually using it - everyone else
// gets just Manager and Cashier.
func TestMigrateAccessRights_SuperStaffRoleOnlyInOrgsThatUsedIt(t *testing.T) {
	db := newAccessTestDB(t)
	world := seedLegacyWorld(t, db)
	orgA, staffA := seedOrgWithStaff(t, db, "Org A", world.roleIDs["super_staff"])
	orgB, _ := seedOrgWithStaff(t, db, "Org B", world.roleIDs["manager"], world.roleIDs["staff"])

	require.NoError(t, MigrateAccessRights(db))

	var rolesA, rolesB []Role
	require.NoError(t, db.Where("org_id = ?", orgA.ID).Find(&rolesA).Error)
	require.NoError(t, db.Where("org_id = ?", orgB.ID).Find(&rolesB).Error)
	codesA, codesB := roleCodes(rolesA), roleCodes(rolesB)
	require.ElementsMatch(t, []string{RoleCodeManager, RoleCodeCashier, RoleCodeSuperStaff}, codesA)
	require.ElementsMatch(t, []string{RoleCodeManager, RoleCodeCashier}, codesB)

	var superStaff Role
	require.NoError(t, db.Where("org_id = ? AND code = ?", orgA.ID, RoleCodeSuperStaff).First(&superStaff).Error)
	require.Equal(t, "Super Staff", superStaff.Name)
	require.False(t, superStaff.IsSystem, "Super Staff is a custom role the Owner may delete")
	var staff Staff
	require.NoError(t, db.First(&staff, staffA[0].ID).Error)
	require.Equal(t, superStaff.ID, staff.RoleID)
}

func roleCodes(roles []Role) []string {
	out := make([]string, len(roles))
	for i, r := range roles {
		out[i] = r.Code
	}
	return out
}

func TestMigrateAccessRights_OrgWithoutStaffStillGetsManagerAndCashier(t *testing.T) {
	db := newAccessTestDB(t)
	seedLegacyWorld(t, db)
	org, _ := seedOrgWithStaff(t, db, "Empty Org")

	require.NoError(t, MigrateAccessRights(db))

	var roles []Role
	require.NoError(t, db.Where("org_id = ?", org.ID).Find(&roles).Error)
	require.ElementsMatch(t, []string{RoleCodeManager, RoleCodeCashier}, roleCodes(roles))
}

// A legacy role that isn't one of the three seeded ones (someone added it by
// hand) is cloned as a custom role rather than dropped, so its staff don't
// lose their permissions.
func TestMigrateAccessRights_UnknownLegacyRoleIsCloned(t *testing.T) {
	db := newAccessTestDB(t)
	world := seedLegacyWorld(t, db)
	var perm Permission
	require.NoError(t, db.Where("code = ?", "approve_void").First(&perm).Error)
	custom := Role{Code: "supervisor", Name: "Supervisor"}
	require.NoError(t, db.Create(&custom).Error)
	require.NoError(t, db.Create(&RolePermission{RoleID: custom.ID, PermissionID: perm.ID}).Error)
	org, staff := seedOrgWithStaff(t, db, "Org A", custom.ID, world.roleIDs["staff"])

	require.NoError(t, MigrateAccessRights(db))

	var clone Role
	require.NoError(t, db.Where("org_id = ? AND code = ?", org.ID, "supervisor").First(&clone).Error)
	require.Equal(t, "Supervisor", clone.Name)
	require.False(t, clone.IsSystem)
	require.Equal(t, []string{"approve_void"}, effectiveCodes(t, db, staff[0].ID))
}

func TestMigrateAccessRights_IsIdempotent(t *testing.T) {
	db := newAccessTestDB(t)
	world := seedLegacyWorld(t, db)
	_, staff := seedOrgWithStaff(t, db, "Org A", world.roleIDs["manager"], world.roleIDs["super_staff"], world.roleIDs["staff"])
	require.NoError(t, MigrateAccessRights(db))
	var rolesBefore, grantsBefore int64
	require.NoError(t, db.Model(&Role{}).Count(&rolesBefore).Error)
	require.NoError(t, db.Model(&OrgRolePermission{}).Count(&grantsBefore).Error)
	permsBefore := effectiveCodes(t, db, staff[0].ID)

	require.NoError(t, MigrateAccessRights(db))

	var rolesAfter, grantsAfter int64
	require.NoError(t, db.Model(&Role{}).Count(&rolesAfter).Error)
	require.NoError(t, db.Model(&OrgRolePermission{}).Count(&grantsAfter).Error)
	require.Equal(t, rolesBefore, rolesAfter)
	require.Equal(t, grantsBefore, grantsAfter)
	require.Equal(t, permsBefore, effectiveCodes(t, db, staff[0].ID))
}

// An Owner who has edited a role must not have it reset by a later restart.
func TestMigrateAccessRights_DoesNotOverwriteAnOwnersEdits(t *testing.T) {
	db := newAccessTestDB(t)
	world := seedLegacyWorld(t, db)
	org, staff := seedOrgWithStaff(t, db, "Org A", world.roleIDs["staff"])
	require.NoError(t, MigrateAccessRights(db))
	var s Staff
	require.NoError(t, db.First(&s, staff[0].ID).Error)
	require.NoError(t, db.Where("org_id = ? AND role_id = ?", org.ID, s.RoleID).Delete(&OrgRolePermission{}).Error)

	require.NoError(t, MigrateAccessRights(db))

	require.Empty(t, effectiveCodes(t, db, staff[0].ID))
}

func TestMigrateAccessRights_EnsuresTheWholeCatalogueAndRefreshesLegacyNames(t *testing.T) {
	db := newAccessTestDB(t)
	seedLegacyWorld(t, db)

	require.NoError(t, MigrateAccessRights(db))

	var perms []Permission
	require.NoError(t, db.Order("id").Find(&perms).Error)
	got := map[string]Permission{}
	for _, p := range perms {
		got[p.Code] = p
	}
	require.Len(t, perms, len(PermissionCatalogue))
	for _, def := range PermissionCatalogue {
		require.Equal(t, def.Name, got[def.Code].Name, def.Code)
		require.Equal(t, def.Category, got[def.Code].Category, def.Code)
	}
}

func TestEnsurePermissionCatalogue_IsIdempotent(t *testing.T) {
	db := newAccessTestDB(t)

	require.NoError(t, EnsurePermissionCatalogue(db))
	require.NoError(t, EnsurePermissionCatalogue(db))

	var n int64
	require.NoError(t, db.Model(&Permission{}).Count(&n).Error)
	require.Equal(t, int64(len(PermissionCatalogue)), n)
}

func TestMigrateAccessRights_OnAFreshDatabaseWithNoLegacyRolesJustSeedsPermissions(t *testing.T) {
	db := newAccessTestDB(t)
	org, _ := seedOrgWithStaff(t, db, "Fresh Org")

	require.NoError(t, MigrateAccessRights(db))

	var roles []Role
	require.NoError(t, db.Where("org_id = ?", org.ID).Find(&roles).Error)
	require.ElementsMatch(t, []string{RoleCodeManager, RoleCodeCashier}, roleCodes(roles))
	var manager Role
	require.NoError(t, db.Where("org_id = ? AND code = ?", org.ID, RoleCodeManager).First(&manager).Error)
	var codes []string
	require.NoError(t, db.Table("org_role_permission").Select("permissions.code").
		Joins("JOIN permissions ON permissions.id = org_role_permission.permission_id").
		Where("org_role_permission.role_id = ?", manager.ID).Scan(&codes).Error)
	require.ElementsMatch(t, DefaultRoles[0].Grants, codes)
}

// The catalogue defaults themselves: Manager is everything except
// manage_staff; Cashier is the till basics.
func TestDefaultRoles_ManagerHoldsEverythingButManageStaff_CashierHoldsTillBasics(t *testing.T) {
	manager, cashier := DefaultRoles[0], DefaultRoles[1]

	require.NotContains(t, manager.Grants, PermManageStaff)
	require.Len(t, manager.Grants, len(PermissionCatalogue)-1)
	require.Contains(t, manager.Grants, PermForceCloseShift)
	require.Contains(t, manager.Grants, PermViewItemCost)

	require.Contains(t, cashier.Grants, PermAccessPOSPortal)
	require.Contains(t, cashier.Grants, PermAcceptPayments)
	require.NotContains(t, cashier.Grants, PermAccessBackOffice)
	require.NotContains(t, cashier.Grants, PermApproveVoid)
	require.NotContains(t, cashier.Grants, PermForceCloseShift)
	// A cashier may ASK for a custom discount (Discount V1); getting one still
	// takes someone else's approval, which they don't hold.
	require.Contains(t, cashier.Grants, PermApplyManualDiscount)
	require.NotContains(t, cashier.Grants, PermApproveManualDiscount)
	require.NotContains(t, cashier.Grants, PermApproveOwnDiscount)
}

func TestPermissionCatalogue_CodesAreUniqueAndFitTheColumn(t *testing.T) {
	seen := map[string]bool{}
	for _, def := range PermissionCatalogue {
		require.False(t, seen[def.Code], "duplicate code %s", def.Code)
		seen[def.Code] = true
		require.LessOrEqual(t, len(def.Code), 50, def.Code)
		require.Contains(t, []string{CategoryPOS, CategoryBackOffice}, def.Category, def.Code)
	}
	for _, role := range append(slices.Clone(DefaultRoles), SuperStaffRole) {
		for _, g := range role.Grants {
			require.True(t, seen[g], "role %s grants %s which is not in the catalogue", role.Code, g)
		}
	}
}

// ---- Discount V1: existing Cashier roles may now ask for a custom discount ----

func cashierOf(t *testing.T, db *gorm.DB, orgID uint) Role {
	t.Helper()
	var role Role
	require.NoError(t, db.Where("org_id = ? AND code = ?", orgID, RoleCodeCashier).First(&role).Error)
	return role
}

func hasGrant(t *testing.T, db *gorm.DB, orgID, roleID uint, code string) bool {
	t.Helper()
	var n int64
	require.NoError(t, db.Table("org_role_permission").
		Joins("JOIN permissions ON permissions.id = org_role_permission.permission_id").
		Where("org_role_permission.org_id = ? AND org_role_permission.role_id = ? AND permissions.code = ?", orgID, roleID, code).
		Count(&n).Error)
	return n > 0
}

// An org whose roles already exist (made before Discount V1) has a Cashier
// without the permission; the next start grants it.
func TestMigrateAccessRights_GrantsApplyManualDiscountToAnExistingCashier(t *testing.T) {
	db := newAccessTestDB(t)
	require.NoError(t, EnsurePermissionCatalogue(db))
	org := Organization{Name: "Old Org", Timezone: "Asia/Yangon"}
	require.NoError(t, db.Create(&org).Error)
	require.NoError(t, ProvisionDefaultRoles(db, org.ID))
	cashier := cashierOf(t, db, org.ID)
	// roll the org back to how Cashier looked before Discount V1
	require.NoError(t, db.Exec(`DELETE FROM org_role_permission WHERE role_id = ? AND permission_id = (SELECT id FROM permissions WHERE code = ?)`,
		cashier.ID, PermApplyManualDiscount).Error)
	require.NoError(t, db.Exec(`DELETE FROM data_migrations`).Error)
	require.False(t, hasGrant(t, db, org.ID, cashier.ID, PermApplyManualDiscount))

	require.NoError(t, MigrateAccessRights(db))

	require.True(t, hasGrant(t, db, org.ID, cashier.ID, PermApplyManualDiscount))
}

// The grant is a one-off, not a rule: an Owner who takes it off Cashier (so
// cashiers can't even ask) must not have it handed back on the next restart.
func TestMigrateAccessRights_DoesNotRegrantApplyManualDiscountAfterAnOwnerRemovedIt(t *testing.T) {
	db := newAccessTestDB(t)
	org := Organization{Name: "Org", Timezone: "Asia/Yangon"}
	require.NoError(t, db.Create(&org).Error)
	require.NoError(t, MigrateAccessRights(db)) // grants it, records that it did
	cashier := cashierOf(t, db, org.ID)
	require.True(t, hasGrant(t, db, org.ID, cashier.ID, PermApplyManualDiscount))
	require.NoError(t, db.Exec(`DELETE FROM org_role_permission WHERE role_id = ? AND permission_id = (SELECT id FROM permissions WHERE code = ?)`,
		cashier.ID, PermApplyManualDiscount).Error)

	require.NoError(t, MigrateAccessRights(db))
	require.NoError(t, MigrateAccessRights(db))

	require.False(t, hasGrant(t, db, org.ID, cashier.ID, PermApplyManualDiscount))
}

// Only the built-in Cashier gets it. A custom role the Owner built keeps
// exactly what they gave it.
func TestMigrateAccessRights_ApplyManualDiscountGrantLeavesCustomRolesAlone(t *testing.T) {
	db := newAccessTestDB(t)
	require.NoError(t, EnsurePermissionCatalogue(db))
	org := Organization{Name: "Org", Timezone: "Asia/Yangon"}
	require.NoError(t, db.Create(&org).Error)
	require.NoError(t, ProvisionDefaultRoles(db, org.ID))
	permIDs, err := permissionIDsByCode(db)
	require.NoError(t, err)
	custom, err := createOrgRole(db, org.ID, "stock_clerk", "Stock Clerk", false, []string{PermAccessPOSPortal}, permIDs)
	require.NoError(t, err)
	require.NoError(t, db.Exec(`DELETE FROM data_migrations`).Error)

	require.NoError(t, MigrateAccessRights(db))

	require.False(t, hasGrant(t, db, org.ID, custom.ID, PermApplyManualDiscount))
}

func TestMigrateAccessRights_ApplyManualDiscountGrantDoesNotDuplicateWhereCashierAlreadyHasIt(t *testing.T) {
	db := newAccessTestDB(t)
	org := Organization{Name: "Org", Timezone: "Asia/Yangon"}
	require.NoError(t, db.Create(&org).Error)
	require.NoError(t, MigrateAccessRights(db))
	cashier := cashierOf(t, db, org.ID)
	require.NoError(t, db.Exec(`DELETE FROM data_migrations`).Error) // pretend the marker was lost

	require.NoError(t, MigrateAccessRights(db))

	var n int64
	require.NoError(t, db.Table("org_role_permission").
		Where("role_id = ? AND permission_id = (SELECT id FROM permissions WHERE code = ?)", cashier.ID, PermApplyManualDiscount).
		Count(&n).Error)
	require.Equal(t, int64(1), n)
}

func TestApplyManualDiscountPermission_IsNamedForAskingForADiscount(t *testing.T) {
	for _, def := range PermissionCatalogue {
		if def.Code == PermApplyManualDiscount {
			require.Equal(t, "Ask for a custom discount (needs approval)", def.Name)
			return
		}
	}
	t.Fatal("apply_manual_discount missing from the catalogue")
}

// The one-time grant has already been recorded; a legacy org that only shows up
// afterwards (db/demo_seed.sql loaded later, say) is migrated with a Cashier
// that must already be able to ask for discounts - the one-off won't run again.
func TestMigrateAccessRights_LegacyOrgMigratedAfterTheOneTimeGrant_StillGetsACashierThatCanAsk(t *testing.T) {
	db := newAccessTestDB(t)
	world := seedLegacyWorld(t, db)
	require.NoError(t, MigrateAccessRights(db)) // no org yet: records the one-time grant and does nothing else
	org, staff := seedOrgWithStaff(t, db, "Late Org", world.roleIDs["staff"])

	require.NoError(t, MigrateAccessRights(db))

	cashier := cashierOf(t, db, org.ID)
	require.True(t, hasGrant(t, db, org.ID, cashier.ID, PermApplyManualDiscount))
	require.Contains(t, effectiveCodes(t, db, staff[0].ID), PermApplyManualDiscount)
	require.NotContains(t, effectiveCodes(t, db, staff[0].ID), PermApproveManualDiscount, "asking is not approving")
}

// ---- manage_branch_prices ----

func managerOf(t *testing.T, db *gorm.DB, orgID uint) Role {
	t.Helper()
	var role Role
	require.NoError(t, db.Where("org_id = ? AND code = ?", orgID, RoleCodeManager).First(&role).Error)
	return role
}

func removeGrant(t *testing.T, db *gorm.DB, roleID uint, code string) {
	t.Helper()
	require.NoError(t, db.Exec(`DELETE FROM org_role_permission WHERE role_id = ? AND permission_id = (SELECT id FROM permissions WHERE code = ?)`,
		roleID, code).Error)
}

func TestManageBranchPricesPermission_IsInTheCatalogueUnderBackOffice(t *testing.T) {
	for _, def := range PermissionCatalogue {
		if def.Code == PermManageBranchPrices {
			require.Equal(t, "manage_branch_prices", def.Code)
			require.Equal(t, "Set their branch's prices", def.Name)
			require.Equal(t, CategoryBackOffice, def.Category)
			return
		}
	}
	t.Fatal("manage_branch_prices missing from the catalogue")
}

// A new org's Manager holds it; Cashier does not.
func TestDefaultRoles_ManagerHoldsManageBranchPrices_CashierDoesNot(t *testing.T) {
	require.Contains(t, DefaultRoles[0].Grants, PermManageBranchPrices)
	require.NotContains(t, DefaultRoles[1].Grants, PermManageBranchPrices)
	require.NotContains(t, SuperStaffRole.Grants, PermManageBranchPrices)
}

// An org whose roles exist already (made before branch prices) has a Manager
// without the permission; the next start grants it - to the Manager only.
func TestMigrateAccessRights_GrantsManageBranchPricesToExistingManagersOnly(t *testing.T) {
	db := newAccessTestDB(t)
	require.NoError(t, EnsurePermissionCatalogue(db))
	var orgs []Organization
	for _, name := range []string{"Old Org A", "Old Org B"} {
		org := Organization{Name: name, Timezone: "Asia/Yangon"}
		require.NoError(t, db.Create(&org).Error)
		require.NoError(t, ProvisionDefaultRoles(db, org.ID))
		orgs = append(orgs, org)
	}
	permIDs, err := permissionIDsByCode(db)
	require.NoError(t, err)
	custom, err := createOrgRole(db, orgs[0].ID, "stock_clerk", "Stock Clerk", false, []string{PermAccessPOSPortal, PermAccessBackOffice}, permIDs)
	require.NoError(t, err)
	// roll both orgs back to how Manager looked before this change
	for _, org := range orgs {
		removeGrant(t, db, managerOf(t, db, org.ID).ID, PermManageBranchPrices)
	}
	require.NoError(t, db.Exec(`DELETE FROM data_migrations`).Error)
	before := map[uint][]string{}
	for _, org := range orgs {
		for _, r := range []Role{cashierOf(t, db, org.ID)} {
			before[r.ID] = grantedCodes(t, db, r.ID)
		}
	}
	before[custom.ID] = grantedCodes(t, db, custom.ID)

	require.NoError(t, MigrateAccessRights(db))

	for _, org := range orgs {
		require.True(t, hasGrant(t, db, org.ID, managerOf(t, db, org.ID).ID, PermManageBranchPrices), "every org's Manager")
		require.False(t, hasGrant(t, db, org.ID, cashierOf(t, db, org.ID).ID, PermManageBranchPrices))
	}
	for roleID, codes := range before {
		require.Equal(t, codes, grantedCodes(t, db, roleID), "other roles are untouched")
	}
}

func grantedCodes(t *testing.T, db *gorm.DB, roleID uint) []string {
	t.Helper()
	var codes []string
	require.NoError(t, db.Table("org_role_permission").
		Joins("JOIN permissions ON permissions.id = org_role_permission.permission_id").
		Where("org_role_permission.role_id = ?", roleID).Order("permissions.code").Pluck("permissions.code", &codes).Error)
	return codes
}

// Re-running is a no-op: nothing is duplicated, and an Owner who switched it off
// for Manager does not get it handed back on the next restart.
func TestMigrateAccessRights_ManageBranchPricesGrantRunsOnce(t *testing.T) {
	db := newAccessTestDB(t)
	org := Organization{Name: "Org", Timezone: "Asia/Yangon"}
	require.NoError(t, db.Create(&org).Error)
	require.NoError(t, MigrateAccessRights(db))
	manager := managerOf(t, db, org.ID)
	require.True(t, hasGrant(t, db, org.ID, manager.ID, PermManageBranchPrices))

	require.NoError(t, MigrateAccessRights(db))
	var n int64
	require.NoError(t, db.Table("org_role_permission").
		Where("role_id = ? AND permission_id = (SELECT id FROM permissions WHERE code = ?)", manager.ID, PermManageBranchPrices).
		Count(&n).Error)
	require.Equal(t, int64(1), n, "re-running changes nothing")

	removeGrant(t, db, manager.ID, PermManageBranchPrices) // the Owner turns it off
	require.NoError(t, MigrateAccessRights(db))
	require.NoError(t, MigrateAccessRights(db))
	require.False(t, hasGrant(t, db, org.ID, manager.ID, PermManageBranchPrices))
}

// Even with the marker lost, a Manager that already has it isn't granted twice.
func TestMigrateAccessRights_ManageBranchPricesGrantDoesNotDuplicate(t *testing.T) {
	db := newAccessTestDB(t)
	org := Organization{Name: "Org", Timezone: "Asia/Yangon"}
	require.NoError(t, db.Create(&org).Error)
	require.NoError(t, MigrateAccessRights(db))
	manager := managerOf(t, db, org.ID)
	require.NoError(t, db.Exec(`DELETE FROM data_migrations`).Error)

	require.NoError(t, MigrateAccessRights(db))

	var n int64
	require.NoError(t, db.Table("org_role_permission").
		Where("role_id = ? AND permission_id = (SELECT id FROM permissions WHERE code = ?)", manager.ID, PermManageBranchPrices).
		Count(&n).Error)
	require.Equal(t, int64(1), n)
}

// A legacy org that is migrated only after the one-time grant already ran gets
// a Manager that has the permission from its defaults.
func TestMigrateAccessRights_LegacyOrgMigratedLater_ManagerHasManageBranchPrices(t *testing.T) {
	db := newAccessTestDB(t)
	world := seedLegacyWorld(t, db)
	require.NoError(t, MigrateAccessRights(db))
	org, _ := seedOrgWithStaff(t, db, "Late Org", world.roleIDs["manager"])

	require.NoError(t, MigrateAccessRights(db))

	require.True(t, hasGrant(t, db, org.ID, managerOf(t, db, org.ID).ID, PermManageBranchPrices))
	require.False(t, hasGrant(t, db, org.ID, cashierOf(t, db, org.ID).ID, PermManageBranchPrices))
}

// ---- view_shift_history ----

func TestViewShiftHistoryPermission_IsInTheCatalogueUnderBackOffice(t *testing.T) {
	for _, def := range PermissionCatalogue {
		if def.Code == PermViewShiftHistory {
			require.Equal(t, "view_shift_history", def.Code)
			require.Equal(t, "Shift history and cash differences", def.Name)
			require.Equal(t, CategoryBackOffice, def.Category)
			return
		}
	}
	t.Fatal("view_shift_history missing from the catalogue")
}

// A new org's Manager holds it; Cashier and Super Staff do not.
func TestDefaultRoles_ManagerHoldsViewShiftHistory_OthersDoNot(t *testing.T) {
	require.Contains(t, DefaultRoles[0].Grants, PermViewShiftHistory)
	require.NotContains(t, DefaultRoles[1].Grants, PermViewShiftHistory)
	require.NotContains(t, SuperStaffRole.Grants, PermViewShiftHistory)
}

// An org whose roles exist already has a Manager without it; the next start
// grants it - to the system Manager ONLY. Cashier and a custom role the Owner
// built (even one called "Manager"-like) are untouched.
func TestMigrateAccessRights_GrantsViewShiftHistoryToExistingSystemManagersOnly(t *testing.T) {
	db := newAccessTestDB(t)
	require.NoError(t, EnsurePermissionCatalogue(db))
	var orgs []Organization
	for _, name := range []string{"Old Org A", "Old Org B"} {
		org := Organization{Name: name, Timezone: "Asia/Yangon"}
		require.NoError(t, db.Create(&org).Error)
		require.NoError(t, ProvisionDefaultRoles(db, org.ID))
		orgs = append(orgs, org)
	}
	permIDs, err := permissionIDsByCode(db)
	require.NoError(t, err)
	custom, err := createOrgRole(db, orgs[0].ID, "shift_lead", "Shift Lead", false, []string{PermAccessPOSPortal, PermAccessBackOffice}, permIDs)
	require.NoError(t, err)
	for _, org := range orgs {
		removeGrant(t, db, managerOf(t, db, org.ID).ID, PermViewShiftHistory)
	}
	require.NoError(t, db.Exec(`DELETE FROM data_migrations`).Error)
	before := map[uint][]string{custom.ID: grantedCodes(t, db, custom.ID)}
	for _, org := range orgs {
		c := cashierOf(t, db, org.ID)
		before[c.ID] = grantedCodes(t, db, c.ID)
	}

	require.NoError(t, MigrateAccessRights(db))

	for _, org := range orgs {
		require.True(t, hasGrant(t, db, org.ID, managerOf(t, db, org.ID).ID, PermViewShiftHistory), "every org's system Manager")
		require.False(t, hasGrant(t, db, org.ID, cashierOf(t, db, org.ID).ID, PermViewShiftHistory))
	}
	require.False(t, hasGrant(t, db, orgs[0].ID, custom.ID, PermViewShiftHistory), "a custom role is left as the Owner set it")
	for roleID, codes := range before {
		require.Equal(t, codes, grantedCodes(t, db, roleID), "other roles are untouched")
	}
}

// Once: re-running changes nothing, and an Owner who switched it off for Manager
// doesn't get it handed back on the next restart.
func TestMigrateAccessRights_ViewShiftHistoryGrantRunsOnce(t *testing.T) {
	db := newAccessTestDB(t)
	org := Organization{Name: "Org", Timezone: "Asia/Yangon"}
	require.NoError(t, db.Create(&org).Error)
	require.NoError(t, MigrateAccessRights(db))
	manager := managerOf(t, db, org.ID)
	require.True(t, hasGrant(t, db, org.ID, manager.ID, PermViewShiftHistory))

	require.NoError(t, MigrateAccessRights(db))
	var n int64
	require.NoError(t, db.Table("org_role_permission").
		Where("role_id = ? AND permission_id = (SELECT id FROM permissions WHERE code = ?)", manager.ID, PermViewShiftHistory).
		Count(&n).Error)
	require.Equal(t, int64(1), n, "re-running changes nothing")

	removeGrant(t, db, manager.ID, PermViewShiftHistory) // the Owner turns it off
	require.NoError(t, MigrateAccessRights(db))
	require.NoError(t, MigrateAccessRights(db))
	require.False(t, hasGrant(t, db, org.ID, manager.ID, PermViewShiftHistory))
}

// Even with the marker lost, a Manager that already has it isn't granted twice.
func TestMigrateAccessRights_ViewShiftHistoryGrantDoesNotDuplicate(t *testing.T) {
	db := newAccessTestDB(t)
	org := Organization{Name: "Org", Timezone: "Asia/Yangon"}
	require.NoError(t, db.Create(&org).Error)
	require.NoError(t, MigrateAccessRights(db))
	manager := managerOf(t, db, org.ID)
	require.NoError(t, db.Exec(`DELETE FROM data_migrations`).Error)

	require.NoError(t, MigrateAccessRights(db))

	var n int64
	require.NoError(t, db.Table("org_role_permission").
		Where("role_id = ? AND permission_id = (SELECT id FROM permissions WHERE code = ?)", manager.ID, PermViewShiftHistory).
		Count(&n).Error)
	require.Equal(t, int64(1), n)
}
