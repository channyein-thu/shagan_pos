package catalog

import (
	"context"

	"github.com/shopspring/decimal"

	"shagan_pos/internal/common"
)

// Where a price came from - ProductResult.PriceSource and Pricing.Source.
const (
	PriceSourceCatalog = "catalog"
	PriceSourceBranch  = "branch"
)

// Pricing is what a product costs at one branch: the effective selling price,
// catalog discount percent and the resulting per-unit sale price. Every path
// that prices a product - sale, queued sale, discount approval, exchange "out"
// line, product reads, the offline catalog - takes it from here, never from
// Product.Price / DiscountPercent / EffectiveSalePrice directly.
type Pricing struct {
	Price           decimal.Decimal
	DiscountPercent decimal.Decimal
	SalePrice       decimal.Decimal
	// Source is PriceSourceBranch when a branch override row applied (even one
	// that sets only one of the two fields), PriceSourceCatalog otherwise.
	Source string
}

// EffectivePricing is the one resolution rule: the branch override's price and
// percent where set, otherwise the product's, each falling back independently.
// override is nil when the branch has none. Without an override the product's
// own sale price is used (Product.EffectiveSalePrice); with one, the stored
// override sale price, computed when the override was written.
func EffectivePricing(p *Product, override *ProductBranchPrice) Pricing {
	if override == nil {
		return Pricing{Price: p.Price, DiscountPercent: p.DiscountPercent, SalePrice: p.EffectiveSalePrice(), Source: PriceSourceCatalog}
	}
	pr := Pricing{Price: p.Price, DiscountPercent: p.DiscountPercent, SalePrice: override.SalePrice, Source: PriceSourceBranch}
	if override.Price != nil {
		pr.Price = *override.Price
	}
	if override.DiscountPercent != nil {
		pr.DiscountPercent = *override.DiscountPercent
	}
	return pr
}

// BranchPriceReader is the one catalog read the resolvers need: a branch's
// override rows. catalog.Repository satisfies it, so sales/returns get it by
// widening their own ProductLookup with these two methods.
type BranchPriceReader interface {
	// GetBranchPrice returns nil, nil when the branch has no override for the
	// product.
	GetBranchPrice(ctx context.Context, orgID, branchID, productID uint) (*ProductBranchPrice, error)
	ListBranchPricesForBranch(ctx context.Context, orgID, branchID uint, productIDs []uint) ([]ProductBranchPrice, error)
}

// ResolveBranchPricing is the single-product form of the resolver: what p
// costs at branchID in orgID.
func ResolveBranchPricing(ctx context.Context, r BranchPriceReader, orgID, branchID uint, p *Product) (Pricing, error) {
	override, err := r.GetBranchPrice(ctx, orgID, branchID, p.ID)
	if err != nil {
		return Pricing{}, err
	}
	return EffectivePricing(p, override), nil
}

// ResolveBranchPricingBatch is the list form: one query for every product's
// override at branchID, result i belonging to products[i].
func ResolveBranchPricingBatch(ctx context.Context, r BranchPriceReader, orgID, branchID uint, products []Product) ([]Pricing, error) {
	out := make([]Pricing, len(products))
	if len(products) == 0 {
		return out, nil
	}
	ids := make([]uint, len(products))
	for i := range products {
		ids[i] = products[i].ID
	}
	rows, err := r.ListBranchPricesForBranch(ctx, orgID, branchID, ids)
	if err != nil {
		return nil, err
	}
	byProduct := make(map[uint]*ProductBranchPrice, len(rows))
	for i := range rows {
		byProduct[rows[i].ProductID] = &rows[i]
	}
	for i := range products {
		out[i] = EffectivePricing(&products[i], byProduct[products[i].ID])
	}
	return out, nil
}

// applyPricing rewrites a product's price fields to what it costs at a branch,
// keeping the derived per-unit discount and included tax consistent with them.
// A catalog-sourced result leaves the product untouched.
func applyPricing(p *Product, pr Pricing) {
	if pr.Source != PriceSourceBranch {
		return
	}
	p.Price = pr.Price
	p.DiscountPercent = pr.DiscountPercent
	p.SalePrice = pr.SalePrice
	p.Discount = pr.Price.Sub(pr.SalePrice)
	p.Tax = common.IncludedTax(pr.SalePrice, p.TaxPercent)
}

// overrideSalePrice computes the stored sale price of an override: the same
// rule and rounding as a product's (productPricing), from the override's own
// price and percent where set and the product's otherwise.
func overrideSalePrice(p *Product, price, percent *decimal.Decimal) decimal.Decimal {
	effPrice, effPercent := p.Price, p.DiscountPercent
	if price != nil {
		effPrice = *price
	}
	if percent != nil {
		effPercent = *percent
	}
	_, salePrice, _ := productPricing(effPrice, effPercent)
	return salePrice
}
