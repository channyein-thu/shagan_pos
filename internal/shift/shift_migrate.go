package shift

import "gorm.io/gorm"

// AutoMigrate creates/updates the tables for the shift domain's models.
// It never drops or alters existing columns - see gorm's AutoMigrate docs.
func AutoMigrate(db *gorm.DB) error {
	if err := db.AutoMigrate(
		&Shift{},
		&ShiftReconciliation{},
		&DrawerEvent{},
		&Expense{},
	); err != nil {
		return err
	}
	return relaxExpenseCreatedBy(db)
}

// relaxExpenseCreatedBy drops the NOT NULL constraint on expenses.created_by.
// An expense an Owner logs themself has no Staff record to point at (it
// records created_by_user_id instead), so created_by must now allow NULL.
// AutoMigrate never drops or alters existing columns, so a database created
// before this change still carries the constraint. DROP NOT NULL is a no-op
// on an already-nullable column, so this is safe to run every startup;
// guarded so it never touches a database that has no such table/column yet.
func relaxExpenseCreatedBy(db *gorm.DB) error {
	if !db.Migrator().HasTable(&Expense{}) || !db.Migrator().HasColumn(&Expense{}, "created_by") {
		return nil
	}
	return db.Exec("ALTER TABLE expenses ALTER COLUMN created_by DROP NOT NULL").Error
}
