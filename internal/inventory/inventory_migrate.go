package inventory

import "gorm.io/gorm"

// AutoMigrate creates/updates the tables for the inventory domain's models.
// It never drops or alters existing columns - see gorm's AutoMigrate docs -
// which is why the reference_id widening below is a separate, explicit step.
func AutoMigrate(db *gorm.DB) error {
	if err := db.AutoMigrate(
		&StockLevel{},
		&StockAdjustment{},
		&StockTransfer{},
		&StockTransferItem{},
		&InventoryLedger{},
	); err != nil {
		return err
	}
	return widenReferenceIDToText(db)
}

// widenReferenceIDToText widens inventory_ledger.reference_id from integer
// to text. It was originally created as an integer column (every
// ReferenceType was uint-keyed at the time), but Sale.ID is a UUID, not a
// uint - AutoMigrate never alters an existing column's type, so this is a
// separate, explicit step. Guarded to run only when the column is still
// numeric, so it's a safe no-op on a database that never had it (a fresh
// AutoMigrate run, or one already widened).
func widenReferenceIDToText(db *gorm.DB) error {
	var dataType string
	err := db.Raw(`
		SELECT data_type FROM information_schema.columns
		WHERE table_name = 'inventory_ledger' AND column_name = 'reference_id'
	`).Scan(&dataType).Error
	if err != nil {
		return err
	}
	if dataType != "integer" && dataType != "bigint" && dataType != "smallint" {
		return nil
	}
	return db.Exec(`ALTER TABLE inventory_ledger ALTER COLUMN reference_id TYPE text USING reference_id::text`).Error
}
