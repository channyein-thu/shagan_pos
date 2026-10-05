package procurement

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"shagan_pos/internal/common"
	"shagan_pos/internal/inventory"
)

type Service struct {
	repo     Repository
	branches BranchLookup
	products ProductLookup
	stock    InventoryWriter
	db       common.Transactioner
}

func NewService(repo Repository, branches BranchLookup, products ProductLookup, stock InventoryWriter, db common.Transactioner) *Service {
	return &Service{repo: repo, branches: branches, products: products, stock: stock, db: db}
}

var _ Interface = (*Service)(nil)

func (s *Service) ListSuppliers(ctx context.Context, orgID uint) ([]Supplier, error) {
	return s.repo.ListSuppliers(ctx, orgID)
}

// CreateSupplier stamps LastOrderAt at creation time (no order has been
// placed yet, but the column is not-null) - it's updated to a real value
// later whenever an actual PurchaseOrder is placed, same reasoning as
// identity.CreateDevice's LastSeenAt.
func (s *Service) CreateSupplier(ctx context.Context, orgID uint, in CreateSupplierRequest) (*Supplier, error) {
	supplier := Supplier{
		OrgID:       orgID,
		Name:        in.Name,
		Phone:       in.Phone,
		Address:     in.Address,
		LastOrderAt: time.Now(),
	}
	if err := s.repo.CreateSupplier(ctx, &supplier); err != nil {
		return nil, err
	}
	return &supplier, nil
}

func (s *Service) UpdateSupplier(ctx context.Context, orgID uint, id uint, in UpdateSupplierRequest) (*Supplier, error) {
	if _, err := s.repo.GetSupplier(ctx, orgID, id); err != nil {
		return nil, err
	}

	updates := map[string]any{}
	if in.Name != nil {
		updates["name"] = *in.Name
	}
	if in.Phone != nil {
		updates["phone"] = *in.Phone
	}
	if in.Address != nil {
		updates["address"] = *in.Address
	}

	if len(updates) > 0 {
		if err := s.repo.UpdateSupplier(ctx, id, updates); err != nil {
			return nil, err
		}
	}

	return s.repo.GetSupplier(ctx, orgID, id)
}

func (s *Service) DeleteSupplier(ctx context.Context, orgID uint, id uint) error {
	if _, err := s.repo.GetSupplier(ctx, orgID, id); err != nil {
		return err
	}

	inUse, err := s.repo.PurchaseOrdersExistForSupplier(ctx, id)
	if err != nil {
		return err
	}
	if inUse {
		return common.ConflictError("supplier is still in use by one or more purchase orders")
	}

	return s.repo.DeleteSupplier(ctx, id)
}

func (s *Service) ListPurchaseOrders(ctx context.Context, orgID uint) ([]PurchaseOrder, error) {
	return s.repo.ListPurchaseOrders(ctx, orgID)
}

// CreatePurchaseOrder confirms in.BranchID, in.SupplierID, and every item's
// ProductID belong to orgID, validates each item's UnitCost is actually
// positive (decimal.Decimal's zero value can't do that from a struct tag -
// see CreatePurchaseOrderItemRequest's doc), computes Total from the items
// and PoNumber from orgID's existing purchase-order count (see Service's
// interface doc), then creates the PurchaseOrder and its PurchaseOrderItems
// together as one
// atomic unit of work.
func (s *Service) CreatePurchaseOrder(ctx context.Context, orgID uint, createdBy uint, in CreatePurchaseOrderRequest) (*PurchaseOrder, error) {
	if _, err := s.branches.GetBranch(ctx, orgID, in.BranchID); err != nil {
		return nil, err
	}
	if _, err := s.repo.GetSupplier(ctx, orgID, in.SupplierID); err != nil {
		return nil, err
	}

	total := decimal.Zero
	for _, item := range in.Items {
		if !item.UnitCost.IsPositive() {
			return nil, common.BadRequestError("unit_cost must be greater than zero")
		}
		if _, err := s.products.GetProduct(ctx, orgID, item.ProductID); err != nil {
			return nil, err
		}
		total = total.Add(item.UnitCost.Mul(decimal.NewFromInt(int64(item.OrderedQty))))
	}

	var result *PurchaseOrder
	err := s.db.Transaction(func(tx *gorm.DB) error {
		count, err := s.repo.CountPurchaseOrders(tx, orgID)
		if err != nil {
			return err
		}
		po := PurchaseOrder{
			OrgID:      orgID,
			BranchID:   in.BranchID,
			PoNumber:   fmt.Sprintf("PO-%04d", count+1),
			SupplierID: in.SupplierID,
			Status:     PurchaseOrderStatusSubmitted,
			Total:      total,
			CreatedBy:  createdBy,
		}
		if err := s.repo.CreatePurchaseOrder(tx, &po); err != nil {
			if common.IsDuplicateError(err) {
				return common.ConflictError("a purchase order number collided with an existing one - please retry")
			}
			return err
		}

		items := make([]PurchaseOrderItem, len(in.Items))
		for i, item := range in.Items {
			items[i] = PurchaseOrderItem{
				PoID:       po.ID,
				ProductID:  item.ProductID,
				OrderedQty: item.OrderedQty,
				UnitCost:   item.UnitCost,
			}
		}
		if err := s.repo.CreatePurchaseOrderItems(tx, items); err != nil {
			return err
		}

		result = &po
		return nil
	})
	if err != nil {
		return nil, err
	}

	return result, nil
}

func (s *Service) GetPurchaseOrder(ctx context.Context, orgID uint, id uint) (*PurchaseOrderResult, error) {
	po, err := s.repo.GetPurchaseOrder(ctx, orgID, id)
	if err != nil {
		return nil, err
	}
	items, err := s.repo.ListPurchaseOrderItemsByPoID(ctx, po.ID)
	if err != nil {
		return nil, err
	}
	return &PurchaseOrderResult{PurchaseOrder: *po, Items: items}, nil
}

// UpdatePurchaseOrder confirms the order exists AND belongs to orgID, and
// blocks any update at all once it's already Received or Cancelled
// (terminal states). Status can move to any value except
// PurchaseOrderStatusReceived - see UpdatePurchaseOrderRequest's doc.
func (s *Service) UpdatePurchaseOrder(ctx context.Context, orgID uint, id uint, in UpdatePurchaseOrderRequest) (*PurchaseOrder, error) {
	existing, err := s.repo.GetPurchaseOrder(ctx, orgID, id)
	if err != nil {
		return nil, err
	}
	if existing.Status == PurchaseOrderStatusReceived || existing.Status == PurchaseOrderStatusCancelled {
		return nil, common.ConflictError("a received or cancelled purchase order can no longer be updated")
	}
	if in.Status != nil && *in.Status == PurchaseOrderStatusReceived {
		return nil, common.BadRequestError("use POST /purchase-orders/:id/receipts to mark a purchase order received")
	}
	if in.SupplierID != nil {
		if _, err := s.repo.GetSupplier(ctx, orgID, *in.SupplierID); err != nil {
			return nil, err
		}
	}

	updates := map[string]any{}
	if in.SupplierID != nil {
		updates["supplier_id"] = *in.SupplierID
	}
	if in.Status != nil {
		updates["status"] = *in.Status
	}

	if len(updates) > 0 {
		if err := s.repo.UpdatePurchaseOrder(ctx, id, updates); err != nil {
			return nil, err
		}
	}

	return s.repo.GetPurchaseOrder(ctx, orgID, id)
}

// CreateGoodsReceipt confirms the purchase order exists AND belongs to
// orgID and hasn't already been Received or Cancelled, resolves each
// requested item against its PurchaseOrderItem (validating po_item_id
// actually belongs to this order, and requiring variance_note whenever
// received_qty differs from ordered_qty), then atomically: creates the
// GoodsReceipt and its GoodsReceiptItems, credits each product's StockLevel
// at the purchase order's own BranchID (products are org-wide, see
// catalog.Product's doc, so the order itself is what carries the branch
// now) by exactly received_qty, appends one InventoryLedger entry per item
// reflecting that same real movement, and marks the purchase order
// Received.
func (s *Service) CreateGoodsReceipt(ctx context.Context, orgID uint, poID uint, receivedBy uint, in CreateGoodsReceiptRequest) (*GoodsReceipt, error) {
	po, err := s.repo.GetPurchaseOrder(ctx, orgID, poID)
	if err != nil {
		return nil, err
	}
	if po.Status == PurchaseOrderStatusReceived {
		return nil, common.ConflictError("this purchase order has already been received")
	}
	if po.Status == PurchaseOrderStatusCancelled {
		return nil, common.ConflictError("a cancelled purchase order cannot be received")
	}

	poItems, err := s.repo.ListPurchaseOrderItemsByPoID(ctx, poID)
	if err != nil {
		return nil, err
	}
	poItemsByID := make(map[uint]PurchaseOrderItem, len(poItems))
	for _, item := range poItems {
		poItemsByID[item.ID] = item
	}

	type resolvedItem struct {
		req             CreateGoodsReceiptItemRequest
		poItem          PurchaseOrderItem
		costPriceBefore decimal.Decimal
	}
	resolved := make([]resolvedItem, len(in.Items))
	total := decimal.Zero
	varianceCount := 0
	for i, item := range in.Items {
		if item.ReceivedQty < 0 {
			return nil, common.BadRequestError("received_qty must not be negative")
		}
		poItem, ok := poItemsByID[item.PoItemID]
		if !ok {
			return nil, common.NotFoundError("purchase order item not found")
		}
		if item.ReceivedQty != poItem.OrderedQty && item.VarianceNote == "" {
			return nil, common.BadRequestError("variance_note is required when received_qty differs from ordered_qty")
		}
		product, err := s.products.GetProduct(ctx, orgID, poItem.ProductID)
		if err != nil {
			return nil, err
		}

		total = total.Add(poItem.UnitCost.Mul(decimal.NewFromInt(int64(item.ReceivedQty))))
		if shortfall := poItem.OrderedQty - item.ReceivedQty; shortfall > 0 {
			varianceCount += shortfall
		}
		resolved[i] = resolvedItem{req: item, poItem: poItem, costPriceBefore: product.CostPrice}
	}

	var result *GoodsReceipt
	err = s.db.Transaction(func(tx *gorm.DB) error {
		receipt := GoodsReceipt{
			PoID:          poID,
			ReceivedBy:    receivedBy,
			ReceivedAt:    in.ReceivedAt,
			TotalAmount:   total,
			VarianceCount: varianceCount,
		}
		if err := s.repo.CreateGoodsReceipt(tx, &receipt); err != nil {
			return err
		}

		receiptItems := make([]GoodsReceiptItem, len(resolved))
		for i, r := range resolved {
			receiptItems[i] = GoodsReceiptItem{
				ReceiptID:    receipt.ID,
				PoItemID:     r.poItem.ID,
				ReceivedQty:  r.req.ReceivedQty,
				UnitCost:     r.poItem.UnitCost,
				VarianceNote: r.req.VarianceNote,
			}
		}
		if err := s.repo.CreateGoodsReceiptItems(tx, receiptItems); err != nil {
			return err
		}

		for _, r := range resolved {
			if r.req.ReceivedQty == 0 {
				// nothing arrived for this line - no stock/ledger movement.
				continue
			}

			level, err := s.stock.GetStockLevel(tx, r.poItem.ProductID, po.BranchID)
			if err != nil {
				return err
			}

			var newQty int
			var currentQty int
			if level == nil {
				newQty = r.req.ReceivedQty
				currentQty = 0
				if err := s.stock.CreateStockLevel(tx, &inventory.StockLevel{
					ProductID: r.poItem.ProductID,
					BranchID:  po.BranchID,
					Qty:       newQty,
				}); err != nil {
					return err
				}
			} else {
				currentQty = level.Qty
				newQty = level.Qty + r.req.ReceivedQty
				if err := s.stock.UpdateStockLevelQty(tx, level.ID, newQty); err != nil {
					return err
				}
			}

			newCost := weightedAverageCost(currentQty, r.costPriceBefore, r.req.ReceivedQty, r.poItem.UnitCost)
			if err := s.products.UpdateProduct(tx, r.poItem.ProductID, map[string]any{"cost_price": newCost}); err != nil {
				return err
			}

			if err := s.stock.CreateInventoryLedgerEntry(tx, &inventory.InventoryLedger{
				OrgID:         orgID,
				ProductID:     r.poItem.ProductID,
				BranchID:      po.BranchID,
				Type:          inventory.LedgerEntryTypePurchaseReceipt,
				Qty:           r.req.ReceivedQty,
				BalanceAfter:  newQty,
				ActorID:       &receivedBy,
				ReferenceType: inventory.ReferenceTypeGoodsReceipt,
				ReferenceID:   strconv.FormatUint(uint64(receipt.ID), 10),
			}); err != nil {
				return err
			}
		}

		if err := s.repo.UpdatePurchaseOrderStatus(tx, poID, PurchaseOrderStatusReceived); err != nil {
			return err
		}

		result = &receipt
		return nil
	})
	if err != nil {
		return nil, err
	}

	return result, nil
}

// weightedAverageCost blends receivedQty units at receivedUnitCost into a
// product's existing cost basis, weighted by currentQty - the product's own
// on-hand qty at this specific branch immediately before this movement (not
// summed across every branch it might also exist at via a transfer - see
// catalog.Product.CostPrice's own doc for why that's an accepted
// simplification). currentQty <= 0 (nothing on hand yet) means there's
// nothing to blend with - the new cost is just receivedUnitCost itself.
// Same small-helper-duplicated-per-domain shape as applyStockDelta - see
// inventory.Service's own copy of this exact function.
func weightedAverageCost(currentQty int, currentCost decimal.Decimal, receivedQty int, receivedUnitCost decimal.Decimal) decimal.Decimal {
	if currentQty <= 0 {
		return receivedUnitCost
	}
	existingValue := currentCost.Mul(decimal.NewFromInt(int64(currentQty)))
	receivedValue := receivedUnitCost.Mul(decimal.NewFromInt(int64(receivedQty)))
	totalQty := decimal.NewFromInt(int64(currentQty + receivedQty))
	return existingValue.Add(receivedValue).Div(totalQty).Round(2)
}
