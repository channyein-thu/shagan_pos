package procurement

import "gorm.io/gorm"

// AutoMigrate creates/updates the tables for the procurement domain's models.
// It never drops or alters existing columns - see gorm's AutoMigrate docs -
// which is why backfillPurchaseOrderBranchID is a separate, explicit step.
func AutoMigrate(db *gorm.DB) error {
	if err := backfillPurchaseOrderBranchID(db); err != nil {
		return err
	}
	return db.AutoMigrate(
		&Supplier{},
		&PurchaseOrder{},
		&PurchaseOrderItem{},
		&GoodsReceipt{},
		&GoodsReceiptItem{},
	)
}

// backfillPurchaseOrderBranchID adds purchase_orders.branch_id as nullable
// and backfills existing rows (from before PurchaseOrder carried a branch -
// see PurchaseOrder's own doc) with their org's first branch, before
// AutoMigrate's main pass tries to add it as NOT NULL directly - which
// Postgres rejects outright on a table that already has rows. Guarded to
// run only if the column doesn't exist yet, so it's a safe no-op on a
// database that's already past this (a fresh AutoMigrate run, or one
// already backfilled). Any row the backfill can't resolve a branch for
// (org_id=0, from a since-fixed bug where CreatePurchaseOrder never set
// OrgID at all - confirmed via live testing, not real data) is deleted
// along with its own PurchaseOrderItems/GoodsReceipts/GoodsReceiptItems,
// rather than left to block every future migration with a NOT NULL
// violation it can never satisfy.
func backfillPurchaseOrderBranchID(db *gorm.DB) error {
	if !db.Migrator().HasTable(&PurchaseOrder{}) {
		// fresh database - AutoMigrate's normal pass below creates the
		// table from scratch with branch_id already NOT NULL, no existing
		// rows to conflict with.
		return nil
	}
	if db.Migrator().HasColumn(&PurchaseOrder{}, "branch_id") {
		return nil
	}
	if err := db.Exec(`ALTER TABLE purchase_orders ADD COLUMN branch_id bigint`).Error; err != nil {
		return err
	}
	if err := db.Exec(`
		UPDATE purchase_orders po
		SET branch_id = (
			SELECT b.id FROM branches b WHERE b.org_id = po.org_id ORDER BY b.id LIMIT 1
		)
		WHERE po.branch_id IS NULL
	`).Error; err != nil {
		return err
	}
	if err := db.Exec(`
		DELETE FROM goods_receipt_items WHERE receipt_id IN (
			SELECT id FROM goods_receipts WHERE po_id IN (SELECT id FROM purchase_orders WHERE branch_id IS NULL)
		)
	`).Error; err != nil {
		return err
	}
	if err := db.Exec(`
		DELETE FROM goods_receipts WHERE po_id IN (SELECT id FROM purchase_orders WHERE branch_id IS NULL)
	`).Error; err != nil {
		return err
	}
	if err := db.Exec(`
		DELETE FROM purchase_order_items WHERE po_id IN (SELECT id FROM purchase_orders WHERE branch_id IS NULL)
	`).Error; err != nil {
		return err
	}
	if err := db.Exec(`DELETE FROM purchase_orders WHERE branch_id IS NULL`).Error; err != nil {
		return err
	}
	return db.Exec(`ALTER TABLE purchase_orders ALTER COLUMN branch_id SET NOT NULL`).Error
}
