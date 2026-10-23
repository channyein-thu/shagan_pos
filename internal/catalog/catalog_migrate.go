package catalog

import (
	"log/slog"

	"gorm.io/gorm"

	"shagan_pos/internal/common"
)

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
		&common.DataMigration{},
	); err != nil {
		return err
	}
	if err := dropStaleBranchScopeColumn(db); err != nil {
		return err
	}
	if err := backfillProductDiscountPercent(db); err != nil {
		return err
	}
	// must come after the discount backfill: the per-unit tax it re-derives is
	// taken from the sale price
	return common.RunOnce(db, migrationProductTaxPercent, convertProductTaxToPercent)
}

const migrationProductTaxPercent = "2026-10-23-product-tax-percent"

// convertProductTaxToPercent moves products from the old fixed per-unit tax
// AMOUNT (which the till added on top of the price) to a RATE that the price
// already contains. tax_percent = tax / price x 100 (2 dp, capped at 100) and
// the price is deliberately LEFT ALONE: those customers paid price + tax
// before and will pay just the price from now on, so the Owner has to review
// every product converted here - each one is logged. The per-unit `tax` column
// is then re-derived as the tax inside the sale price.
//
// Run once (data_migrations): a conversion that ran on every start would
// overwrite a rate the Owner later chose with whatever the old amount implied.
func convertProductTaxToPercent(tx *gorm.DB) error {
	var converted []struct {
		ID    uint
		Name  string
		Price string
		Tax   string
	}
	if err := tx.Raw(`SELECT id, name, price, tax FROM products
		WHERE tax > 0 AND price > 0 AND tax_percent = 0 ORDER BY id`).Scan(&converted).Error; err != nil {
		return err
	}
	if err := tx.Exec(`UPDATE products
		SET tax_percent = CASE WHEN tax * 100.0 / price > 100 THEN 100 ELSE ROUND(tax * 100.0 / price, 2) END
		WHERE tax > 0 AND price > 0 AND tax_percent = 0`).Error; err != nil {
		return err
	}
	if err := tx.Exec(`UPDATE products
		SET tax = ROUND(sale_price * tax_percent * 1.0 / (100 + tax_percent), 2)
		WHERE tax_percent > 0`).Error; err != nil {
		return err
	}
	for _, p := range converted {
		slog.Warn("product tax converted from a fixed amount to a percent already inside the price - review it",
			"product_id", p.ID, "name", p.Name, "price", p.Price, "old_tax_amount", p.Tax)
	}
	return nil
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
