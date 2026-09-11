package platform

import "gorm.io/gorm"

// TODO: fields are stubbed pending the ERD field-level pass (see shagan-pos-erd.drawio).

type ReceiptSetting struct {
	gorm.Model
}

type Locale struct {
	gorm.Model
}
