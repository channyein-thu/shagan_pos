package inventory

import "gorm.io/gorm"

// TODO: fields are stubbed pending the ERD field-level pass (see shagan-pos-erd.drawio).

type StockLevel struct {
	gorm.Model
}

type InventoryLedger struct {
	gorm.Model
}

type StockAdjustment struct {
	gorm.Model
}

type StockTransfer struct {
	gorm.Model
}

type StockTransferItem struct {
	gorm.Model
}
