package migrate

import (
	"fmt"

	"gorm.io/gorm"

	"shagan_pos/internal/audit"
	"shagan_pos/internal/catalog"
	"shagan_pos/internal/customer"
	"shagan_pos/internal/datasync"
	"shagan_pos/internal/identity"
	"shagan_pos/internal/inventory"
	"shagan_pos/internal/platform"
	"shagan_pos/internal/procurement"
	"shagan_pos/internal/returns"
	"shagan_pos/internal/sales"
	"shagan_pos/internal/shift"
)

// Run auto-migrates every domain's models against db. Order doesn't matter for
// correctness here since no Go-level relationships (and therefore no DB-level FK
// constraints) are declared on these models - see internal/<domain>/model.go.
func Run(db *gorm.DB) error {
	migrators := []struct {
		name string
		fn   func(*gorm.DB) error
	}{
		{"identity", identity.AutoMigrate},
		{"customer", customer.AutoMigrate},
		{"platform", platform.AutoMigrate},
		{"catalog", catalog.AutoMigrate},
		{"procurement", procurement.AutoMigrate},
		{"inventory", inventory.AutoMigrate},
		{"sales", sales.AutoMigrate},
		{"returns", returns.AutoMigrate},
		{"shift", shift.AutoMigrate},
		{"datasync", datasync.AutoMigrate},
		{"audit", audit.AutoMigrate},
	}

	for _, m := range migrators {
		if err := m.fn(db); err != nil {
			return fmt.Errorf("migrate %s: %w", m.name, err)
		}
	}
	return nil
}
