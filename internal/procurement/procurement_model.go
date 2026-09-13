package procurement

import (
	"time"

	"github.com/shopspring/decimal"
)

// TODO: relationships (belongs-to/has-many) are intentionally omitted here;
// wire them up as needed in repository.go queries.
// PurchaseOrderStatus is a best-guess enum (ERD only specified "enum"; confirm real values).
type PurchaseOrderStatus string

const (
	PurchaseOrderStatusDraft     PurchaseOrderStatus = "draft"
	PurchaseOrderStatusSubmitted PurchaseOrderStatus = "submitted"
	PurchaseOrderStatusApproved  PurchaseOrderStatus = "approved"
	PurchaseOrderStatusReceived  PurchaseOrderStatus = "received"
	PurchaseOrderStatusCancelled PurchaseOrderStatus = "cancelled"
)

// Supplier maps to the "Suppliers" table in the ERD.
type Supplier struct {
	ID          uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	OrgID       uint      `gorm:"index;not null" json:"org_id"`
	Name        string    `gorm:"size:255;not null" json:"name"`
	Phone       string    `gorm:"size:25;not null" json:"phone"`
	Address     string    `gorm:"size:255;not null" json:"address"`
	LastOrderAt time.Time `gorm:"not null" json:"last_order_at"`
}

// PurchaseOrder maps to the "Purchase_orders" table in the ERD.
type PurchaseOrder struct {
	ID         uint                `gorm:"primaryKey;autoIncrement" json:"id"`
	PoNumber   string              `gorm:"size:50;uniqueIndex:ux_purchase_orders_org_po_number;not null" json:"po_number"` // TODO: ERD has no org_id on this table, so this is only globally unique, not per-org
	SupplierID uint                `gorm:"index;not null" json:"supplier_id"`
	Status     PurchaseOrderStatus `gorm:"type:varchar(30);not null" json:"status"` // one of PurchaseOrderStatus* constants below (TODO: confirm real values)
	Total      decimal.Decimal     `gorm:"type:decimal(10,2);not null" json:"total"`
	CreatedBy  uint                `gorm:"index;not null" json:"created_by"`
	CreatedAt  time.Time           `gorm:"autoCreateTime;not null" json:"created_at"`
}

// PurchaseOrderItem maps to the "Purchase_order_items" table in the ERD.
type PurchaseOrderItem struct {
	ID         uint            `gorm:"primaryKey;autoIncrement" json:"id"`
	PoID       uint            `gorm:"index;not null" json:"po_id"`
	ProductID  uint            `gorm:"index;not null" json:"product_id"`
	OrderedQty int             `gorm:"not null" json:"ordered_qty"`
	UnitCost   decimal.Decimal `gorm:"type:decimal(10,2);not null" json:"unit_cost"`
}

// GoodsReceipt maps to the "Goods_receipts" table in the ERD.
type GoodsReceipt struct {
	ID            uint            `gorm:"primaryKey;autoIncrement" json:"id"`
	PoID          uint            `gorm:"index;not null" json:"po_id"`
	ReceivedBy    uint            `gorm:"index;not null" json:"received_by"`
	ReceivedAt    time.Time       `gorm:"not null" json:"received_at"`
	TotalAmount   decimal.Decimal `gorm:"type:decimal(10,2);not null" json:"total_amount"`
	VarianceCount int             `gorm:"not null" json:"variance_count"`
}

// GoodsReceiptItem maps to the "Goods_receipts_items" table in the ERD.
type GoodsReceiptItem struct {
	ID           uint            `gorm:"primaryKey;autoIncrement" json:"id"`
	ReceiptID    uint            `gorm:"index;not null" json:"receipt_id"`
	PoItemID     uint            `gorm:"index;not null" json:"po_item_id"`
	ReceivedQty  int             `gorm:"not null" json:"received_qty"`
	UnitCost     decimal.Decimal `gorm:"type:decimal(10,2);not null" json:"unit_cost"`
	VarianceNote string          `gorm:"size:255;not null" json:"variance_note"`
}

func (GoodsReceiptItem) TableName() string { return "goods_receipts_items" }
