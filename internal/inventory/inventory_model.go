package inventory

import (
	"time"
)

// TODO: relationships (belongs-to/has-many) are intentionally omitted here;
// wire them up as needed in repository.go queries.
// TransferStatus is a best-guess enum (ERD only specified "enum"; confirm real values).
type TransferStatus string

const (
	TransferStatusPending   TransferStatus = "pending"
	TransferStatusInTransit TransferStatus = "in_transit"
	TransferStatusCompleted TransferStatus = "completed"
	TransferStatusCancelled TransferStatus = "cancelled"
)

// LedgerEntryType is a best-guess enum (ERD only specified "enum"; confirm real values).
type LedgerEntryType string

const (
	LedgerEntryTypeSale            LedgerEntryType = "sale"
	LedgerEntryTypeReturn          LedgerEntryType = "return"
	LedgerEntryTypeVoid            LedgerEntryType = "void"
	LedgerEntryTypeExchangeIn      LedgerEntryType = "exchange_in"
	LedgerEntryTypeExchangeOut     LedgerEntryType = "exchange_out"
	LedgerEntryTypeAdjustment      LedgerEntryType = "adjustment"
	LedgerEntryTypeTransferIn      LedgerEntryType = "transfer_in"
	LedgerEntryTypeTransferOut     LedgerEntryType = "transfer_out"
	LedgerEntryTypePurchaseReceipt LedgerEntryType = "purchase_receipt"
	// LedgerEntryTypeReturnWriteoff records goods that came back through a
	// Return or an Exchange but were not restocked (any condition but
	// sellable). Qty is always 0 - stock did not move - and BalanceAfter is the
	// balance as it stands, so the movement is visible in the ledger without
	// changing it.
	LedgerEntryTypeReturnWriteoff LedgerEntryType = "return_writeoff"
)

// ReferenceType is a best-guess enum (ERD only specified "enum"; confirm real values).
type ReferenceType string

const (
	ReferenceTypeSale          ReferenceType = "sale"
	ReferenceTypeReturn        ReferenceType = "return"
	ReferenceTypeVoid          ReferenceType = "void"
	ReferenceTypeExchange      ReferenceType = "exchange"
	ReferenceTypeAdjustment    ReferenceType = "adjustment"
	ReferenceTypeStockTransfer ReferenceType = "stock_transfer"
	ReferenceTypePurchaseOrder ReferenceType = "purchase_order"
	ReferenceTypeGoodsReceipt  ReferenceType = "goods_receipt"
)

// StockLevel maps to the "Stock_levels" table in the ERD.
type StockLevel struct {
	ID        uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	ProductID uint      `gorm:"index;not null" json:"product_id"`
	BranchID  uint      `gorm:"index;not null" json:"branch_id"`
	Qty       int       `gorm:"not null" json:"qty"`
	UpdatedAt time.Time `gorm:"autoUpdateTime;not null" json:"updated_at"`
}

// StockAdjustment maps to the "Stock_adjustments" table in the ERD.
type StockAdjustment struct {
	ID        uint   `gorm:"primaryKey;autoIncrement" json:"id"`
	ProductID uint   `gorm:"index;not null" json:"product_id"`
	BranchID  uint   `gorm:"index;not null" json:"branch_id"`
	Delta     int    `gorm:"not null" json:"delta"`
	Reason    string `gorm:"not null" json:"reason"`
	ActorID   uint   `gorm:"index;not null" json:"actor_id"`
}

// StockTransfer maps to the "Stock_transfers" table in the ERD.
type StockTransfer struct {
	ID         uint           `gorm:"primaryKey;autoIncrement" json:"id"`
	FromBranch uint           `gorm:"index;not null" json:"from_branch"`
	ToBranch   uint           `gorm:"index;not null" json:"to_branch"`
	Status     TransferStatus `gorm:"type:varchar(30);not null" json:"status"` // one of TransferStatus* constants below (TODO: confirm real values)
	ActorID    uint           `gorm:"index;not null" json:"actor_id"`
	// Note is the optional free-text reason the transfer was raised ("" when
	// none was given) - set at creation, never edited.
	Note      string    `gorm:"type:text;not null;default:''" json:"note"`
	CreatedAt time.Time `gorm:"autoCreateTime;not null" json:"created_at"`
}

// StockTransferItem maps to the "Stock_transfers_items" table in the ERD.
type StockTransferItem struct {
	ID         uint `gorm:"primaryKey;autoIncrement" json:"id"`
	TransferID uint `gorm:"index;not null" json:"transfer_id"`
	ProductID  uint `gorm:"index;not null" json:"product_id"`
	Qty        int  `gorm:"not null" json:"qty"`
}

// InventoryLedger maps to the "Inventory_ledger" table in the ERD.
type InventoryLedger struct {
	ID            uint            `gorm:"primaryKey;autoIncrement" json:"id"`
	OrgID         uint            `gorm:"index;not null" json:"org_id"`
	ProductID     uint            `gorm:"index;not null" json:"product_id"`
	BranchID      uint            `gorm:"index;not null" json:"branch_id"`
	Type          LedgerEntryType `gorm:"type:varchar(30);not null" json:"type"` // one of LedgerEntryType* constants below (TODO: confirm real values)
	Qty           int             `gorm:"not null" json:"qty"`
	BalanceAfter  int             `gorm:"not null" json:"balance_after"`
	ActorID       *uint           `gorm:"index" json:"actor_id"`
	ReferenceType ReferenceType   `gorm:"type:varchar(30);not null" json:"reference_type"` // one of ReferenceType* constants below (TODO: confirm real values)
	// ReferenceID is polymorphic, paired with ReferenceType, not a DB-level
	// FK - a uint-keyed reference (adjustment/stock_transfer/purchase_order/
	// goods_receipt) is its decimal string; a sale reference is Sale.ID's
	// UUID string (Sale is the one referenced record that isn't uint-keyed).
	ReferenceID string `gorm:"type:text;not null" json:"reference_id"`
	CreatedAt     time.Time       `gorm:"autoCreateTime;not null" json:"created_at"`
}

func (StockTransferItem) TableName() string { return "stock_transfers_items" }

func (InventoryLedger) TableName() string { return "inventory_ledger" }
