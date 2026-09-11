package sales

import "gorm.io/gorm"

// TODO: fields are stubbed pending the ERD field-level pass (see shagan-pos-erd.drawio).

type Sale struct {
	gorm.Model
}

type SaleItem struct {
	gorm.Model
}

type Payment struct {
	gorm.Model
}

type HeldSale struct {
	gorm.Model
}
