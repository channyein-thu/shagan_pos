package shift

import "gorm.io/gorm"

// TODO: fields are stubbed pending the ERD field-level pass (see shagan-pos-erd.drawio).

type Shift struct {
	gorm.Model
}

type ShiftReconciliation struct {
	gorm.Model
}

type DrawerEvent struct {
	gorm.Model
}

type Expense struct {
	gorm.Model
}
