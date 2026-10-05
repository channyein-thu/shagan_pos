package platform

import "gorm.io/gorm"

// AutoMigrate creates/updates the tables for the platform domain's models.
// It never drops or alters existing columns - see gorm's AutoMigrate docs.
func AutoMigrate(db *gorm.DB) error {
	if err := db.AutoMigrate(
		&Translation{},
		&ReceiptSetting{},
		&PaymentQRCode{},
	); err != nil {
		return err
	}
	return dropStaleProviderColumn(db)
}

// dropStaleProviderColumn removes payment_qr_codes.provider, a NOT NULL
// column left over from before PaymentQRCode.BankName replaced the old
// free-form Provider field. AutoMigrate never drops or alters existing
// columns, so any database created before that rename still has this column
// with its NOT NULL constraint - which rejects every QR code insert, since
// the current PaymentQRCode struct never sets it. Guarded to run only if the
// column still exists, so it's a safe no-op on a database that never had it
// (a fresh AutoMigrate run, or one already cleaned up) - same reasoning as
// catalog.dropStaleBranchScopeColumn.
func dropStaleProviderColumn(db *gorm.DB) error {
	if !db.Migrator().HasColumn(&PaymentQRCode{}, "provider") {
		return nil
	}
	return db.Migrator().DropColumn(&PaymentQRCode{}, "provider")
}
