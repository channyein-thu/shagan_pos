package audit

import "gorm.io/gorm"

// AutoMigrate creates/updates the tables for the audit domain's models.
// It never drops or alters existing columns - see gorm's AutoMigrate docs -
// which is why the entity_id widening below is a separate, explicit step.
func AutoMigrate(db *gorm.DB) error {
	if err := db.AutoMigrate(
		&AuditLog{},
	); err != nil {
		return err
	}
	return widenEntityIDToText(db)
}

// widenEntityIDToText widens audit_log.entity_id from integer to text -
// same reasoning as inventory.widenReferenceIDToText (EntityID now holds a
// Sale's UUID string for "sale" entries, not just a uint's decimal string).
// Guarded to run only when the column is still numeric, so it's a safe
// no-op on a database that never had it.
func widenEntityIDToText(db *gorm.DB) error {
	var dataType string
	err := db.Raw(`
		SELECT data_type FROM information_schema.columns
		WHERE table_name = 'audit_log' AND column_name = 'entity_id'
	`).Scan(&dataType).Error
	if err != nil {
		return err
	}
	if dataType != "integer" && dataType != "bigint" && dataType != "smallint" {
		return nil
	}
	return db.Exec(`ALTER TABLE audit_log ALTER COLUMN entity_id TYPE text USING entity_id::text`).Error
}
