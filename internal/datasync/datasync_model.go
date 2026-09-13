package datasync

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
)

// TODO: relationships (belongs-to/has-many) are intentionally omitted here;
// wire them up as needed in repository.go queries.
// IdempotencyStatus is a best-guess enum (ERD only specified "enum"; confirm real values).
type IdempotencyStatus string

const (
	IdempotencyStatusPending   IdempotencyStatus = "pending"
	IdempotencyStatusCompleted IdempotencyStatus = "completed"
	IdempotencyStatusFailed    IdempotencyStatus = "failed"
)

// SyncConflictReason is a best-guess enum (ERD only specified "enum"; confirm real values).
type SyncConflictReason string

const (
	SyncConflictReasonVersionMismatch SyncConflictReason = "version_mismatch"
	SyncConflictReasonDuplicate       SyncConflictReason = "duplicate"
	SyncConflictReasonValidationError SyncConflictReason = "validation_error"
	SyncConflictReasonOther           SyncConflictReason = "other"
)

// IdempotencyKey maps to the "Idempotency_keys" table in the ERD.
type IdempotencyKey struct {
	Key         uuid.UUID         `gorm:"primaryKey;type:uuid" json:"key"`
	OrgID       uint              `gorm:"index;not null" json:"org_id"`
	Endpoint    string            `gorm:"size:255;not null" json:"endpoint"`
	RequestHash string            `gorm:"size:64;not null" json:"request_hash"`
	Response    datatypes.JSON    `gorm:"type:json;not null" json:"response"`
	Status      IdempotencyStatus `gorm:"type:varchar(30);not null" json:"status"` // one of IdempotencyStatus* constants below (TODO: confirm real values)
	CreatedAt   time.Time         `gorm:"autoCreateTime;not null" json:"created_at"`
}

// SyncConflict maps to the "Sync_conflicts" table in the ERD.
type SyncConflict struct {
	ID         uint               `gorm:"primaryKey;autoIncrement" json:"id"`
	SaleID     uuid.UUID          `gorm:"type:uuid;index;not null" json:"sale_id"`
	Reason     SyncConflictReason `gorm:"type:varchar(30);not null" json:"reason"` // one of SyncConflictReason* constants below (TODO: confirm real values)
	Payload    datatypes.JSON     `gorm:"type:jsonb;not null" json:"payload"`
	ResolvedBy *uint              `gorm:"index" json:"resolved_by"`
	ResolvedAt *time.Time         `json:"resolved_at"`
}
