package sales

import (
	"context"
	"fmt"
	"time"

	"github.com/shopspring/decimal"

	"shagan_pos/internal/catalog"
	"shagan_pos/internal/common"
)

// priceCombos is what makes a combo line trustworthy. The till still expands a
// combo into one line per component (so every line stays an ordinary product
// sale), but a combo_id on its own proves nothing: it is only a label the
// client typed. So for every distinct combo_id in the request, once:
//
//   - the combo must exist in this org (404 otherwise: another org's combo
//     looks the same as a missing one),
//   - it must not have expired as of `at` (409; a queued sale is judged at the
//     time it was rung up),
//   - the lines carrying that combo_id must be exactly its components - one
//     line per component product, none missing, none extra, none twice - each
//     with qty = the component's qty x k, the SAME whole k >= 1 for all of them
//     (400 otherwise), and
//   - the bundle's discount is computed HERE from the combo's price, never read
//     from the client (see allocateBundleDiscount).
//
// Without this a cashier could ring any combo_id with a "discount" equal to
// the price and walk out with a free sale that reports book as a bundle sale.
//
// For an offline-queued sale (queued) the sale already happened at the till,
// so what is FAKE is refused (unknown combo, lines that don't match it, a
// combo that had expired when the sale was rung up: a per-item 400 for the
// ingest result) while what is merely STALE is tolerated: the bundle is priced
// at the combo's current price and, if that differs from what the till
// charged, the difference is reported on the first line (comboMismatch) for
// datasync to raise as a price_mismatch conflict instead of rejecting.
func (s *Service) priceCombos(ctx context.Context, orgID uint, in CreateSaleRequest, lines []pricedLine, queued bool, at time.Time) error {
	var comboIDs []uint
	linesOf := make(map[uint][]int)
	for i, item := range in.Items {
		if item.ComboID == nil {
			continue
		}
		id := *item.ComboID
		if _, seen := linesOf[id]; !seen {
			comboIDs = append(comboIDs, id)
		}
		linesOf[id] = append(linesOf[id], i)
	}
	if len(comboIDs) == 0 {
		return nil
	}

	combos := make(map[uint]*catalog.Combo, len(comboIDs))
	for _, id := range comboIDs {
		combo, err := s.combos.GetCombo(ctx, orgID, id)
		if err != nil {
			if queued && isNotFound(err) {
				return common.BadRequestError(fmt.Sprintf("combo %d does not exist", id))
			}
			return err
		}
		combos[id] = combo
	}
	// One query for every combo's components, not one per combo.
	components, err := s.combos.ListComboItemsByComboIDs(ctx, comboIDs)
	if err != nil {
		return err
	}
	componentsOf := make(map[uint][]catalog.ComboItem, len(comboIDs))
	for _, c := range components {
		componentsOf[c.ComboID] = append(componentsOf[c.ComboID], c)
	}

	for _, id := range comboIDs {
		if err := priceCombo(combos[id], componentsOf[id], linesOf[id], in, lines, queued, at); err != nil {
			return err
		}
	}
	return nil
}

// priceCombo validates and prices the lines (indexes into in.Items/lines) of
// one combo. See priceCombos.
func priceCombo(combo *catalog.Combo, components []catalog.ComboItem, idx []int, in CreateSaleRequest, lines []pricedLine, queued bool, at time.Time) error {
	if at.After(combo.ExpiresAt) {
		if queued {
			return common.BadRequestError(fmt.Sprintf("combo %d had expired when the sale was rung up", combo.ID))
		}
		return common.ConflictError("combo has expired")
	}

	perBundle := make(map[uint]int, len(components))
	for _, c := range components {
		perBundle[c.ProductID] = c.Qty
	}
	mismatch := common.BadRequestError(fmt.Sprintf("combo %d: quantities don't match the bundle", combo.ID))

	seen := make(map[uint]bool, len(idx))
	bundles := 0
	for _, i := range idx {
		item := in.Items[i]
		qty, ok := perBundle[item.ProductID]
		if !ok {
			return common.BadRequestError(fmt.Sprintf("combo %d: product %d is not part of the bundle", combo.ID, item.ProductID))
		}
		if seen[item.ProductID] {
			return common.BadRequestError(fmt.Sprintf("combo %d: product %d is listed more than once", combo.ID, item.ProductID))
		}
		seen[item.ProductID] = true
		if qty <= 0 || item.Qty%qty != 0 {
			return mismatch
		}
		// The same whole number of bundles on every component.
		if k := item.Qty / qty; bundles == 0 {
			bundles = k
		} else if k != bundles {
			return mismatch
		}
	}
	for _, c := range components {
		if !seen[c.ProductID] {
			return common.BadRequestError(fmt.Sprintf("combo %d: line for product %d is missing", combo.ID, c.ProductID))
		}
	}

	normal := make([]decimal.Decimal, len(idx))
	for j, i := range idx {
		normal[j] = lines[i].item.UnitPrice.Mul(decimal.NewFromInt(int64(lines[i].item.Qty)))
	}
	bundlePrice := combo.Price.Mul(decimal.NewFromInt(int64(bundles)))
	shares := allocateBundleDiscount(normal, bundlePrice)

	serverTotal, clientTotal := decimal.Zero, decimal.Zero
	for j, i := range idx {
		line := &lines[i].item
		line.ComboDiscount = shares[j]
		line.Discount = line.CatalogDiscount.Add(shares[j])
		line.LineTotal = normal[j].Sub(line.Discount)
		serverTotal = serverTotal.Add(line.LineTotal)
		clientTotal = clientTotal.Add(lines[i].clientCharged)
	}

	if queued && !clientTotal.Equal(serverTotal) {
		comboID := combo.ID
		lines[idx[0]].comboMismatch = &PriceMismatch{
			ComboID: &comboID, ClientBundleTotal: &clientTotal, CurrentBundleTotal: &serverTotal,
		}
		lines[idx[0]].tillDelta = clientTotal.Sub(serverTotal)
	}
	return nil
}

// allocateBundleDiscount splits a bundle's discount across its lines.
// normal[j] is line j's price x qty at normal prices; bundlePrice is what the
// whole bundle (all k of it) should cost.
//
// The discount is normal total - bundle price, floored at zero: a combo can
// never RAISE a price. It is shared in proportion to each line's normal total,
// each share rounded to 2 decimals, and the rounding remainder goes to the
// line with the largest normal total (the first on a tie) so the lines add up
// to exactly the bundle price - the till's old rule, now enforced here.
func allocateBundleDiscount(normal []decimal.Decimal, bundlePrice decimal.Decimal) []decimal.Decimal {
	shares := make([]decimal.Decimal, len(normal))
	total := decimal.Zero
	for _, n := range normal {
		total = total.Add(n)
	}
	discount := total.Sub(bundlePrice)
	if !discount.IsPositive() {
		return shares
	}
	assigned, largest := decimal.Zero, 0
	for j, n := range normal {
		shares[j] = discount.Mul(n).DivRound(total, 2)
		assigned = assigned.Add(shares[j])
		if n.GreaterThan(normal[largest]) {
			largest = j
		}
	}
	shares[largest] = shares[largest].Add(discount.Sub(assigned))
	return shares
}
