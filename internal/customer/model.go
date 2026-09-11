package customer

import "gorm.io/gorm"

// TODO: fields are stubbed pending the ERD field-level pass (see shagan-pos-erd.drawio).

type Customer struct {
	gorm.Model
}

type CustomerConsent struct {
	gorm.Model
}
