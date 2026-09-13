package audit

import (
	"time"

	"gorm.io/datatypes"
)

// TODO: relationships (belongs-to/has-many) are intentionally omitted here;
// wire them up as needed in repository.go queries.
// AuditLog maps to the "audit_log" table in the ERD.
type AuditLog struct {
	ID        uint           `gorm:"primaryKey;autoIncrement" json:"id"`
	OrgID     uint           `gorm:"index;not null" json:"org_id"`
	ActorID   *uint          `gorm:"index" json:"actor_id"`
	BranchID  *uint          `json:"branch_id"`
	Entity    string         `gorm:"size:255;not null" json:"entity"`
	EntityID  uint           `gorm:"not null" json:"entity_id"` // polymorphic: paired with Entity (table name), not a DB-level FK - see docs/db-schema.md
	Action    string         `gorm:"size:255;not null" json:"action"`
	Before    datatypes.JSON `gorm:"type:jsonb;not null" json:"before"`
	After     datatypes.JSON `gorm:"type:jsonb;not null" json:"after"`
	CreatedAt time.Time      `gorm:"autoCreateTime;not null" json:"created_at"`
}

func (AuditLog) TableName() string { return "audit_log" }
