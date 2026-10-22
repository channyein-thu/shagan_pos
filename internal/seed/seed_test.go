//go:build cgo

package seed

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"shagan_pos/internal/identity"
)

func newSeedTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	name := strings.NewReplacer("/", "_", " ", "_").Replace(t.Name())
	db, err := gorm.Open(
		sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", name)),
		&gorm.Config{Logger: logger.Default.LogMode(logger.Silent)},
	)
	require.NoError(t, err)
	require.NoError(t, identity.AutoMigrate(db))
	return db
}

func seedOrg(t *testing.T, db *gorm.DB, name string) identity.Organization {
	t.Helper()
	org := identity.Organization{Name: name, Timezone: "Asia/Yangon"}
	require.NoError(t, db.Create(&org).Error)
	return org
}

// roleGrantCodes lists the permission codes granted to orgID's role roleCode,
// duplicates kept so a double grant shows up instead of being hidden by a set.
func roleGrantCodes(t *testing.T, db *gorm.DB, orgID uint, roleCode string) []string {
	t.Helper()
	var role identity.Role
	require.NoError(t, db.Where("org_id = ? AND code = ?", orgID, roleCode).First(&role).Error)
	var codes []string
	require.NoError(t, db.Table("org_role_permission").
		Select("permissions.code").
		Joins("JOIN permissions ON permissions.id = org_role_permission.permission_id").
		Where("org_role_permission.role_id = ?", role.ID).
		Scan(&codes).Error)
	return codes
}

func count(codes []string, code string) int {
	n := 0
	for _, c := range codes {
		if c == code {
			n++
		}
	}
	return n
}

func TestRun_SeedsTheWholePermissionCatalogue(t *testing.T) {
	db := newSeedTestDB(t)

	require.NoError(t, Run(db))

	var n int64
	require.NoError(t, db.Model(&identity.Permission{}).Count(&n).Error)
	require.Equal(t, int64(len(identity.PermissionCatalogue)), n)
}

// Task 2 (R13): the manager can open the drawer without a sale, exactly once.
func TestRun_ManagerIsGrantedOpenDrawerNoSale_AndCashierIsNot(t *testing.T) {
	db := newSeedTestDB(t)
	org := seedOrg(t, db, "Org A")

	require.NoError(t, Run(db))

	require.Equal(t, 1, count(roleGrantCodes(t, db, org.ID, identity.RoleCodeManager), identity.PermOpenDrawerNoSale))
	require.Equal(t, 0, count(roleGrantCodes(t, db, org.ID, identity.RoleCodeCashier), identity.PermOpenDrawerNoSale))
}

func TestRun_SeedingTwiceDoesNotDuplicateRolesOrGrants(t *testing.T) {
	db := newSeedTestDB(t)
	org := seedOrg(t, db, "Org A")

	require.NoError(t, Run(db))
	require.NoError(t, Run(db))

	manager := roleGrantCodes(t, db, org.ID, identity.RoleCodeManager)
	require.Equal(t, 1, count(manager, identity.PermOpenDrawerNoSale))
	require.Len(t, manager, len(identity.DefaultRoles[0].Grants))
	var roles int64
	require.NoError(t, db.Model(&identity.Role{}).Where("org_id = ?", org.ID).Count(&roles).Error)
	require.Equal(t, int64(2), roles)
}

func TestRun_GivesEveryOrgWithoutRolesItsOwn(t *testing.T) {
	db := newSeedTestDB(t)
	a, b := seedOrg(t, db, "Org A"), seedOrg(t, db, "Org B")

	require.NoError(t, Run(db))

	var managerA, managerB identity.Role
	require.NoError(t, db.Where("org_id = ? AND code = ?", a.ID, identity.RoleCodeManager).First(&managerA).Error)
	require.NoError(t, db.Where("org_id = ? AND code = ?", b.ID, identity.RoleCodeManager).First(&managerB).Error)
	require.NotEqual(t, managerA.ID, managerB.ID)
}

// An Owner's edit survives a re-seed - the seed only fills in what's missing.
func TestRun_DoesNotResetARoleAnOwnerHasEdited(t *testing.T) {
	db := newSeedTestDB(t)
	org := seedOrg(t, db, "Org A")
	require.NoError(t, Run(db))
	var manager identity.Role
	require.NoError(t, db.Where("org_id = ? AND code = ?", org.ID, identity.RoleCodeManager).First(&manager).Error)
	var perm identity.Permission
	require.NoError(t, db.Where("code = ?", identity.PermOpenDrawerNoSale).First(&perm).Error)
	require.NoError(t, db.Where("org_id = ? AND role_id = ? AND permission_id = ?", org.ID, manager.ID, perm.ID).Delete(&identity.OrgRolePermission{}).Error)

	require.NoError(t, Run(db))

	require.Equal(t, 0, count(roleGrantCodes(t, db, org.ID, identity.RoleCodeManager), identity.PermOpenDrawerNoSale))
}
