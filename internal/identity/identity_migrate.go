package identity

import (
	"gorm.io/gorm"

	"shagan_pos/internal/common"
)

// AutoMigrate creates/updates the tables for the identity domain's models.
// It never drops or alters existing columns - see gorm's AutoMigrate docs.
func AutoMigrate(db *gorm.DB) error {
	return db.AutoMigrate(
		&Organization{},
		&Branch{},
		&Device{},
		&User{},
		&Session{},
		&Staff{},
		&Permission{},
		&RolePermission{},
		&OrgRolePermission{},
		&common.DataMigration{},
		&Role{},
	)
}
