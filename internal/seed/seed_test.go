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

// permissionCodes returns the codes granted to roleCode, with duplicates kept
// so a double grant shows up instead of being hidden by a set.
func permissionCodes(t *testing.T, db *gorm.DB, roleCode string) []string {
	t.Helper()
	var role identity.Role
	require.NoError(t, db.Where("code = ?", roleCode).First(&role).Error)
	var codes []string
	require.NoError(t, db.Table("role_permission").
		Select("permissions.code").
		Joins("JOIN permissions ON permissions.id = role_permission.permission_id").
		Where("role_permission.role_id = ?", role.ID).
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

func TestRun_ManagerIsGrantedOpenDrawerNoSale(t *testing.T) {
	db := newSeedTestDB(t)

	require.NoError(t, Run(db))

	require.Equal(t, 1, count(permissionCodes(t, db, "manager"), "open_drawer_no_sale"))
	require.Equal(t, 1, count(permissionCodes(t, db, "super_staff"), "open_drawer_no_sale"))
	require.Equal(t, 0, count(permissionCodes(t, db, "staff"), "open_drawer_no_sale"))
}

func TestRun_SeedingTwiceDoesNotDuplicateGrants(t *testing.T) {
	db := newSeedTestDB(t)

	require.NoError(t, Run(db))
	require.NoError(t, Run(db))

	require.Equal(t, 1, count(permissionCodes(t, db, "manager"), "open_drawer_no_sale"))
	require.Equal(t, 7, len(permissionCodes(t, db, "manager")))
}

// An existing database seeded before this grant existed must pick it up on
// the next run, not only fresh databases.
func TestRun_AddsMissingManagerGrantToAnAlreadySeededDatabase(t *testing.T) {
	db := newSeedTestDB(t)
	require.NoError(t, Run(db))
	var manager identity.Role
	require.NoError(t, db.Where("code = ?", "manager").First(&manager).Error)
	var perm identity.Permission
	require.NoError(t, db.Where("code = ?", "open_drawer_no_sale").First(&perm).Error)
	require.NoError(t, db.Where("role_id = ? AND permission_id = ?", manager.ID, perm.ID).Delete(&identity.RolePermission{}).Error)
	require.Equal(t, 0, count(permissionCodes(t, db, "manager"), "open_drawer_no_sale"))

	require.NoError(t, Run(db))

	require.Equal(t, 1, count(permissionCodes(t, db, "manager"), "open_drawer_no_sale"))
}
