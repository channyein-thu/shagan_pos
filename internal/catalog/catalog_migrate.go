package catalog

import "gorm.io/gorm"

// AutoMigrate creates/updates the tables for the catalog domain's models.
// It never drops or alters existing columns - see gorm's AutoMigrate docs -
// which is why the stale-column cleanups below are separate, explicit steps.
// dropStaleProductBranchIDColumn runs before the main pass so the old
// {branch_id, barcode} unique index (which Postgres auto-drops along with
// the column) is gone before AutoMigrate tries to create the new
// {org_id, barcode} one declared on the current Product struct.
func AutoMigrate(db *gorm.DB) error {
	if err := dropStaleProductBranchIDColumn(db); err != nil {
		return err
	}
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
	if err := dropStaleBranchScopeColumn(db); err != nil {
		return err
	}
	return backfillProductDiscountPercent(db)
}

// backfillProductDiscountPercent moves products from the old fixed per-unit
// discount amount to a percent, without changing a single price: every product
// still without a sale price gets sale_price = price - discount (exactly what
// the till charged before; deliberately NOT re-rounded to K 100) and
// discount_percent = discount / price x 100 (2 dp). The amount in `discount` itself
// is left alone, so price - sale_price still equals it.
//
// Idempotent: it only touches rows whose sale_price is still 0, which a new
// column's default gives every pre-existing row. A product the server has
// priced since then has a non-zero sale price and is never revisited; one that
// is genuinely free (100%) is revisited but comes out identical.
func backfillProductDiscountPercent(db *gorm.DB) error {
	return db.Exec(`UPDATE products
		SET discount_percent = ROUND(discount * 100.0 / price, 2), sale_price = price - discount
		WHERE sale_price = 0 AND price > 0`).Error
}

// dropStaleProductBranchIDColumn removes products.branch_id, a NOT NULL
// column left over from before Product became org-wide (see the Product
// struct's doc) - Postgres automatically drops the composite
// ux_products_branch_barcode index along with it, since branch_id was one of
// its columns. Guarded to run only if the column still exists, so it's a
// safe no-op on a database that never had it.
func dropStaleProductBranchIDColumn(db *gorm.DB) error {
	if !db.Migrator().HasColumn(&Product{}, "branch_id") {
		return nil
	}
	return db.Migrator().DropColumn(&Product{}, "branch_id")
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
