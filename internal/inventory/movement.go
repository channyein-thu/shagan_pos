package inventory

import (
	"gorm.io/gorm"

	"shagan_pos/internal/common"
)

// MovementStore exposes the persistence operations for one stock movement.
// All operations must use the supplied transaction.
type MovementStore interface {
	GetStockLevel(tx *gorm.DB, productID, branchID uint) (*StockLevel, error)
	CreateStockLevel(tx *gorm.DB, level *StockLevel) error
	UpdateStockLevelQty(tx *gorm.DB, id uint, qty int) error
	CreateInventoryLedgerEntry(tx *gorm.DB, entry *InventoryLedger) error
}

type NegativeStockPolicy uint8

const (
	RejectNegativeStock NegativeStockPolicy = iota
	AllowNegativeStock
)

// ApplyMovement updates stock and records its ledger entry in the caller's
// transaction. The caller must return any error to roll back that transaction.
// entry.Qty is the signed delta; BalanceAfter is always calculated here.
func ApplyMovement(tx *gorm.DB, store MovementStore, entry InventoryLedger, policy NegativeStockPolicy) (int, error) {
	if policy != RejectNegativeStock && policy != AllowNegativeStock {
		return 0, common.BadRequestError("invalid negative stock policy")
	}
	level, err := store.GetStockLevel(tx, entry.ProductID, entry.BranchID)
	if err != nil {
		return 0, err
	}
	current := 0
	if level != nil {
		current = level.Qty
	}
	newQty := current + entry.Qty
	if newQty < 0 && policy == RejectNegativeStock {
		return 0, common.ConflictError("insufficient stock for this movement")
	}
	if level == nil {
		if err := store.CreateStockLevel(tx, &StockLevel{
			ProductID: entry.ProductID, BranchID: entry.BranchID, Qty: newQty,
		}); err != nil {
			return 0, err
		}
	} else if err := store.UpdateStockLevelQty(tx, level.ID, newQty); err != nil {
		return 0, err
	}
	entry.BalanceAfter = newQty
	if err := store.CreateInventoryLedgerEntry(tx, &entry); err != nil {
		return 0, err
	}
	return newQty, nil
}
