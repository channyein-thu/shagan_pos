package datasync

import "gorm.io/gorm"

// TODO: fields are stubbed pending the ERD field-level pass (see shagan-pos-erd.drawio).

type IdempotencyKey struct {
	gorm.Model
}

type SyncConflict struct {
	gorm.Model
}
