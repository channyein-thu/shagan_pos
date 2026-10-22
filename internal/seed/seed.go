package seed

import (
	"gorm.io/gorm"

	"shagan_pos/internal/identity"
)

// Run seeds baseline reference data for local development and any fresh
// environment: the permission catalogue (identity.PermissionCatalogue - the
// checklist an Owner picks from) and, for every organization that has no roles
// of its own yet, its Manager and Cashier roles with the default grants.
//
// Roles and their grants are per organization now - there is no global role
// matrix to seed - so this only ever creates what's missing: an org that
// already has roles (including one whose Owner has edited them) is left
// exactly as it is. The same function runs at API start-up via migrate.Run,
// which is also what moves a pre-existing database from the old global roles
// onto per-org ones. Idempotent: safe to run any number of times.
func Run(db *gorm.DB) error {
	return identity.MigrateAccessRights(db)
}
