//go:build cgo

package identity

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"shagan_pos/internal/common"
)

// provisioned returns a database with two orgs that each got the default
// Manager and Cashier roles, plus one branch and one manager/cashier staff
// member in each.
type provisionedOrgs struct {
	db                           *gorm.DB
	orgA, orgB                   Organization
	branchA, branchB             Branch
	managerA, cashierA           Role
	managerB, cashierB           Role
	staffManagerA, staffCashierA Staff
}

func newProvisionedOrgs(t *testing.T) provisionedOrgs {
	t.Helper()
	db := newAccessTestDB(t)
	require.NoError(t, EnsurePermissionCatalogue(db))
	p := provisionedOrgs{db: db}
	mk := func(name string) (Organization, Branch, Role, Role) {
		org := Organization{Name: name, Timezone: "Asia/Yangon"}
		require.NoError(t, db.Create(&org).Error)
		branch := Branch{OrgID: org.ID, Name: name + " main", Status: BranchStatusActive}
		require.NoError(t, db.Create(&branch).Error)
		require.NoError(t, NewRepository(db).ProvisionDefaultRoles(db, org.ID))
		var manager, cashier Role
		require.NoError(t, db.Where("org_id = ? AND code = ?", org.ID, RoleCodeManager).First(&manager).Error)
		require.NoError(t, db.Where("org_id = ? AND code = ?", org.ID, RoleCodeCashier).First(&cashier).Error)
		return org, branch, manager, cashier
	}
	p.orgA, p.branchA, p.managerA, p.cashierA = mk("Org A")
	p.orgB, p.branchB, p.managerB, p.cashierB = mk("Org B")
	p.staffManagerA = Staff{BranchID: p.branchA.ID, Name: "Mgr A", RoleID: p.managerA.ID, PinHash: "x", Phone: "1", Status: StaffStatusActive}
	p.staffCashierA = Staff{BranchID: p.branchA.ID, Name: "Cash A", RoleID: p.cashierA.ID, PinHash: "x", Phone: "1", Status: StaffStatusActive}
	require.NoError(t, db.Create(&p.staffManagerA).Error)
	require.NoError(t, db.Create(&p.staffCashierA).Error)
	return p
}

func permissionIDs(t *testing.T, db *gorm.DB, codes ...string) []uint {
	t.Helper()
	ids, err := permissionIDsByCode(db)
	require.NoError(t, err)
	out := make([]uint, len(codes))
	for i, c := range codes {
		out[i] = ids[c]
		require.NotZero(t, out[i], c)
	}
	return out
}

func codesOf(perms []Permission) []string {
	out := make([]string, len(perms))
	for i, p := range perms {
		out[i] = p.Code
	}
	return out
}

func TestRepository_ProvisionDefaultRoles_GivesManagerAndCashierTheirDefaults(t *testing.T) {
	p := newProvisionedOrgs(t)
	repo := NewRepository(p.db)

	managerPerms, err := repo.ListRolePermissions(context.Background(), p.orgA.ID, p.managerA.ID)
	require.NoError(t, err)
	cashierPerms, err := repo.ListRolePermissions(context.Background(), p.orgA.ID, p.cashierA.ID)
	require.NoError(t, err)

	require.ElementsMatch(t, DefaultRoles[0].Grants, codesOf(managerPerms))
	require.ElementsMatch(t, DefaultRoles[1].Grants, codesOf(cashierPerms))
	require.True(t, p.managerA.IsSystem)
	require.True(t, p.cashierA.IsSystem)
}

// The first org on a fresh database, before any seed has run: provisioning
// must still work rather than fail on missing permission rows.
func TestRepository_ProvisionDefaultRoles_WorksBeforeThePermissionCatalogueIsSeeded(t *testing.T) {
	db := newAccessTestDB(t)
	org := Organization{Name: "First", Timezone: "Asia/Yangon"}
	require.NoError(t, db.Create(&org).Error)

	require.NoError(t, NewRepository(db).ProvisionDefaultRoles(db, org.ID))

	var n int64
	require.NoError(t, db.Model(&Role{}).Where("org_id = ?", org.ID).Count(&n).Error)
	require.Equal(t, int64(2), n)
}

func TestRepository_ListRoles_ReturnsOnlyTheOrgsOwnRolesSystemFirst(t *testing.T) {
	p := newProvisionedOrgs(t)
	repo := NewRepository(p.db)
	custom := Role{OrgID: &p.orgA.ID, Code: "alpha", Name: "Alpha"}
	require.NoError(t, repo.CreateRole(p.db, &custom))
	// a legacy global template that must never show up
	require.NoError(t, p.db.Create(&Role{Code: "staff", Name: "Staff"}).Error)

	got, err := repo.ListRoles(context.Background(), p.orgA.ID)

	require.NoError(t, err)
	require.Equal(t, []string{RoleCodeCashier, RoleCodeManager, "alpha"}, roleCodes(got), "system roles first (by name), then custom")
	for _, r := range got {
		require.NotNil(t, r.OrgID)
		require.Equal(t, p.orgA.ID, *r.OrgID)
	}
}

func TestRepository_GetRole_OtherOrgsRoleIsNotFound(t *testing.T) {
	p := newProvisionedOrgs(t)

	_, err := NewRepository(p.db).GetRole(context.Background(), p.orgA.ID, p.managerB.ID)

	var restErr common.RestError
	require.ErrorAs(t, err, &restErr)
	require.Equal(t, http.StatusNotFound, restErr.Status)
}

func TestRepository_GetRole_ReturnsTheOrgsRole(t *testing.T) {
	p := newProvisionedOrgs(t)

	got, err := NewRepository(p.db).GetRole(context.Background(), p.orgA.ID, p.managerA.ID)

	require.NoError(t, err)
	require.Equal(t, p.managerA.ID, got.ID)
}

// Two orgs edit "their Manager" independently: a grant change in one must not
// touch the other.
func TestRepository_ReplaceRoleGrants_TwoOrgsDivergeIndependently(t *testing.T) {
	p := newProvisionedOrgs(t)
	repo := NewRepository(p.db)

	require.NoError(t, repo.ReplaceRoleGrants(p.db, p.orgA.ID, p.managerA.ID, permissionIDs(t, p.db, PermAccessPOSPortal, PermApproveVoid)))

	a, err := repo.ListRolePermissions(context.Background(), p.orgA.ID, p.managerA.ID)
	require.NoError(t, err)
	b, err := repo.ListRolePermissions(context.Background(), p.orgB.ID, p.managerB.ID)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{PermAccessPOSPortal, PermApproveVoid}, codesOf(a))
	require.ElementsMatch(t, DefaultRoles[0].Grants, codesOf(b))
}

func TestRepository_ReplaceRoleGrants_ToNothingClearsEveryGrant(t *testing.T) {
	p := newProvisionedOrgs(t)
	repo := NewRepository(p.db)

	require.NoError(t, repo.ReplaceRoleGrants(p.db, p.orgA.ID, p.cashierA.ID, nil))

	got, err := repo.ListRolePermissions(context.Background(), p.orgA.ID, p.cashierA.ID)
	require.NoError(t, err)
	require.Empty(t, got)
}

func TestRepository_ListRolePermissions_AnotherOrgsRoleYieldsNothing(t *testing.T) {
	p := newProvisionedOrgs(t)

	got, err := NewRepository(p.db).ListRolePermissions(context.Background(), p.orgB.ID, p.managerA.ID)

	require.NoError(t, err)
	require.Empty(t, got)
}

func TestRepository_ListRoleGrantCodes_AndCountStaffByRole(t *testing.T) {
	p := newProvisionedOrgs(t)
	repo := NewRepository(p.db)

	grants, err := repo.ListRoleGrantCodes(context.Background(), p.orgA.ID)
	require.NoError(t, err)
	counts, err := repo.CountStaffByRole(context.Background(), p.orgA.ID)
	require.NoError(t, err)

	require.ElementsMatch(t, DefaultRoles[1].Grants, grants[p.cashierA.ID])
	require.ElementsMatch(t, DefaultRoles[0].Grants, grants[p.managerA.ID])
	require.NotContains(t, grants, p.managerB.ID, "another org's roles never appear")
	require.Equal(t, map[uint]int{p.managerA.ID: 1, p.cashierA.ID: 1}, counts)
}

func TestRepository_UpdateRoleName_ChangesOnlyTheName(t *testing.T) {
	p := newProvisionedOrgs(t)
	repo := NewRepository(p.db)

	require.NoError(t, repo.UpdateRoleName(p.db, p.orgA.ID, p.cashierA.ID, "Till Operator"))

	got, err := repo.GetRole(context.Background(), p.orgA.ID, p.cashierA.ID)
	require.NoError(t, err)
	require.Equal(t, "Till Operator", got.Name)
	require.Equal(t, RoleCodeCashier, got.Code, "the code is stable across renames")
	other, err := repo.GetRole(context.Background(), p.orgB.ID, p.cashierB.ID)
	require.NoError(t, err)
	require.Equal(t, "Cashier", other.Name)
}

func TestRepository_UpdateRoleName_OtherOrgsRoleIsNotFound(t *testing.T) {
	p := newProvisionedOrgs(t)

	err := NewRepository(p.db).UpdateRoleName(p.db, p.orgA.ID, p.managerB.ID, "Hijacked")

	var restErr common.RestError
	require.ErrorAs(t, err, &restErr)
	require.Equal(t, http.StatusNotFound, restErr.Status)
}

func TestRepository_DeleteRole_RemovesTheRoleAndItsGrantsOnly(t *testing.T) {
	p := newProvisionedOrgs(t)
	repo := NewRepository(p.db)
	custom := Role{OrgID: &p.orgA.ID, Code: "temp", Name: "Temp"}
	require.NoError(t, repo.CreateRole(p.db, &custom))
	require.NoError(t, repo.ReplaceRoleGrants(p.db, p.orgA.ID, custom.ID, permissionIDs(t, p.db, PermAccessPOSPortal)))

	require.NoError(t, repo.DeleteRole(p.db, p.orgA.ID, custom.ID))

	_, err := repo.GetRole(context.Background(), p.orgA.ID, custom.ID)
	require.Error(t, err)
	var grants int64
	require.NoError(t, p.db.Model(&OrgRolePermission{}).Where("role_id = ?", custom.ID).Count(&grants).Error)
	require.Zero(t, grants)
	managerPerms, err := repo.ListRolePermissions(context.Background(), p.orgA.ID, p.managerA.ID)
	require.NoError(t, err)
	require.NotEmpty(t, managerPerms, "other roles keep their grants")
}

func TestRepository_DeleteRole_OtherOrgsRoleIsNotFoundAndSurvives(t *testing.T) {
	p := newProvisionedOrgs(t)

	err := NewRepository(p.db).DeleteRole(p.db, p.orgA.ID, p.managerB.ID)

	var restErr common.RestError
	require.ErrorAs(t, err, &restErr)
	require.Equal(t, http.StatusNotFound, restErr.Status)
	var n int64
	require.NoError(t, p.db.Model(&Role{}).Where("id = ?", p.managerB.ID).Count(&n).Error)
	require.Equal(t, int64(1), n)
}

// "manager" isn't one role: whichever of the org's roles grants the
// permission is who the till offers as an approver.
func TestRepository_ListBranchManagers_UsesTheOrgsOwnRoleGrants(t *testing.T) {
	p := newProvisionedOrgs(t)
	repo := NewRepository(p.db)
	// Org A's cashier role is given approve_void; org B's identical role is not.
	require.NoError(t, repo.ReplaceRoleGrants(p.db, p.orgA.ID, p.cashierA.ID, permissionIDs(t, p.db, PermAccessPOSPortal, PermApproveVoid)))
	staffB := Staff{BranchID: p.branchB.ID, Name: "Cash B", RoleID: p.cashierB.ID, PinHash: "x", Phone: "1", Status: StaffStatusActive}
	require.NoError(t, p.db.Create(&staffB).Error)

	gotA, err := repo.ListBranchManagers(context.Background(), p.orgA.ID, p.branchA.ID, PermApproveVoid)
	require.NoError(t, err)
	gotB, err := repo.ListBranchManagers(context.Background(), p.orgB.ID, p.branchB.ID, PermApproveVoid)
	require.NoError(t, err)

	require.ElementsMatch(t, []uint{p.staffManagerA.ID, p.staffCashierA.ID}, staffIDs(gotA))
	require.Empty(t, gotB)
}

func TestRepository_ListBranchManagers_UnknownPermissionYieldsNobody(t *testing.T) {
	p := newProvisionedOrgs(t)

	got, err := NewRepository(p.db).ListBranchManagers(context.Background(), p.orgA.ID, p.branchA.ID, "does_not_exist")

	require.NoError(t, err)
	require.Empty(t, got)
}

func staffIDs(staff []Staff) []uint {
	out := make([]uint, len(staff))
	for i, s := range staff {
		out[i] = s.ID
	}
	return out
}
