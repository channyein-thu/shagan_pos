package identity

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"shagan_pos/internal/audit"
	"shagan_pos/internal/common"
)

const testOrg = uint(7)
const testActor = uint(99)

func newRolesService(t *testing.T) (*Service, *MockRepository, *MockAuditWriter) {
	repo := NewMockRepository(t)
	audW := NewMockAuditWriter(t)
	return newTestService(repo, audW), repo, audW
}

// catalogueRows is the permissions table as the seed leaves it: every
// catalogue code with an id equal to its position + 1.
func catalogueRows() []Permission {
	rows := make([]Permission, len(PermissionCatalogue))
	for i, def := range PermissionCatalogue {
		rows[i] = Permission{ID: uint(i + 1), Code: def.Code, Name: def.Name, Category: def.Category}
	}
	return rows
}

func idOf(code string) uint {
	for i, def := range PermissionCatalogue {
		if def.Code == code {
			return uint(i + 1)
		}
	}
	panic("unknown code " + code)
}

func requireRoleErr(t *testing.T, err error, status int) {
	t.Helper()
	require.Error(t, err)
	requireRestErrorStatus(t, err, status)
}

func auditFor(action string, entityID string) interface{} {
	return mock.MatchedBy(func(e *audit.AuditLog) bool {
		return e.Entity == "role" && e.Action == action && e.EntityID == entityID &&
			e.OrgID == testOrg && e.ActorUserID != nil && *e.ActorUserID == testActor && e.ActorID == nil
	})
}

// ---- GET /roles, /permissions, /roles/:id/permissions ----

func TestService_ListRoles_PrependsAFixedOwnerRowAndSummarisesEachRole(t *testing.T) {
	svc, repo, _ := newRolesService(t)
	repo.EXPECT().ListRoles(mock.Anything, testOrg).Return([]Role{
		{ID: 10, Code: "manager", Name: "Manager", IsSystem: true},
		{ID: 11, Code: "cashier", Name: "Cashier", IsSystem: true},
		{ID: 12, Code: "stock_clerk", Name: "Stock clerk"},
	}, nil).Once()
	repo.EXPECT().ListRoleGrantCodes(mock.Anything, testOrg).Return(map[uint][]string{
		10: {PermAccessPOSPortal, PermAccessBackOffice},
		11: {PermAccessPOSPortal},
		12: {PermAccessBackOffice},
	}, nil).Once()
	repo.EXPECT().CountStaffByRole(mock.Anything, testOrg).Return(map[uint]int{10: 2, 11: 5}, nil).Once()

	got, err := svc.ListRoles(context.Background(), testOrg)

	require.NoError(t, err)
	require.Len(t, got, 4)
	require.Equal(t, RoleSummary{ID: 0, Code: "owner", Name: "Owner", IsSystem: true, Editable: false, POS: true, Backoffice: true}, got[0])
	require.Equal(t, RoleSummary{ID: 10, Code: "manager", Name: "Manager", IsSystem: true, Editable: true, StaffCount: 2, POS: true, Backoffice: true}, got[1])
	require.Equal(t, RoleSummary{ID: 11, Code: "cashier", Name: "Cashier", IsSystem: true, Editable: true, StaffCount: 5, POS: true, Backoffice: false}, got[2])
	require.Equal(t, RoleSummary{ID: 12, Code: "stock_clerk", Name: "Stock clerk", Editable: true, StaffCount: 0, POS: false, Backoffice: true}, got[3])
}

func TestService_ListRoles_PropagatesRepositoryErrors(t *testing.T) {
	svc, repo, _ := newRolesService(t)
	repo.EXPECT().ListRoles(mock.Anything, testOrg).Return(nil, common.SystemError("db read failed")).Once()

	_, err := svc.ListRoles(context.Background(), testOrg)

	requireRoleErr(t, err, http.StatusInternalServerError)
}

func TestService_ListPermissions_GroupsByCategoryInCatalogueOrder(t *testing.T) {
	svc, repo, _ := newRolesService(t)
	rows := catalogueRows()
	// the table returns rows in whatever order, and may hold codes the catalogue doesn't know
	rows[0], rows[len(rows)-1] = rows[len(rows)-1], rows[0]
	rows = append(rows, Permission{ID: 999, Code: "legacy_unknown", Name: "x", Category: "sales"})
	repo.EXPECT().ListPermissions(mock.Anything).Return(rows, nil).Once()

	got, err := svc.ListPermissions(context.Background())

	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Equal(t, CategoryPOS, got[0].Category)
	require.Equal(t, "POS", got[0].Label)
	require.Equal(t, CategoryBackOffice, got[1].Category)
	require.Equal(t, "Back office", got[1].Label)
	require.Equal(t, PermAccessPOSPortal, got[0].Permissions[0].Code, "catalogue order, not table order")
	require.Equal(t, PermOpenDrawerNoSale, got[0].Permissions[len(got[0].Permissions)-1].Code)
	require.Equal(t, PermAccessBackOffice, got[1].Permissions[0].Code)
	require.Equal(t, PermResolveSyncConflicts, got[1].Permissions[len(got[1].Permissions)-1].Code)
	total := len(got[0].Permissions) + len(got[1].Permissions)
	require.Equal(t, len(PermissionCatalogue), total, "codes outside the catalogue are not offered")
}

func TestService_ListRolePermissions_ChecksTheRoleBelongsToTheOrgFirst(t *testing.T) {
	svc, repo, _ := newRolesService(t)
	repo.EXPECT().GetRole(mock.Anything, testOrg, uint(500)).Return(nil, common.NotFoundError("role not found")).Once()
	// ListRolePermissions must not be consulted for another org's role

	_, err := svc.ListRolePermissions(context.Background(), testOrg, 500)

	requireRoleErr(t, err, http.StatusNotFound)
}

func TestService_ListRolePermissions_ReturnsTheRolesGrants(t *testing.T) {
	svc, repo, _ := newRolesService(t)
	want := []Permission{{ID: 1, Code: PermAccessPOSPortal}}
	repo.EXPECT().GetRole(mock.Anything, testOrg, uint(10)).Return(&Role{ID: 10}, nil).Once()
	repo.EXPECT().ListRolePermissions(mock.Anything, testOrg, uint(10)).Return(want, nil).Once()

	got, err := svc.ListRolePermissions(context.Background(), testOrg, 10)

	require.NoError(t, err)
	require.Equal(t, want, got)
}

// ---- POST /roles ----

func TestService_CreateRole_CreatesRoleWithSlugCodeGrantsAndAuditEntry(t *testing.T) {
	svc, repo, audW := newRolesService(t)
	repo.EXPECT().ListPermissions(mock.Anything).Return(catalogueRows(), nil).Once()
	repo.EXPECT().ListRoles(mock.Anything, testOrg).Return([]Role{{ID: 10, Code: "manager"}}, nil).Once()
	repo.EXPECT().
		CreateRole(mock.Anything, mock.MatchedBy(func(r *Role) bool {
			return r.OrgID != nil && *r.OrgID == testOrg && r.Code == "stock_clerk" && r.Name == "Stock Clerk" && !r.IsSystem
		})).
		Run(func(_ *gorm.DB, r *Role) { r.ID = 30 }).Return(nil).Once()
	repo.EXPECT().
		ReplaceRoleGrants(mock.Anything, testOrg, uint(30), []uint{idOf(PermAccessPOSPortal), idOf(PermAcceptPayments)}).
		Return(nil).Once()
	audW.EXPECT().CreateAuditLog(mock.Anything, auditFor("created", "30")).Return(nil).Once()

	got, err := svc.CreateRole(context.Background(), testOrg, testActor, CreateRoleRequest{
		Name:            "  Stock Clerk ",
		PermissionCodes: []string{PermAcceptPayments, PermAccessPOSPortal, PermAcceptPayments}, // unordered, duplicated
	})

	require.NoError(t, err)
	require.Equal(t, uint(30), got.ID)
	require.Equal(t, "stock_clerk", got.Code)
	require.Equal(t, "Stock Clerk", got.Name)
	require.False(t, got.IsSystem)
	require.True(t, got.Editable)
	require.True(t, got.POS)
	require.False(t, got.Backoffice)
	require.Equal(t, []string{PermAccessPOSPortal, PermAcceptPayments}, got.PermissionCodes, "catalogue order, de-duplicated")
	require.Empty(t, got.Warnings)
	require.NotNil(t, got.Warnings, "warnings is [] not null")
}

func TestService_CreateRole_WithNoPermissions_IsAllowed(t *testing.T) {
	svc, repo, audW := newRolesService(t)
	repo.EXPECT().ListRoles(mock.Anything, testOrg).Return(nil, nil).Once()
	repo.EXPECT().CreateRole(mock.Anything, mock.Anything).Run(func(_ *gorm.DB, r *Role) { r.ID = 31 }).Return(nil).Once()
	repo.EXPECT().ReplaceRoleGrants(mock.Anything, testOrg, uint(31), []uint(nil)).Return(nil).Once()
	audW.EXPECT().CreateAuditLog(mock.Anything, auditFor("created", "31")).Return(nil).Once()

	got, err := svc.CreateRole(context.Background(), testOrg, testActor, CreateRoleRequest{Name: "Empty"})

	require.NoError(t, err)
	require.Equal(t, []string{}, got.PermissionCodes)
}

func TestService_CreateRole_BackOfficePermissionWithoutTheSwitch_IsAcceptedWithAWarning(t *testing.T) {
	svc, repo, audW := newRolesService(t)
	repo.EXPECT().ListPermissions(mock.Anything).Return(catalogueRows(), nil).Once()
	repo.EXPECT().ListRoles(mock.Anything, testOrg).Return(nil, nil).Once()
	repo.EXPECT().CreateRole(mock.Anything, mock.Anything).Run(func(_ *gorm.DB, r *Role) { r.ID = 32 }).Return(nil).Once()
	repo.EXPECT().ReplaceRoleGrants(mock.Anything, testOrg, uint(32), mock.Anything).Return(nil).Once()
	audW.EXPECT().CreateAuditLog(mock.Anything, mock.Anything).Return(nil).Once()

	got, err := svc.CreateRole(context.Background(), testOrg, testActor, CreateRoleRequest{
		Name: "Reports only", PermissionCodes: []string{PermAccessPOSPortal, PermViewReports, PermManageCatalog},
	})

	require.NoError(t, err)
	require.Equal(t, []string{PermAccessPOSPortal, PermViewReports, PermManageCatalog}, got.PermissionCodes, "still granted")
	require.Len(t, got.Warnings, 1)
	require.Contains(t, got.Warnings[0], "Back office switch")
	require.Contains(t, got.Warnings[0], PermViewReports)
	require.Contains(t, got.Warnings[0], PermManageCatalog)
}

func TestService_CreateRole_PosPermissionWithoutThePosSwitch_IsAcceptedWithAWarning(t *testing.T) {
	svc, repo, audW := newRolesService(t)
	repo.EXPECT().ListPermissions(mock.Anything).Return(catalogueRows(), nil).Once()
	repo.EXPECT().ListRoles(mock.Anything, testOrg).Return(nil, nil).Once()
	repo.EXPECT().CreateRole(mock.Anything, mock.Anything).Run(func(_ *gorm.DB, r *Role) { r.ID = 33 }).Return(nil).Once()
	repo.EXPECT().ReplaceRoleGrants(mock.Anything, testOrg, uint(33), mock.Anything).Return(nil).Once()
	audW.EXPECT().CreateAuditLog(mock.Anything, mock.Anything).Return(nil).Once()

	got, err := svc.CreateRole(context.Background(), testOrg, testActor, CreateRoleRequest{
		Name: "No switch", PermissionCodes: []string{PermAccessBackOffice, PermAcceptPayments},
	})

	require.NoError(t, err)
	require.Len(t, got.Warnings, 1)
	require.Contains(t, got.Warnings[0], "POS switch")
	require.Contains(t, got.Warnings[0], PermAcceptPayments)
}

func TestService_CreateRole_RejectsABadName(t *testing.T) {
	for name, in := range map[string]string{
		"empty":           "",
		"blank":           "   ",
		"too long":        strings.Repeat("a", 51),
		"no letter/digit": "!!! ---",
	} {
		t.Run(name, func(t *testing.T) {
			svc, _, _ := newRolesService(t) // no repository calls may happen
			_, err := svc.CreateRole(context.Background(), testOrg, testActor, CreateRoleRequest{Name: in})
			requireRoleErr(t, err, http.StatusBadRequest)
		})
	}
}

func TestService_CreateRole_ExactlyFiftyCharactersIsFine(t *testing.T) {
	svc, repo, audW := newRolesService(t)
	repo.EXPECT().ListRoles(mock.Anything, testOrg).Return(nil, nil).Once()
	repo.EXPECT().CreateRole(mock.Anything, mock.Anything).Run(func(_ *gorm.DB, r *Role) { r.ID = 34 }).Return(nil).Once()
	repo.EXPECT().ReplaceRoleGrants(mock.Anything, testOrg, uint(34), mock.Anything).Return(nil).Once()
	audW.EXPECT().CreateAuditLog(mock.Anything, mock.Anything).Return(nil).Once()

	_, err := svc.CreateRole(context.Background(), testOrg, testActor, CreateRoleRequest{Name: strings.Repeat("a", 50)})

	require.NoError(t, err)
}

func TestService_CreateRole_UnknownPermissionCode_IsBadRequestAndWritesNothing(t *testing.T) {
	svc, _, _ := newRolesService(t) // no repository calls: rejected before anything is read or written

	_, err := svc.CreateRole(context.Background(), testOrg, testActor, CreateRoleRequest{
		Name: "Odd", PermissionCodes: []string{PermAccessPOSPortal, "can_fly", "also_bogus"},
	})

	requireRoleErr(t, err, http.StatusBadRequest)
	require.Contains(t, err.Error(), "can_fly")
	require.Contains(t, err.Error(), "also_bogus")
}

func TestService_CreateRole_NameAlreadyUsedInTheOrg_IsConflict(t *testing.T) {
	svc, repo, _ := newRolesService(t)
	repo.EXPECT().ListRoles(mock.Anything, testOrg).Return([]Role{{ID: 10, Code: "cashier", Name: "Cashier"}}, nil).Once()

	_, err := svc.CreateRole(context.Background(), testOrg, testActor, CreateRoleRequest{Name: "cashier"})

	requireRoleErr(t, err, http.StatusConflict)
}

func TestService_CreateRole_OwnerIsAReservedName(t *testing.T) {
	svc, repo, _ := newRolesService(t)
	repo.EXPECT().ListRoles(mock.Anything, testOrg).Return(nil, nil).Once()

	_, err := svc.CreateRole(context.Background(), testOrg, testActor, CreateRoleRequest{Name: "Owner"})

	requireRoleErr(t, err, http.StatusConflict)
}

func TestService_CreateRole_AuditFailureFailsTheWholeCreate(t *testing.T) {
	svc, repo, audW := newRolesService(t)
	repo.EXPECT().ListRoles(mock.Anything, testOrg).Return(nil, nil).Once()
	repo.EXPECT().CreateRole(mock.Anything, mock.Anything).Run(func(_ *gorm.DB, r *Role) { r.ID = 35 }).Return(nil).Once()
	repo.EXPECT().ReplaceRoleGrants(mock.Anything, testOrg, uint(35), mock.Anything).Return(nil).Once()
	audW.EXPECT().CreateAuditLog(mock.Anything, mock.Anything).Return(common.SystemError("audit down")).Once()

	_, err := svc.CreateRole(context.Background(), testOrg, testActor, CreateRoleRequest{Name: "Whatever"})

	requireRoleErr(t, err, http.StatusInternalServerError)
}

func TestRoleCodeFromName(t *testing.T) {
	cases := map[string]string{
		"Stock Clerk":         "stock_clerk",
		"  Night-shift  lead": "night_shift_lead",
		"Cashier (Senior)":    "cashier_senior",
		"A&B":                 "a_b",
		"Role 2":              "role_2",
		"!!!":                 "",
	}
	for name, want := range cases {
		require.Equal(t, want, roleCodeFromName(name), name)
	}
}

// ---- PATCH /roles/:id ----

func strp(s string) *string { return &s }

func TestService_UpdateRole_OwnerRowIsForbidden(t *testing.T) {
	svc, _, _ := newRolesService(t) // nothing may be read

	_, err := svc.UpdateRole(context.Background(), testOrg, testActor, 0, UpdateRoleRequest{Name: strp("Boss")})

	requireRoleErr(t, err, http.StatusForbidden)
}

func TestService_UpdateRole_AnotherOrgsRoleIsNotFound(t *testing.T) {
	svc, repo, _ := newRolesService(t)
	repo.EXPECT().GetRole(mock.Anything, testOrg, uint(500)).Return(nil, common.NotFoundError("role not found")).Once()

	_, err := svc.UpdateRole(context.Background(), testOrg, testActor, 500, UpdateRoleRequest{Name: strp("x")})

	requireRoleErr(t, err, http.StatusNotFound)
}

// expectRoleState wires the reads UpdateRole starts with: the role, every
// role's grants, and the staff counts.
func expectRoleState(repo *MockRepository, role Role, grants map[uint][]string, counts map[uint]int) {
	repo.EXPECT().GetRole(mock.Anything, testOrg, role.ID).Return(&role, nil).Once()
	repo.EXPECT().ListRoleGrantCodes(mock.Anything, testOrg).Return(grants, nil).Once()
	repo.EXPECT().CountStaffByRole(mock.Anything, testOrg).Return(counts, nil).Once()
}

func TestService_UpdateRole_RenamesAndReplacesGrants_Audited(t *testing.T) {
	svc, repo, audW := newRolesService(t)
	role := Role{ID: 11, Code: "cashier", Name: "Cashier", IsSystem: true}
	expectRoleState(repo, role, map[uint][]string{11: {PermAccessPOSPortal}, 10: {PermAccessPOSPortal}}, map[uint]int{11: 4})
	repo.EXPECT().ListRoles(mock.Anything, testOrg).Return([]Role{role, {ID: 10, Name: "Manager"}}, nil).Once()
	repo.EXPECT().ListPermissions(mock.Anything).Return(catalogueRows(), nil).Once()
	repo.EXPECT().UpdateRoleName(mock.Anything, testOrg, uint(11), "Till Operator").Return(nil).Once()
	repo.EXPECT().
		ReplaceRoleGrants(mock.Anything, testOrg, uint(11), []uint{idOf(PermAccessPOSPortal), idOf(PermAcceptPayments), idOf(PermApproveVoid)}).
		Return(nil).Once()
	audW.EXPECT().CreateAuditLog(mock.Anything, auditFor("updated", "11")).Return(nil).Once()
	codes := []string{PermApproveVoid, PermAcceptPayments, PermAccessPOSPortal}

	got, err := svc.UpdateRole(context.Background(), testOrg, testActor, 11, UpdateRoleRequest{Name: strp(" Till Operator "), PermissionCodes: &codes})

	require.NoError(t, err)
	require.Equal(t, "Till Operator", got.Name)
	require.Equal(t, "cashier", got.Code, "the code survives a rename")
	require.Equal(t, 4, got.StaffCount)
	require.Equal(t, []string{PermAccessPOSPortal, PermAcceptPayments, PermApproveVoid}, got.PermissionCodes)
}

func TestService_UpdateRole_NameOnly_LeavesGrantsAlone(t *testing.T) {
	svc, repo, audW := newRolesService(t)
	role := Role{ID: 12, Code: "clerk", Name: "Clerk"}
	expectRoleState(repo, role, map[uint][]string{12: {PermAccessPOSPortal, PermAcceptPayments}}, nil)
	repo.EXPECT().ListRoles(mock.Anything, testOrg).Return([]Role{role}, nil).Once()
	repo.EXPECT().UpdateRoleName(mock.Anything, testOrg, uint(12), "Clerk II").Return(nil).Once()
	audW.EXPECT().CreateAuditLog(mock.Anything, auditFor("updated", "12")).Return(nil).Once()
	// ReplaceRoleGrants must not be called

	got, err := svc.UpdateRole(context.Background(), testOrg, testActor, 12, UpdateRoleRequest{Name: strp("Clerk II")})

	require.NoError(t, err)
	require.Equal(t, []string{PermAccessPOSPortal, PermAcceptPayments}, got.PermissionCodes)
}

func TestService_UpdateRole_GrantsOnly_KeepsTheName(t *testing.T) {
	svc, repo, audW := newRolesService(t)
	role := Role{ID: 12, Code: "clerk", Name: "Clerk"}
	expectRoleState(repo, role, map[uint][]string{12: {PermAccessPOSPortal}, 10: {PermAccessPOSPortal}}, nil)
	repo.EXPECT().ListPermissions(mock.Anything).Return(catalogueRows(), nil).Once()
	repo.EXPECT().ReplaceRoleGrants(mock.Anything, testOrg, uint(12), []uint{idOf(PermAccessPOSPortal), idOf(PermReprintReceipt)}).Return(nil).Once()
	audW.EXPECT().CreateAuditLog(mock.Anything, auditFor("updated", "12")).Return(nil).Once()
	// UpdateRoleName must not be called
	codes := []string{PermAccessPOSPortal, PermReprintReceipt}

	got, err := svc.UpdateRole(context.Background(), testOrg, testActor, 12, UpdateRoleRequest{PermissionCodes: &codes})

	require.NoError(t, err)
	require.Equal(t, "Clerk", got.Name)
}

func TestService_UpdateRole_EmptyGrantList_ClearsEverythingUnlessItWasTheLastPosRole(t *testing.T) {
	svc, repo, audW := newRolesService(t)
	role := Role{ID: 12, Code: "clerk", Name: "Clerk"}
	// another role holds the POS switch, so clearing this one is fine
	expectRoleState(repo, role, map[uint][]string{12: {PermAccessPOSPortal, PermAcceptPayments}, 10: {PermAccessPOSPortal}}, nil)
	repo.EXPECT().ReplaceRoleGrants(mock.Anything, testOrg, uint(12), []uint(nil)).Return(nil).Once()
	audW.EXPECT().CreateAuditLog(mock.Anything, auditFor("updated", "12")).Return(nil).Once()
	none := []string{}

	got, err := svc.UpdateRole(context.Background(), testOrg, testActor, 12, UpdateRoleRequest{PermissionCodes: &none})

	require.NoError(t, err)
	require.Equal(t, []string{}, got.PermissionCodes)
	require.False(t, got.POS)
}

func TestService_UpdateRole_UnknownCode_IsBadRequestAndWritesNothing(t *testing.T) {
	svc, repo, _ := newRolesService(t)
	expectRoleState(repo, Role{ID: 12, Code: "clerk", Name: "Clerk"}, map[uint][]string{12: {PermAccessPOSPortal}}, nil)
	codes := []string{"can_fly"}

	_, err := svc.UpdateRole(context.Background(), testOrg, testActor, 12, UpdateRoleRequest{PermissionCodes: &codes})

	requireRoleErr(t, err, http.StatusBadRequest)
}

// Turning the POS switch off on the only role that has it would lock every
// cashier out of the till, with nobody able to sign in to fix it.
func TestService_UpdateRole_RemovingPosFromTheLastRoleThatHasIt_IsConflict(t *testing.T) {
	svc, repo, _ := newRolesService(t)
	role := Role{ID: 10, Code: "manager", Name: "Manager", IsSystem: true}
	expectRoleState(repo, role, map[uint][]string{
		10: {PermAccessPOSPortal, PermAccessBackOffice},
		11: {PermAccessBackOffice}, // no longer has POS
	}, nil)
	repo.EXPECT().ListPermissions(mock.Anything).Return(catalogueRows(), nil).Once()
	codes := []string{PermAccessBackOffice}

	_, err := svc.UpdateRole(context.Background(), testOrg, testActor, 10, UpdateRoleRequest{PermissionCodes: &codes})

	requireRoleErr(t, err, http.StatusConflict)
}

func TestService_UpdateRole_RemovingPosIsFineWhenAnotherRoleKeepsIt(t *testing.T) {
	svc, repo, audW := newRolesService(t)
	role := Role{ID: 10, Code: "manager", Name: "Manager", IsSystem: true}
	expectRoleState(repo, role, map[uint][]string{
		10: {PermAccessPOSPortal, PermAccessBackOffice},
		11: {PermAccessPOSPortal},
	}, nil)
	repo.EXPECT().ListPermissions(mock.Anything).Return(catalogueRows(), nil).Once()
	repo.EXPECT().ReplaceRoleGrants(mock.Anything, testOrg, uint(10), []uint{idOf(PermAccessBackOffice)}).Return(nil).Once()
	audW.EXPECT().CreateAuditLog(mock.Anything, mock.Anything).Return(nil).Once()
	codes := []string{PermAccessBackOffice}

	got, err := svc.UpdateRole(context.Background(), testOrg, testActor, 10, UpdateRoleRequest{PermissionCodes: &codes})

	require.NoError(t, err)
	require.False(t, got.POS)
	require.True(t, got.Backoffice)
}

// A role that never had the POS switch can be edited freely - the guard is
// only about taking it away.
func TestService_UpdateRole_RoleWithoutPos_IsNotSubjectToTheLastPosGuard(t *testing.T) {
	svc, repo, audW := newRolesService(t)
	role := Role{ID: 12, Code: "clerk", Name: "Clerk"}
	expectRoleState(repo, role, map[uint][]string{12: {PermAccessBackOffice}}, nil)
	repo.EXPECT().ListPermissions(mock.Anything).Return(catalogueRows(), nil).Once()
	repo.EXPECT().ReplaceRoleGrants(mock.Anything, testOrg, uint(12), []uint{idOf(PermAccessBackOffice), idOf(PermViewReports)}).Return(nil).Once()
	audW.EXPECT().CreateAuditLog(mock.Anything, mock.Anything).Return(nil).Once()
	codes := []string{PermViewReports, PermAccessBackOffice}

	_, err := svc.UpdateRole(context.Background(), testOrg, testActor, 12, UpdateRoleRequest{PermissionCodes: &codes})

	require.NoError(t, err)
}

func TestService_UpdateRole_RenameToANameTakenByAnotherRole_IsConflictCaseInsensitively(t *testing.T) {
	svc, repo, _ := newRolesService(t)
	role := Role{ID: 12, Code: "clerk", Name: "Clerk"}
	expectRoleState(repo, role, map[uint][]string{}, nil)
	repo.EXPECT().ListRoles(mock.Anything, testOrg).Return([]Role{role, {ID: 11, Code: "cashier", Name: "Cashier"}}, nil).Once()

	_, err := svc.UpdateRole(context.Background(), testOrg, testActor, 12, UpdateRoleRequest{Name: strp("CASHIER")})

	requireRoleErr(t, err, http.StatusConflict)
}

func TestService_UpdateRole_RejectsABadName(t *testing.T) {
	svc, repo, _ := newRolesService(t)
	expectRoleState(repo, Role{ID: 12, Code: "clerk", Name: "Clerk"}, map[uint][]string{}, nil)

	_, err := svc.UpdateRole(context.Background(), testOrg, testActor, 12, UpdateRoleRequest{Name: strp("   ")})

	requireRoleErr(t, err, http.StatusBadRequest)
}

// Sending the role's current name back changes nothing: no write, no audit entry.
func TestService_UpdateRole_NothingToChange_WritesNothing(t *testing.T) {
	svc, repo, _ := newRolesService(t)
	role := Role{ID: 12, Code: "clerk", Name: "Clerk"}
	expectRoleState(repo, role, map[uint][]string{12: {PermAccessPOSPortal}}, map[uint]int{12: 1})

	got, err := svc.UpdateRole(context.Background(), testOrg, testActor, 12, UpdateRoleRequest{Name: strp("Clerk")})

	require.NoError(t, err)
	require.Equal(t, "Clerk", got.Name)
	require.Equal(t, 1, got.StaffCount)
}

func TestService_UpdateRole_BackOfficePermissionsWithoutTheSwitch_WarnButSave(t *testing.T) {
	svc, repo, audW := newRolesService(t)
	role := Role{ID: 12, Code: "clerk", Name: "Clerk"}
	expectRoleState(repo, role, map[uint][]string{12: {PermAccessPOSPortal}, 10: {PermAccessPOSPortal}}, nil)
	repo.EXPECT().ListPermissions(mock.Anything).Return(catalogueRows(), nil).Once()
	repo.EXPECT().ReplaceRoleGrants(mock.Anything, testOrg, uint(12), mock.Anything).Return(nil).Once()
	audW.EXPECT().CreateAuditLog(mock.Anything, mock.Anything).Return(nil).Once()
	codes := []string{PermAccessPOSPortal, PermViewReports}

	got, err := svc.UpdateRole(context.Background(), testOrg, testActor, 12, UpdateRoleRequest{PermissionCodes: &codes})

	require.NoError(t, err)
	require.Len(t, got.Warnings, 1)
	require.Contains(t, got.Warnings[0], PermViewReports)
}

// ---- DELETE /roles/:id ----

func TestService_DeleteRole_DeletesAnUnusedCustomRole_Audited(t *testing.T) {
	svc, repo, audW := newRolesService(t)
	role := Role{ID: 12, Code: "clerk", Name: "Clerk"}
	repo.EXPECT().GetRole(mock.Anything, testOrg, uint(12)).Return(&role, nil).Once()
	repo.EXPECT().CountStaffByRole(mock.Anything, testOrg).Return(map[uint]int{10: 3}, nil).Once()
	repo.EXPECT().ListRoleGrantCodes(mock.Anything, testOrg).Return(map[uint][]string{12: {PermAccessPOSPortal}}, nil).Once()
	repo.EXPECT().DeleteRole(mock.Anything, testOrg, uint(12)).Return(nil).Once()
	audW.EXPECT().CreateAuditLog(mock.Anything, auditFor("deleted", "12")).Return(nil).Once()

	require.NoError(t, svc.DeleteRole(context.Background(), testOrg, testActor, 12))
}

func TestService_DeleteRole_OwnerRowIsForbidden(t *testing.T) {
	svc, _, _ := newRolesService(t)

	requireRoleErr(t, svc.DeleteRole(context.Background(), testOrg, testActor, 0), http.StatusForbidden)
}

func TestService_DeleteRole_SystemRoleIsConflict(t *testing.T) {
	svc, repo, _ := newRolesService(t)
	repo.EXPECT().GetRole(mock.Anything, testOrg, uint(10)).Return(&Role{ID: 10, Code: "manager", IsSystem: true}, nil).Once()

	requireRoleErr(t, svc.DeleteRole(context.Background(), testOrg, testActor, 10), http.StatusConflict)
}

func TestService_DeleteRole_RoleInUseIsConflict(t *testing.T) {
	svc, repo, _ := newRolesService(t)
	repo.EXPECT().GetRole(mock.Anything, testOrg, uint(12)).Return(&Role{ID: 12, Code: "clerk"}, nil).Once()
	repo.EXPECT().CountStaffByRole(mock.Anything, testOrg).Return(map[uint]int{12: 1}, nil).Once()

	requireRoleErr(t, svc.DeleteRole(context.Background(), testOrg, testActor, 12), http.StatusConflict)
}

func TestService_DeleteRole_AnotherOrgsRoleIsNotFound(t *testing.T) {
	svc, repo, _ := newRolesService(t)
	repo.EXPECT().GetRole(mock.Anything, testOrg, uint(500)).Return(nil, common.NotFoundError("role not found")).Once()

	requireRoleErr(t, svc.DeleteRole(context.Background(), testOrg, testActor, 500), http.StatusNotFound)
}

// ---- staff can only be given their own org's roles ----

func TestService_CreateStaff_RoleOfAnotherOrg_IsNotFoundAndNothingIsCreated(t *testing.T) {
	svc, repo, _ := newRolesService(t)
	repo.EXPECT().GetRole(mock.Anything, testOrg, uint(500)).Return(nil, common.NotFoundError("role not found")).Once()
	// CreateStaff must never be called

	_, err := svc.CreateStaff(context.Background(), testOrg, CreateStaffRequest{BranchID: 1, Name: "Joe", Role: 500, Pin: "123456", Phone: "1", Status: StaffStatusActive})

	requireRoleErr(t, err, http.StatusNotFound)
}

func TestService_UpdateStaff_RoleOfAnotherOrg_IsNotFoundAndNothingIsChanged(t *testing.T) {
	svc, repo, _ := newRolesService(t)
	repo.EXPECT().GetStaff(mock.Anything, testOrg, uint(5)).Return(&Staff{ID: 5, BranchID: 1}, nil).Once()
	repo.EXPECT().GetRole(mock.Anything, testOrg, uint(500)).Return(nil, common.NotFoundError("role not found")).Once()
	role := uint(500)

	_, err := svc.UpdateStaff(context.Background(), testOrg, testActor, 5, UpdateStaffRequest{Role: &role})

	requireRoleErr(t, err, http.StatusNotFound)
}

func TestService_UpdateStaff_OwnOrgsRole_IsAccepted(t *testing.T) {
	svc, repo, audW := newRolesService(t)
	before := &Staff{ID: 5, BranchID: 1, RoleID: 11}
	repo.EXPECT().GetStaff(mock.Anything, testOrg, uint(5)).Return(before, nil).Once()
	repo.EXPECT().GetRole(mock.Anything, testOrg, uint(10)).Return(&Role{ID: 10}, nil).Once()
	role := uint(10)
	repo.EXPECT().UpdateStaff(mock.Anything, testOrg, uint(5), mock.Anything).Return(&Staff{ID: 5, BranchID: 1, RoleID: 10}, nil).Once()
	audW.EXPECT().CreateAuditLog(mock.Anything, mock.Anything).Return(nil).Once()

	got, err := svc.UpdateStaff(context.Background(), testOrg, testActor, 5, UpdateStaffRequest{Role: &role})

	require.NoError(t, err)
	require.Equal(t, uint(10), got.RoleID)
}

// ---- a new org gets its roles ----

func TestService_CreateAccount_ProvisionsManagerAndCashierInTheSameTransaction(t *testing.T) {
	svc, repo, audW := newRolesService(t)
	audW.EXPECT().CreateAuditLog(mock.Anything, mock.Anything).Return(nil).Once()
	repo.EXPECT().CreateOrganization(mock.Anything, mock.Anything).Run(func(_ *gorm.DB, org *Organization) { org.ID = 41 }).Return(nil).Once()
	repo.EXPECT().ProvisionDefaultRoles(mock.Anything, uint(41)).Return(nil).Once()
	repo.EXPECT().CreateUser(mock.Anything, mock.Anything).Run(func(_ *gorm.DB, u *User) { u.ID = 1 }).Return(nil).Twice()

	_, err := svc.CreateAccount(context.Background(), CreateAccountInput{
		OrganizationName: "New Org", OwnerEmail: "o@new.test", OwnerPassword: "correct horse battery",
		ServiceCenterEmail: "sc@new.test", ServiceCenterPassword: "another strong password",
	})

	require.NoError(t, err)
}

func TestService_CreateAccount_ProvisioningFails_NoUsersAreCreated(t *testing.T) {
	svc, repo, audW := newRolesService(t)
	audW.EXPECT().CreateAuditLog(mock.Anything, mock.Anything).Return(nil).Once()
	repo.EXPECT().CreateOrganization(mock.Anything, mock.Anything).Run(func(_ *gorm.DB, org *Organization) { org.ID = 42 }).Return(nil).Once()
	repo.EXPECT().ProvisionDefaultRoles(mock.Anything, uint(42)).Return(common.SystemError("roles failed")).Once()
	// CreateUser must never be called

	_, err := svc.CreateAccount(context.Background(), CreateAccountInput{
		OrganizationName: "New Org", OwnerEmail: "o@new.test", OwnerPassword: "correct horse battery",
		ServiceCenterEmail: "sc@new.test", ServiceCenterPassword: "another strong password",
	})

	requireRoleErr(t, err, http.StatusInternalServerError)
}
