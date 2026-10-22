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
		require.Equal(t, before[s.ID], onlyLegacy(after), "legacy permissions of staff %d (%s)", s.ID, legacyCode[s.ID])

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
	// see the note in identity_access.go: cashiers still need approval today
	require.NotContains(t, cashier.Grants, PermApplyManualDiscount)
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
