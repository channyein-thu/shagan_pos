package common

import (
	"fmt"
	"time"

	"gorm.io/gorm"
)

// DataMigration records a one-off data change that has already been applied, so
// it is not applied again on the next start. Unlike an idempotent "create what
// is missing" step, a change that GRANTS something or CONVERTS values would
// undo an Owner's later edit if it ran every start.
type DataMigration struct {
	Name      string    `gorm:"primaryKey;size:100"`
	AppliedAt time.Time `gorm:"autoCreateTime;not null"`
}

func (DataMigration) TableName() string { return "data_migrations" }

// RunOnce runs fn in a transaction and records name, unless name is already
// recorded. The table must exist (AutoMigrate &DataMigration{} first).
func RunOnce(db *gorm.DB, name string, fn func(tx *gorm.DB) error) error {
	return db.Transaction(func(tx *gorm.DB) error {
		var done int64
		if err := tx.Model(&DataMigration{}).Where("name = ?", name).Count(&done).Error; err != nil {
			return err
		}
		if done > 0 {
			return nil
		}
		if err := fn(tx); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		return tx.Create(&DataMigration{Name: name}).Error
	})
}
