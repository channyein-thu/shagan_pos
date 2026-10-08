package returns

import "gorm.io/gorm"

// AutoMigrate creates/updates the tables for the returns domain's models.
// It never drops or alters existing columns - see gorm's AutoMigrate docs.
func AutoMigrate(db *gorm.DB) error {
	if err := db.AutoMigrate(
		&Void{},
		&Return{},
		&ReturnItem{},
		&Exchange{},
		&ExchangeItem{},
	); err != nil {
		return err
	}
	return relaxVoidApprovedBy(db)
}

// relaxVoidApprovedBy drops the NOT NULL constraint on voids.approved_by. A
// sale the Owner voids directly has no Staff record to point at (it records
// approved_by_user_id instead), so approved_by must now allow NULL - same
// reasoning, and same safe-every-startup guard, as
// shift.relaxExpenseCreatedBy.
func relaxVoidApprovedBy(db *gorm.DB) error {
	if !db.Migrator().HasTable(&Void{}) || !db.Migrator().HasColumn(&Void{}, "approved_by") {
		return nil
	}
	return db.Exec("ALTER TABLE voids ALTER COLUMN approved_by DROP NOT NULL").Error
}
