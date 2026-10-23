package sales

import "gorm.io/gorm"

// AutoMigrate creates/updates the tables for the sales domain's models.
// It never drops or alters existing columns - see gorm's AutoMigrate docs.
func AutoMigrate(db *gorm.DB) error {
	if err := db.AutoMigrate(
		&Sale{},
		&SaleItem{},
		&Payment{},
		&HeldSale{},
		&DiscountApproval{},
	); err != nil {
		return err
	}
	return backfillComboDiscount(db)
}

// backfillComboDiscount classifies the discount of lines sold before the
// discount was split by source. A combo line's discount has only ever been its
// share of the bundle discount, so it is exactly combo_discount. A plain line's
// discount could have been a catalog or a manual one - there is no way to tell
// after the fact - so those stay only in `discount` and the new columns read 0.
// Nothing here changes `discount`, line_total or any sale total.
//
// Idempotent: it only touches lines whose combo_discount is still 0.
func backfillComboDiscount(db *gorm.DB) error {
	return db.Exec(`UPDATE sale_items SET combo_discount = discount
		WHERE combo_id IS NOT NULL AND combo_discount = 0 AND discount > 0`).Error
}
