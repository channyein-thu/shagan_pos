package catalog

import "gorm.io/gorm"

// AutoMigrate creates/updates the tables for the catalog domain's models.
// It never drops or alters existing columns - see gorm's AutoMigrate docs -
// which is why the branch_scope cleanup below is a separate, explicit step.
func AutoMigrate(db *gorm.DB) error {
	if err := db.AutoMigrate(
		&Product{},
		&Category{},
		&ProductImage{},
		&Combo{},
		&ComboItem{},
		&ComboImage{},
	); err != nil {
		return err
	}
	return dropStaleBranchScopeColumn(db)
}

// dropStaleBranchScopeColumn removes products.branch_scope, a NOT NULL
// column left over from before Product.BranchID replaced the old
// BranchScope all/specific enum. AutoMigrate never drops or alters existing
// columns, so any database created before that refactor still has this
// column with its NOT NULL constraint - which rejects every product insert,
// since the current Product struct never sets it. Guarded to run only if
// the column still exists, so it's a safe no-op on a database that never
// had it (a fresh AutoMigrate run, or one already cleaned up).
func dropStaleBranchScopeColumn(db *gorm.DB) error {
	if !db.Migrator().HasColumn(&Product{}, "branch_scope") {
		return nil
	}
	return db.Migrator().DropColumn(&Product{}, "branch_scope")
}
