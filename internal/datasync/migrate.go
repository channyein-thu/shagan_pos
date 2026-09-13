package datasync

import "gorm.io/gorm"

// AutoMigrate creates/updates the tables for the datasync domain's models.
// It never drops or alters existing columns - see gorm's AutoMigrate docs.
func AutoMigrate(db *gorm.DB) error {
	return db.AutoMigrate(
		&IdempotencyKey{},
		&SyncConflict{},
	)
}
