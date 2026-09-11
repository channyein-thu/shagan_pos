package procurement

import "gorm.io/gorm"

// TODO: fields are stubbed pending the ERD field-level pass (see shagan-pos-erd.drawio).

type Supplier struct {
	gorm.Model
}

type PurchaseOrder struct {
	gorm.Model
}

type PurchaseOrderItem struct {
	gorm.Model
}

type GoodsReceipt struct {
	gorm.Model
}

type GoodsReceiptItem struct {
	gorm.Model
}
