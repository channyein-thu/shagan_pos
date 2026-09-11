package seed

import "gorm.io/gorm"

// Run seeds baseline reference data (default organization, roles, permissions) for local development.
// TODO: implement per-domain seed data once models are finalized.
func Run(db *gorm.DB) error {
	return nil
}
