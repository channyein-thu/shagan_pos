package sales

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"shagan_pos/internal/audit"
	"shagan_pos/internal/catalog"
	"shagan_pos/internal/common"
)

var hundred = decimal.NewFromInt(100)

// priceLines prices every request line from its product - see
// CreateSaleItemRequest for the rules - and validates the discount approvals
// the lines carry. Nothing it returns has touched the database.
func (s *Service) priceLines(ctx context.Context, orgID, branchID uint, actor SaleActor, in CreateSaleRequest, queued bool) ([]pricedLine, error) {
	products := make(map[uint]*catalog.Product, len(in.Items))
	lines := make([]pricedLine, len(in.Items))
	var approvalIDs []uuid.UUID
	seenApproval := make(map[uuid.UUID]bool)

	for i, item := range in.Items {
		if item.PriceOverride != nil {
			return nil, common.BadRequestError("price override is not allowed")
		}
		if item.Discount.IsNegative() {
			return nil, common.BadRequestError("item discount must be zero or greater")
		}
		isCombo := item.ComboID != nil
		if item.DiscountApprovalID != nil {
			if queued {
				return nil, common.BadRequestError("custom discounts need approval and can't be queued offline")
			}
			if isCombo {
				return nil, common.BadRequestError("a combo line can't carry a custom discount")
			}
			if seenApproval[*item.DiscountApprovalID] {
				return nil, common.ConflictError("the same discount approval can't be used on two lines")
			}
			seenApproval[*item.DiscountApprovalID] = true
			approvalIDs = append(approvalIDs, *item.DiscountApprovalID)
		}

		product, ok := products[item.ProductID]
		if !ok {
			var err error
			if product, err = s.products.GetProduct(ctx, orgID, item.ProductID); err != nil {
				return nil, err
			}
			products[item.ProductID] = product
		}

		qty := decimal.NewFromInt(int64(item.Qty))
		original := product.Price
		line := SaleItem{
			SaleID: in.ID, ProductID: item.ProductID, ComboID: item.ComboID, NameSnapshot: item.NameSnapshot,
			Qty: item.Qty, UnitCost: product.CostPrice,
		}
		var mismatch *PriceMismatch
		unitSale := original
		// The tax rate printed on the product. Only an offline-queued line may
		// bring its own (the till's cached one).
		taxPct := product.TaxPercent

		switch {
		case queued:
			// Priced from the till's cached catalog: take its snapshot as
			// rung up, but flag it if the product has since changed.
			if item.UnitPrice.IsPositive() {
				original = item.UnitPrice
			}
			pct := decimal.Zero
			if item.CatalogDiscountPercent != nil {
				pct = *item.CatalogDiscountPercent
			}
			if pct.IsNegative() || pct.GreaterThan(hundred) {
				return nil, common.BadRequestError("catalog_discount_percent must be between 0 and 100")
			}
			if !isCombo {
				line.CatalogDiscountPercent = pct
				unitSale = common.DiscountedUnitPrice(original, pct)
			}
			if item.TaxPercent != nil {
				taxPct = *item.TaxPercent
			}
			if taxPct.IsNegative() || taxPct.GreaterThan(hundred) {
				return nil, common.BadRequestError("tax_percent must be between 0 and 100")
			}
			if !original.Equal(product.Price) || !pct.Equal(product.DiscountPercent) || !taxPct.Equal(product.TaxPercent) {
				mismatch = &PriceMismatch{
					ProductID: item.ProductID, ClientUnitPrice: original, ClientPercent: pct, ClientTaxPercent: taxPct,
					CurrentUnitPrice: product.Price, CurrentPercent: product.DiscountPercent, CurrentTaxPercent: product.TaxPercent,
				}
			}
		default:
			if item.UnitPrice.IsPositive() && !item.UnitPrice.Equal(original) {
				return nil, common.ConflictError("price changed, refresh the catalog")
			}
			if !isCombo && item.DiscountApprovalID == nil {
				unitSale = product.EffectiveSalePrice()
				line.CatalogDiscountPercent = product.DiscountPercent
			}
		}
		if unitSale.GreaterThan(original) {
			unitSale = original
		}

		line.UnitPrice = original
		gross := original.Mul(qty)
		if isCombo {
			// The client's bundle share, validated only for sanity: it can't
			// take more off the line than the line is worth.
			if item.Discount.GreaterThan(gross) {
				return nil, common.BadRequestError("combo discount can't exceed the line's price")
			}
			line.ComboDiscount = item.Discount
		} else if item.DiscountApprovalID == nil {
			line.CatalogDiscount = original.Sub(unitSale).Mul(qty)
		}
		line.Discount = line.CatalogDiscount.Add(line.ComboDiscount)
		line.LineTotal = gross.Sub(line.Discount)
		line.TaxPercent = taxPct
		lines[i] = pricedLine{item: line, mismatch: mismatch}
	}

	if len(approvalIDs) > 0 {
		if err := s.applyApprovals(ctx, orgID, branchID, actor, in, lines, approvalIDs); err != nil {
			return nil, err
		}
	}
	// Tax comes last, after every discount (catalog, custom, combo) is known:
	// it is the tax inside what the customer actually pays for the line.
	for i := range lines {
		lines[i].item.Tax = common.IncludedTax(lines[i].item.LineTotal, lines[i].item.TaxPercent)
	}
	return lines, nil
}

// applyApprovals validates every discount approval the request's lines carry
// and, for each, replaces that line's catalog discount with the approval's %
// (taken from the ORIGINAL price).
func (s *Service) applyApprovals(ctx context.Context, orgID, branchID uint, actor SaleActor, in CreateSaleRequest, lines []pricedLine, approvalIDs []uuid.UUID) error {
	approvals, err := s.repo.ListDiscountApprovals(ctx, orgID, approvalIDs)
	if err != nil {
		return err
	}
	byID := make(map[uuid.UUID]*DiscountApproval, len(approvals))
	for i := range approvals {
		byID[approvals[i].ID] = &approvals[i]
	}
	now := time.Now()
	for i, item := range in.Items {
		if item.DiscountApprovalID == nil {
			continue
		}
		approval := byID[*item.DiscountApprovalID]
		if err := checkApproval(approval, branchID, in.ShiftID, actor.StaffID, item, now); err != nil {
			return err
		}
		line := &lines[i].item
		qty := decimal.NewFromInt(int64(item.Qty))
		unitSale := common.DiscountedUnitPrice(line.UnitPrice, approval.Percent)
		line.ManualDiscountPercent = approval.Percent
		line.ManualDiscount = line.UnitPrice.Sub(unitSale).Mul(qty)
		line.ManualDiscountReason = approval.Reason
		line.DiscountApprovalID = &approval.ID
		approvedBy := approval.ApprovedBy
		line.ManualApprovedBy = &approvedBy
		line.Discount = line.ManualDiscount
		line.LineTotal = line.UnitPrice.Mul(qty).Sub(line.Discount)
		lines[i].approval = approval
	}
	return nil
}

// checkApproval is every condition a custom discount must meet to be spent on
// a line. They are all 409s with a message the till can show: the request was
// well-formed, but the approval it names can't be used for this.
func checkApproval(a *DiscountApproval, branchID, shiftID, staffID uint, item CreateSaleItemRequest, now time.Time) error {
	switch {
	case a == nil || a.BranchID != branchID:
		return common.ConflictError("discount approval not found for this branch")
	case a.RequestedBy != staffID:
		return common.ConflictError("this discount was approved for a different seller")
	case a.Status == DiscountApprovalUsed:
		return common.ConflictError("this discount approval was already used")
	case a.Status == DiscountApprovalCancelled:
		return common.ConflictError("this discount approval was cancelled")
	case !now.Before(a.ExpiresAt):
		return common.ConflictError("this discount approval has expired")
	case a.ShiftID != shiftID:
		return common.ConflictError("this discount was approved on a different shift")
	case a.ProductID != item.ProductID:
		return common.ConflictError("this discount approval is for a different product")
	case item.Qty > a.MaxQty:
		return common.ConflictError("this discount approval covers " + strconv.Itoa(a.MaxQty) + " unit(s); the line has " + strconv.Itoa(item.Qty))
	}
	return nil
}

func (s *Service) CreateDiscountApproval(ctx context.Context, orgID uint, branchID uint, seller DiscountSeller, in CreateDiscountApprovalRequest) (*DiscountApprovalResult, error) {
	if !slices.Contains(seller.Permissions, "apply_manual_discount") {
		return nil, common.ForbiddenError("Your role can't ask for discounts.")
	}
	if !in.Percent.IsPositive() || in.Percent.GreaterThan(hundred) {
		return nil, common.BadRequestError("percent must be greater than 0 and at most 100")
	}
	if in.Qty <= 0 {
		return nil, common.BadRequestError("qty must be at least 1")
	}
	reason := strings.TrimSpace(in.Reason)
	if utf8.RuneCountInString(reason) > 200 {
		return nil, common.BadRequestError("reason must be at most 200 characters")
	}
	hasApprover := in.ApproverStaffID != 0 || in.Pin != ""
	if hasApprover && (in.ApproverStaffID == 0 || in.Pin == "") {
		return nil, common.BadRequestError("approver_staff_id and pin must be sent together")
	}

	// Cheap, side-effect-free checks first: a PIN attempt counts toward the
	// approver's lockout, so it only happens once everything else is in order.
	shift, err := s.repo.FindOpenShift(ctx, orgID, seller.PosUserID)
	if err != nil {
		return nil, err
	}
	if shift == nil || shift.BranchID != branchID {
		return nil, common.ConflictError("there is no open shift on this till")
	}
	product, err := s.products.GetProduct(ctx, orgID, in.ProductID)
	if err != nil {
		return nil, err
	}

	approverID, approverName := seller.StaffID, ""
	if hasApprover {
		approver, perms, err := s.staff.VerifyApprover(ctx, orgID, branchID, in.ApproverStaffID, in.Pin)
		if err != nil {
			return nil, err
		}
		if !slices.Contains(perms, "approve_manual_discount") {
			return nil, common.ForbiddenError("That staff member can't approve discounts.")
		}
		if approver.ID == seller.StaffID && !slices.Contains(perms, "approve_own_discount") {
			return nil, common.ForbiddenError("This discount needs approval from someone else.")
		}
		approverID, approverName = approver.ID, approver.Name
	} else {
		// self-approval: the seller must be allowed to approve discounts AND
		// to approve their own
		if !slices.Contains(seller.Permissions, "approve_manual_discount") || !slices.Contains(seller.Permissions, "approve_own_discount") {
			return nil, common.ForbiddenError("This discount needs approval from someone else.")
		}
		self, err := s.staff.GetStaff(ctx, orgID, seller.StaffID)
		if err != nil {
			return nil, err
		}
		approverName = self.Name
	}

	approval := DiscountApproval{
		ID: uuid.New(), OrgID: orgID, BranchID: branchID, ShiftID: shift.ID,
		RequestedBy: seller.StaffID, ApprovedBy: approverID, ProductID: product.ID,
		Percent: in.Percent.Round(2), MaxQty: in.Qty, Reason: reason,
		Status: DiscountApprovalApproved, ExpiresAt: time.Now().Add(DiscountApprovalMaxLifetime),
	}
	err = s.db.Transaction(func(tx *gorm.DB) error {
		if err := s.repo.CreateDiscountApproval(tx, &approval); err != nil {
			return err
		}
		return s.audit.CreateAuditLog(tx, &audit.AuditLog{
			OrgID: orgID, ActorID: &seller.StaffID, BranchID: &branchID,
			Entity: "sale", EntityID: approval.ID.String(), Action: "discount_approved",
			Before: audit.ToJSON(nil),
			After: audit.ToJSON(map[string]any{
				"seller": seller.StaffID, "approver": approverID, "product_id": product.ID,
				"percent": approval.Percent, "qty": approval.MaxQty, "reason": reason, "shift_id": shift.ID,
			}),
		})
	})
	if err != nil {
		return nil, err
	}
	return &DiscountApprovalResult{
		ID: approval.ID, ProductID: product.ID, Percent: approval.Percent, MaxQty: approval.MaxQty,
		UnitPrice: product.Price, UnitSalePrice: common.DiscountedUnitPrice(product.Price, approval.Percent),
		ApprovedBy: ApproverRef{ID: approverID, Name: approverName}, ExpiresAt: approval.ExpiresAt,
	}, nil
}

func (s *Service) CancelDiscountApproval(ctx context.Context, orgID uint, staffID uint, id uuid.UUID) error {
	approval, err := s.repo.GetDiscountApproval(ctx, orgID, id)
	if err != nil {
		return err
	}
	// Someone else's approval reads as not found, never as forbidden: a till
	// has no business learning which approval ids exist.
	if approval.RequestedBy != staffID {
		return common.NotFoundError("discount approval not found")
	}
	switch approval.Status {
	case DiscountApprovalUsed:
		return common.ConflictError("this discount approval was already used")
	case DiscountApprovalCancelled:
		return nil
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		cancelled, err := s.repo.CancelDiscountApproval(tx, id)
		if err != nil {
			return err
		}
		if !cancelled {
			// spent by a sale between the read above and this write
			return common.ConflictError("this discount approval was already used")
		}
		return s.audit.CreateAuditLog(tx, &audit.AuditLog{
			OrgID: orgID, ActorID: &staffID, BranchID: &approval.BranchID,
			Entity: "sale", EntityID: id.String(), Action: "discount_cancelled",
			Before: audit.ToJSON(map[string]any{"status": approval.Status}),
			After:  audit.ToJSON(map[string]any{"status": DiscountApprovalCancelled, "product_id": approval.ProductID}),
		})
	})
}
