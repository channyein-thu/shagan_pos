package catalog

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"shagan_pos/internal/audit"
	"shagan_pos/internal/common"
	"shagan_pos/internal/identity"
)

func dec(s string) decimal.Decimal { return decimal.RequireFromString(s) }
func decPtr(s string) *decimal.Decimal {
	v := dec(s)
	return &v
}

// A 6,500 product with the catalog's 10% (sale price 5,900 after Kyat rounding).
func pricedProduct() *Product {
	return &Product{
		ID: 1, OrgID: 7, Price: dec("6500"), DiscountPercent: dec("10"),
		SalePrice: common.DiscountedUnitPrice(dec("6500"), dec("10")), TaxPercent: dec("5"),
	}
}

// ---- the resolver: no override / price only / % only / both ----

func TestEffectivePricing(t *testing.T) {
	p := pricedProduct()
	cases := map[string]struct {
		override                   *ProductBranchPrice
		price, percent, sale, from string
	}{
		"no override: the catalog": {nil, "6500", "10", "5900", PriceSourceCatalog},
		"price only: the percent is inherited": {
			&ProductBranchPrice{Price: decPtr("7000"), SalePrice: dec("6300")}, "7000", "10", "6300", PriceSourceBranch},
		"percent only: the price is inherited": {
			&ProductBranchPrice{DiscountPercent: decPtr("20"), SalePrice: dec("5200")}, "6500", "20", "5200", PriceSourceBranch},
		"both": {
			&ProductBranchPrice{Price: decPtr("7000"), DiscountPercent: decPtr("0"), SalePrice: dec("7000")}, "7000", "0", "7000", PriceSourceBranch},
		"a 100% branch discount is free, not 'unpriced'": {
			&ProductBranchPrice{DiscountPercent: decPtr("100"), SalePrice: dec("0")}, "6500", "100", "0", PriceSourceBranch},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			got := EffectivePricing(p, c.override)
			require.True(t, dec(c.price).Equal(got.Price), "price %s", got.Price)
			require.True(t, dec(c.percent).Equal(got.DiscountPercent), "percent %s", got.DiscountPercent)
			require.True(t, dec(c.sale).Equal(got.SalePrice), "sale price %s", got.SalePrice)
			require.Equal(t, c.from, got.Source)
		})
	}
}

func TestEffectivePricing_UnpricedCatalogRow_SellsAtItsPrice(t *testing.T) {
	p := &Product{ID: 1, Price: dec("6500")} // sale_price never stored, no percent
	require.True(t, dec("6500").Equal(EffectivePricing(p, nil).SalePrice))
}

func TestResolveBranchPricingBatch_OneQuery_MatchesEachProductToItsOverride(t *testing.T) {
	repo := NewMockRepository(t)
	products := []Product{*pricedProduct(), {ID: 2, OrgID: 7, Price: dec("1000"), SalePrice: dec("1000")}}
	repo.EXPECT().ListBranchPricesForBranch(mock.Anything, uint(7), uint(3), []uint{1, 2}).
		Return([]ProductBranchPrice{{ProductID: 2, BranchID: 3, Price: decPtr("1200"), SalePrice: dec("1200")}}, nil).Once()

	got, err := ResolveBranchPricingBatch(context.Background(), repo, 7, 3, products)

	require.NoError(t, err)
	require.Equal(t, PriceSourceCatalog, got[0].Source)
	require.True(t, dec("5900").Equal(got[0].SalePrice))
	require.Equal(t, PriceSourceBranch, got[1].Source)
	require.True(t, dec("1200").Equal(got[1].Price))
}

func TestResolveBranchPricing_SingleProduct(t *testing.T) {
	repo := NewMockRepository(t)
	repo.EXPECT().GetBranchPrice(mock.Anything, uint(7), uint(3), uint(1)).
		Return(&ProductBranchPrice{ProductID: 1, BranchID: 3, Price: decPtr("7000"), SalePrice: dec("6300")}, nil).Once()

	got, err := ResolveBranchPricing(context.Background(), repo, 7, 3, pricedProduct())

	require.NoError(t, err)
	require.True(t, dec("7000").Equal(got.Price))
	require.True(t, dec("6300").Equal(got.SalePrice))
}

// ---- product reads for a branch ----

type branchPriceFixture struct {
	svc      *Service
	repo     *MockRepository
	branches *MockBranchLookup
	audW     *MockAuditWriter
}

func newBranchPriceFixture(t *testing.T) *branchPriceFixture {
	t.Helper()
	repo, branches, audW := NewMockRepository(t), NewMockBranchLookup(t), NewMockAuditWriter(t)
	return &branchPriceFixture{svc: NewService(repo, branches, audW, fakeTransactioner{}, NewMockStorage(t)), repo: repo, branches: branches, audW: audW}
}

func uintPtr(v uint) *uint { return &v }

// The Owner (user 21, org-wide) and a manager (staff 14) at the till of branch 3.
var (
	ownerActor   = BranchPriceActor{UserID: uintPtr(21)}
	managerActor = BranchPriceActor{StaffID: uintPtr(14), BranchID: uintPtr(3)}
)

func branch(id uint, name string, status identity.BranchStatus) *identity.Branch {
	return &identity.Branch{ID: id, OrgID: 7, Name: name, Status: status}
}

func TestService_GetProduct_ForABranch_ReturnsItsEffectivePricesAndSource(t *testing.T) {
	fx := newBranchPriceFixture(t)
	b := uint(3)
	fx.repo.EXPECT().GetProduct(mock.Anything, uint(7), uint(1)).Return(pricedProduct(), nil).Once()
	fx.repo.EXPECT().ListProductImagesByProductIDs(mock.Anything, []uint{1}).Return(nil, nil).Once()
	fx.branches.EXPECT().GetBranch(mock.Anything, uint(7), uint(3)).Return(branch(3, "Mandalay", identity.BranchStatusActive), nil).Once()
	fx.repo.EXPECT().ListBranchPricesForBranch(mock.Anything, uint(7), uint(3), []uint{1}).
		Return([]ProductBranchPrice{{ProductID: 1, BranchID: 3, Price: decPtr("7000"), SalePrice: dec("6300")}}, nil).Once()

	got, err := fx.svc.GetProduct(context.Background(), 7, 1, &b)

	require.NoError(t, err)
	require.Equal(t, PriceSourceBranch, got.PriceSource)
	require.True(t, dec("7000").Equal(got.Price))
	require.True(t, dec("10").Equal(got.DiscountPercent), "inherited from the catalog")
	require.True(t, dec("6300").Equal(got.SalePrice))
	require.True(t, dec("700").Equal(got.Discount), "per-unit amount stays consistent: price - sale price")
	require.True(t, dec("300").Equal(got.Tax), "tax inside 6,300 at 5%")
}

func TestService_ListProducts_ForABranchWithNoOverride_IsCatalogPricedAndSaysSo(t *testing.T) {
	fx := newBranchPriceFixture(t)
	b := uint(4)
	fx.repo.EXPECT().ListProducts(mock.Anything, uint(7)).Return([]Product{*pricedProduct()}, nil).Once()
	fx.repo.EXPECT().ListProductImagesByProductIDs(mock.Anything, []uint{1}).Return(nil, nil).Once()
	fx.branches.EXPECT().GetBranch(mock.Anything, uint(7), uint(4)).Return(branch(4, "Yangon", identity.BranchStatusActive), nil).Once()
	fx.repo.EXPECT().ListBranchPricesForBranch(mock.Anything, uint(7), uint(4), []uint{1}).Return(nil, nil).Once()

	got, err := fx.svc.ListProducts(context.Background(), 7, &b)

	require.NoError(t, err)
	require.Equal(t, PriceSourceCatalog, got[0].PriceSource)
	require.True(t, dec("6500").Equal(got[0].Price))
	require.True(t, dec("5900").Equal(got[0].SalePrice))
}

// Without a branch the catalog row is returned as it always was: no lookup, no
// price_source.
func TestService_ListProducts_WithoutABranch_IsThePlainCatalog(t *testing.T) {
	fx := newBranchPriceFixture(t)
	fx.repo.EXPECT().ListProducts(mock.Anything, uint(7)).Return([]Product{*pricedProduct()}, nil).Once()
	fx.repo.EXPECT().ListProductImagesByProductIDs(mock.Anything, []uint{1}).Return(nil, nil).Once()

	got, err := fx.svc.ListProducts(context.Background(), 7, nil)

	require.NoError(t, err)
	require.Empty(t, got[0].PriceSource)
	require.True(t, dec("6500").Equal(got[0].Price))
}

func TestService_GetProduct_ForAnotherOrgsBranch_IsNotFound(t *testing.T) {
	fx := newBranchPriceFixture(t)
	b := uint(99)
	fx.repo.EXPECT().GetProduct(mock.Anything, uint(7), uint(1)).Return(pricedProduct(), nil).Once()
	fx.repo.EXPECT().ListProductImagesByProductIDs(mock.Anything, []uint{1}).Return(nil, nil).Once()
	fx.branches.EXPECT().GetBranch(mock.Anything, uint(7), uint(99)).Return(nil, common.NotFoundError("branch not found")).Once()

	_, err := fx.svc.GetProduct(context.Background(), 7, 1, &b)

	requireRestErrorStatus(t, err, http.StatusNotFound)
}

// ---- GET /products/:id/branch-prices ----

func TestService_ListBranchPrices_OneRowPerBranch_WithOverrideAndEffectivePrices(t *testing.T) {
	fx := newBranchPriceFixture(t)
	fx.repo.EXPECT().GetProduct(mock.Anything, uint(7), uint(1)).Return(pricedProduct(), nil).Once()
	fx.branches.EXPECT().ListBranches(mock.Anything, uint(7)).Return([]identity.Branch{
		*branch(3, "Mandalay", identity.BranchStatusActive), *branch(4, "Yangon", identity.BranchStatusActive),
	}, nil).Once()
	fx.repo.EXPECT().ListBranchPricesByProduct(mock.Anything, uint(7), uint(1)).
		Return([]ProductBranchPrice{{ProductID: 1, BranchID: 3, Price: decPtr("7000"), SalePrice: dec("6300")}}, nil).Once()

	got, err := fx.svc.ListBranchPrices(context.Background(), 7, 1, nil)

	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Equal(t, "Mandalay", got[0].BranchName)
	require.True(t, dec("7000").Equal(*got[0].Price))
	require.Nil(t, got[0].DiscountPercent, "inherits the catalog %")
	require.True(t, dec("7000").Equal(got[0].EffectivePrice))
	require.True(t, dec("6300").Equal(got[0].EffectiveSalePrice))
	require.Equal(t, PriceSourceBranch, got[0].PriceSource)
	require.NotNil(t, got[0].UpdatedAt)
	require.Equal(t, "Yangon", got[1].BranchName)
	require.Nil(t, got[1].Price)
	require.True(t, dec("6500").Equal(got[1].EffectivePrice))
	require.True(t, dec("5900").Equal(got[1].EffectiveSalePrice))
	require.Equal(t, PriceSourceCatalog, got[1].PriceSource)
	require.Nil(t, got[1].UpdatedAt)
}

// A manager at a till is limited to its own branch: the other branch's row (and
// its price) is never even looked up.
func TestService_ListBranchPrices_ForOneBranch_ReturnsOnlyThatBranch(t *testing.T) {
	fx := newBranchPriceFixture(t)
	b := uint(3)
	fx.repo.EXPECT().GetProduct(mock.Anything, uint(7), uint(1)).Return(pricedProduct(), nil).Once()
	fx.branches.EXPECT().GetBranch(mock.Anything, uint(7), uint(3)).Return(branch(3, "Mandalay", identity.BranchStatusActive), nil).Once()
	fx.repo.EXPECT().ListBranchPricesByProduct(mock.Anything, uint(7), uint(1)).Return(nil, nil).Once()

	got, err := fx.svc.ListBranchPrices(context.Background(), 7, 1, &b)

	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, uint(3), got[0].BranchID)
}

func TestService_ListBranchPrices_ProductOfAnotherOrg_IsNotFound(t *testing.T) {
	fx := newBranchPriceFixture(t)
	fx.repo.EXPECT().GetProduct(mock.Anything, uint(7), uint(1)).Return(nil, common.NotFoundError("product not found")).Once()

	_, err := fx.svc.ListBranchPrices(context.Background(), 7, 1, nil)

	requireRestErrorStatus(t, err, http.StatusNotFound)
}

// ---- PUT /products/:id/branch-prices/:branchId ----

func (fx *branchPriceFixture) expectProductAndBranch(status identity.BranchStatus) {
	fx.repo.EXPECT().GetProduct(mock.Anything, uint(7), uint(1)).Return(pricedProduct(), nil).Once()
	fx.branches.EXPECT().GetBranch(mock.Anything, uint(7), uint(3)).Return(branch(3, "Mandalay", status), nil).Once()
}

func TestService_SetBranchPrice_PriceOnly_ComputesTheSalePriceWithTheProductsRule(t *testing.T) {
	fx := newBranchPriceFixture(t)
	fx.expectProductAndBranch(identity.BranchStatusActive)
	fx.repo.EXPECT().GetBranchPrice(mock.Anything, uint(7), uint(3), uint(1)).Return(nil, nil).Once()
	var saved *ProductBranchPrice
	fx.repo.EXPECT().UpsertBranchPrice(mock.Anything, mock.Anything).
		Run(func(_ *gorm.DB, r *ProductBranchPrice) { saved = r }).Return(nil).Once()
	var entry *audit.AuditLog
	fx.audW.EXPECT().CreateAuditLog(mock.Anything, mock.Anything).
		Run(func(_ *gorm.DB, e *audit.AuditLog) { entry = e }).Return(nil).Once()

	got, err := fx.svc.SetBranchPrice(context.Background(), 7, ownerActor, 1, 3, SetBranchPriceRequest{Price: decPtr("7000")})

	require.NoError(t, err)
	require.NotNil(t, saved)
	require.Equal(t, uint(7), saved.OrgID)
	require.Equal(t, uint(1), saved.ProductID)
	require.Equal(t, uint(3), saved.BranchID)
	require.True(t, dec("7000").Equal(*saved.Price))
	require.Nil(t, saved.DiscountPercent)
	// 7,000 at the catalog's inherited 10% = 6,300: the same DiscountedUnitPrice
	// rule and rounding a product's own sale price uses
	require.True(t, common.DiscountedUnitPrice(dec("7000"), dec("10")).Equal(saved.SalePrice))
	require.True(t, dec("6300").Equal(saved.SalePrice))
	require.Equal(t, uint(21), *saved.UpdatedByUserID)
	require.True(t, dec("6300").Equal(got.EffectiveSalePrice))
	require.Equal(t, PriceSourceBranch, got.PriceSource)

	require.NotNil(t, entry)
	require.Equal(t, "product_branch_price", entry.Entity)
	require.Equal(t, "1", entry.EntityID)
	require.Equal(t, "branch_price_set", entry.Action)
	require.Equal(t, uint(3), *entry.BranchID)
	require.Equal(t, uint(21), *entry.ActorUserID)
	require.Nil(t, entry.ActorID, "an org-wide account, not a staff member")
	require.Equal(t, "null", string(entry.Before), "nothing before")
	require.Contains(t, string(entry.After), `"sale_price":"6300"`)
}

func TestService_SetBranchPrice_PercentOnly_UsesTheCatalogPrice(t *testing.T) {
	fx := newBranchPriceFixture(t)
	fx.expectProductAndBranch(identity.BranchStatusActive)
	fx.repo.EXPECT().GetBranchPrice(mock.Anything, uint(7), uint(3), uint(1)).Return(nil, nil).Once()
	var saved *ProductBranchPrice
	fx.repo.EXPECT().UpsertBranchPrice(mock.Anything, mock.Anything).
		Run(func(_ *gorm.DB, r *ProductBranchPrice) { saved = r }).Return(nil).Once()
	fx.audW.EXPECT().CreateAuditLog(mock.Anything, mock.Anything).Return(nil).Once()

	_, err := fx.svc.SetBranchPrice(context.Background(), 7, ownerActor, 1, 3, SetBranchPriceRequest{DiscountPercent: decPtr("20")})

	require.NoError(t, err)
	require.Nil(t, saved.Price)
	require.True(t, dec("5200").Equal(saved.SalePrice), "6,500 less 20%%")
}

// The second write replaces the first, and the audit entry carries both.
func TestService_SetBranchPrice_ReplacingAnOverride_AuditsBeforeAndAfter(t *testing.T) {
	fx := newBranchPriceFixture(t)
	fx.expectProductAndBranch(identity.BranchStatusActive)
	fx.repo.EXPECT().GetBranchPrice(mock.Anything, uint(7), uint(3), uint(1)).
		Return(&ProductBranchPrice{ID: 5, Price: decPtr("7000"), SalePrice: dec("6300")}, nil).Once()
	fx.repo.EXPECT().UpsertBranchPrice(mock.Anything, mock.Anything).Return(nil).Once()
	var entry *audit.AuditLog
	fx.audW.EXPECT().CreateAuditLog(mock.Anything, mock.Anything).
		Run(func(_ *gorm.DB, e *audit.AuditLog) { entry = e }).Return(nil).Once()

	_, err := fx.svc.SetBranchPrice(context.Background(), 7, ownerActor, 1, 3, SetBranchPriceRequest{Price: decPtr("1900")})

	require.NoError(t, err)
	require.Contains(t, string(entry.Before), `"price":"7000"`)
	require.Contains(t, string(entry.After), `"price":"1900"`)
}

func TestService_SetBranchPrice_BothNull_ClearsTheOverrideLikeADelete(t *testing.T) {
	fx := newBranchPriceFixture(t)
	// once for the PUT's own checks, once inside the delete it delegates to
	fx.repo.EXPECT().GetProduct(mock.Anything, uint(7), uint(1)).Return(pricedProduct(), nil).Twice()
	fx.branches.EXPECT().GetBranch(mock.Anything, uint(7), uint(3)).Return(branch(3, "Mandalay", identity.BranchStatusActive), nil).Twice()
	fx.repo.EXPECT().GetBranchPrice(mock.Anything, uint(7), uint(3), uint(1)).
		Return(&ProductBranchPrice{ID: 5, Price: decPtr("7000"), SalePrice: dec("6300")}, nil).Once()
	fx.repo.EXPECT().DeleteBranchPrice(mock.Anything, uint(7), uint(1), uint(3)).Return(nil).Once()
	var entry *audit.AuditLog
	fx.audW.EXPECT().CreateAuditLog(mock.Anything, mock.Anything).
		Run(func(_ *gorm.DB, e *audit.AuditLog) { entry = e }).Return(nil).Once()

	got, err := fx.svc.SetBranchPrice(context.Background(), 7, ownerActor, 1, 3, SetBranchPriceRequest{})

	require.NoError(t, err)
	require.Equal(t, "branch_price_removed", entry.Action)
	require.Equal(t, PriceSourceCatalog, got.PriceSource)
	require.Nil(t, got.Price)
	require.True(t, dec("5900").Equal(got.EffectiveSalePrice))
}

func TestService_SetBranchPrice_Validation_RejectsBeforeWriting(t *testing.T) {
	cases := map[string]SetBranchPriceRequest{
		"zero price":        {Price: decPtr("0")},
		"negative price":    {Price: decPtr("-1")},
		"negative percent":  {DiscountPercent: decPtr("-1")},
		"percent above 100": {DiscountPercent: decPtr("100.5")},
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			fx := newBranchPriceFixture(t)
			fx.expectProductAndBranch(identity.BranchStatusActive)
			// no upsert, no audit

			_, err := fx.svc.SetBranchPrice(context.Background(), 7, ownerActor, 1, 3, in)

			requireRestErrorStatus(t, err, http.StatusBadRequest)
		})
	}
}

func TestService_SetBranchPrice_InactiveBranch_IsBadRequest(t *testing.T) {
	fx := newBranchPriceFixture(t)
	fx.expectProductAndBranch(identity.BranchStatusInactive)

	_, err := fx.svc.SetBranchPrice(context.Background(), 7, ownerActor, 1, 3, SetBranchPriceRequest{Price: decPtr("7000")})

	requireRestErrorStatus(t, err, http.StatusBadRequest)
}

func TestService_SetBranchPrice_BranchOfAnotherOrg_IsNotFound(t *testing.T) {
	fx := newBranchPriceFixture(t)
	fx.repo.EXPECT().GetProduct(mock.Anything, uint(7), uint(1)).Return(pricedProduct(), nil).Once()
	fx.branches.EXPECT().GetBranch(mock.Anything, uint(7), uint(3)).Return(nil, common.NotFoundError("branch not found")).Once()

	_, err := fx.svc.SetBranchPrice(context.Background(), 7, ownerActor, 1, 3, SetBranchPriceRequest{Price: decPtr("7000")})

	requireRestErrorStatus(t, err, http.StatusNotFound)
}

func TestService_SetBranchPrice_ProductOfAnotherOrg_IsNotFound(t *testing.T) {
	fx := newBranchPriceFixture(t)
	fx.repo.EXPECT().GetProduct(mock.Anything, uint(7), uint(1)).Return(nil, common.NotFoundError("product not found")).Once()

	_, err := fx.svc.SetBranchPrice(context.Background(), 7, ownerActor, 1, 3, SetBranchPriceRequest{Price: decPtr("7000")})

	requireRestErrorStatus(t, err, http.StatusNotFound)
}

// ---- DELETE /products/:id/branch-prices/:branchId ----

func TestService_DeleteBranchPrice_RemovesTheOverrideAndAudits(t *testing.T) {
	fx := newBranchPriceFixture(t)
	fx.expectProductAndBranch(identity.BranchStatusActive)
	fx.repo.EXPECT().GetBranchPrice(mock.Anything, uint(7), uint(3), uint(1)).
		Return(&ProductBranchPrice{ID: 5, Price: decPtr("7000"), SalePrice: dec("6300")}, nil).Once()
	fx.repo.EXPECT().DeleteBranchPrice(mock.Anything, uint(7), uint(1), uint(3)).Return(nil).Once()
	var entry *audit.AuditLog
	fx.audW.EXPECT().CreateAuditLog(mock.Anything, mock.Anything).
		Run(func(_ *gorm.DB, e *audit.AuditLog) { entry = e }).Return(nil).Once()

	require.NoError(t, fx.svc.DeleteBranchPrice(context.Background(), 7, ownerActor, 1, 3))

	require.Equal(t, "branch_price_removed", entry.Action)
	require.Equal(t, uint(3), *entry.BranchID)
	require.Contains(t, string(entry.Before), `"price":"7000"`)
	require.Equal(t, "null", string(entry.After))
}

// Deleting what isn't there is a no-op: nothing to write, nothing to audit.
func TestService_DeleteBranchPrice_NoOverride_IsANoOp(t *testing.T) {
	fx := newBranchPriceFixture(t)
	fx.expectProductAndBranch(identity.BranchStatusActive)
	fx.repo.EXPECT().GetBranchPrice(mock.Anything, uint(7), uint(3), uint(1)).Return(nil, nil).Once()

	require.NoError(t, fx.svc.DeleteBranchPrice(context.Background(), 7, ownerActor, 1, 3))
}

// A closed branch's overrides can still be cleaned up.
func TestService_DeleteBranchPrice_InactiveBranch_IsAllowed(t *testing.T) {
	fx := newBranchPriceFixture(t)
	fx.expectProductAndBranch(identity.BranchStatusInactive)
	fx.repo.EXPECT().GetBranchPrice(mock.Anything, uint(7), uint(3), uint(1)).
		Return(&ProductBranchPrice{ID: 5, Price: decPtr("7000"), SalePrice: dec("6300")}, nil).Once()
	fx.repo.EXPECT().DeleteBranchPrice(mock.Anything, uint(7), uint(1), uint(3)).Return(nil).Once()
	fx.audW.EXPECT().CreateAuditLog(mock.Anything, mock.Anything).Return(nil).Once()

	require.NoError(t, fx.svc.DeleteBranchPrice(context.Background(), 7, ownerActor, 1, 3))
}

// ---- keeping sale_price in step with the catalog ----

// An override that inherits the changed field gets a new sale price; one that
// sets both fields is untouched.
func TestService_UpdateProduct_ChangingTheCatalogPrice_RecomputesInheritingOverrides(t *testing.T) {
	fx := newBranchPriceFixture(t)
	existing := pricedProduct() // 6,500 at 10%
	percentOnly := ProductBranchPrice{ID: 11, ProductID: 1, BranchID: 3, DiscountPercent: decPtr("20"), SalePrice: dec("5200")}
	priceOnly := ProductBranchPrice{ID: 12, ProductID: 1, BranchID: 4, Price: decPtr("7000"), SalePrice: dec("6300")}
	both := ProductBranchPrice{ID: 13, ProductID: 1, BranchID: 5, Price: decPtr("7000"), DiscountPercent: decPtr("0"), SalePrice: dec("7000")}
	fx.repo.EXPECT().GetProduct(mock.Anything, uint(7), uint(1)).Return(existing, nil).Twice()
	fx.repo.EXPECT().ListBranchPricesByProduct(mock.Anything, uint(7), uint(1)).
		Return([]ProductBranchPrice{percentOnly, priceOnly, both}, nil).Once()
	fx.repo.EXPECT().UpdateProduct(mock.Anything, uint(1), mock.Anything).Return(nil).Once()
	// the catalog price goes to 8,000: the percent-only override follows (8,000
	// less 20% = 6,400); the price-only one keeps its own price and the catalog
	// 10% (unchanged), and the both-set one is untouched
	fx.repo.EXPECT().UpdateBranchPriceSalePrice(mock.Anything, uint(11), mock.MatchedBy(func(d decimal.Decimal) bool { return d.Equal(dec("6400")) })).Return(nil).Once()
	price := dec("8000")

	_, err := fx.svc.UpdateProduct(context.Background(), 7, 1, UpdateProductRequest{Price: &price}, nil, 0, "", "")

	require.NoError(t, err)
}

func TestService_UpdateProduct_ChangingTheCatalogPercent_RecomputesPriceOnlyOverrides(t *testing.T) {
	fx := newBranchPriceFixture(t)
	priceOnly := ProductBranchPrice{ID: 12, ProductID: 1, BranchID: 4, Price: decPtr("7000"), SalePrice: dec("6300")}
	fx.repo.EXPECT().GetProduct(mock.Anything, uint(7), uint(1)).Return(pricedProduct(), nil).Twice()
	fx.repo.EXPECT().ListBranchPricesByProduct(mock.Anything, uint(7), uint(1)).Return([]ProductBranchPrice{priceOnly}, nil).Once()
	fx.repo.EXPECT().UpdateProduct(mock.Anything, uint(1), mock.Anything).Return(nil).Once()
	fx.repo.EXPECT().UpdateBranchPriceSalePrice(mock.Anything, uint(12), mock.MatchedBy(func(d decimal.Decimal) bool { return d.Equal(dec("5600")) })).Return(nil).Once() // 7,000 less 20%
	pct := dec("20")

	_, err := fx.svc.UpdateProduct(context.Background(), 7, 1, UpdateProductRequest{DiscountPercent: &pct}, nil, 0, "", "")

	require.NoError(t, err)
}

// Editing something unrelated never touches the overrides.
func TestService_UpdateProduct_UnrelatedField_DoesNotLookAtOverrides(t *testing.T) {
	fx := newBranchPriceFixture(t)
	fx.repo.EXPECT().GetProduct(mock.Anything, uint(7), uint(1)).Return(pricedProduct(), nil).Twice()
	fx.repo.EXPECT().UpdateProduct(mock.Anything, uint(1), mock.Anything).Return(nil).Once()
	name := "Renamed"

	_, err := fx.svc.UpdateProduct(context.Background(), 7, 1, UpdateProductRequest{Name: &name}, nil, 0, "", "")

	require.NoError(t, err)
}

// ---- a manager at a till (manage_branch_prices) ----

// A manager writes their OWN branch: the row records the staff member (not a
// user), and so does the audit entry.
func TestService_SetBranchPrice_ByAManagerOnTheirOwnBranch_RecordsTheStaffActor(t *testing.T) {
	fx := newBranchPriceFixture(t)
	fx.expectProductAndBranch(identity.BranchStatusActive)
	fx.repo.EXPECT().GetBranchPrice(mock.Anything, uint(7), uint(3), uint(1)).Return(nil, nil).Once()
	var saved *ProductBranchPrice
	fx.repo.EXPECT().UpsertBranchPrice(mock.Anything, mock.Anything).
		Run(func(_ *gorm.DB, r *ProductBranchPrice) { saved = r }).Return(nil).Once()
	var entry *audit.AuditLog
	fx.audW.EXPECT().CreateAuditLog(mock.Anything, mock.Anything).
		Run(func(_ *gorm.DB, e *audit.AuditLog) { entry = e }).Return(nil).Once()

	got, err := fx.svc.SetBranchPrice(context.Background(), 7, managerActor, 1, 3, SetBranchPriceRequest{Price: decPtr("7000")})

	require.NoError(t, err)
	require.True(t, dec("6300").Equal(got.EffectiveSalePrice))
	require.Equal(t, uint(14), *saved.UpdatedByStaffID)
	require.Nil(t, saved.UpdatedByUserID, "exactly one actor")
	require.Equal(t, uint(14), *entry.ActorID, "a manager is the staff actor")
	require.Nil(t, entry.ActorUserID)
	require.Equal(t, "branch_price_set", entry.Action)
	require.Equal(t, uint(3), *entry.BranchID)
}

// The Owner stays the user actor and has no staff id.
func TestService_SetBranchPrice_ByTheOwner_RecordsTheUserActorOnly(t *testing.T) {
	fx := newBranchPriceFixture(t)
	fx.expectProductAndBranch(identity.BranchStatusActive)
	fx.repo.EXPECT().GetBranchPrice(mock.Anything, uint(7), uint(3), uint(1)).Return(nil, nil).Once()
	var saved *ProductBranchPrice
	fx.repo.EXPECT().UpsertBranchPrice(mock.Anything, mock.Anything).
		Run(func(_ *gorm.DB, r *ProductBranchPrice) { saved = r }).Return(nil).Once()
	var entry *audit.AuditLog
	fx.audW.EXPECT().CreateAuditLog(mock.Anything, mock.Anything).
		Run(func(_ *gorm.DB, e *audit.AuditLog) { entry = e }).Return(nil).Once()

	_, err := fx.svc.SetBranchPrice(context.Background(), 7, ownerActor, 1, 3, SetBranchPriceRequest{Price: decPtr("7000")})

	require.NoError(t, err)
	require.Equal(t, uint(21), *saved.UpdatedByUserID)
	require.Nil(t, saved.UpdatedByStaffID)
	require.Equal(t, uint(21), *entry.ActorUserID)
	require.Nil(t, entry.ActorID)
}

// Another branch - or another org's branch id - is a 403, decided before
// anything is looked up (so it tells a manager nothing about that id) and
// before anything is written.
func TestService_SetBranchPrice_ByAManagerOnAnotherBranch_IsForbiddenBeforeAnyLookup(t *testing.T) {
	for _, otherBranch := range []uint{4, 99} {
		fx := newBranchPriceFixture(t) // no repo / branch / audit expectation: any call fails the test

		_, err := fx.svc.SetBranchPrice(context.Background(), 7, managerActor, 1, otherBranch, SetBranchPriceRequest{Price: decPtr("7000")})

		requireRestErrorStatus(t, err, http.StatusForbidden)
		require.Contains(t, err.Error(), "your own branch")
	}
}

func TestService_SetBranchPrice_BothNullByAManagerOnAnotherBranch_IsForbidden(t *testing.T) {
	fx := newBranchPriceFixture(t)

	_, err := fx.svc.SetBranchPrice(context.Background(), 7, managerActor, 1, 4, SetBranchPriceRequest{})

	requireRestErrorStatus(t, err, http.StatusForbidden)
}

func TestService_DeleteBranchPrice_ByAManagerOnTheirOwnBranch_AuditsTheStaffActor(t *testing.T) {
	fx := newBranchPriceFixture(t)
	fx.expectProductAndBranch(identity.BranchStatusActive)
	fx.repo.EXPECT().GetBranchPrice(mock.Anything, uint(7), uint(3), uint(1)).
		Return(&ProductBranchPrice{ID: 5, Price: decPtr("7000"), SalePrice: dec("6300")}, nil).Once()
	fx.repo.EXPECT().DeleteBranchPrice(mock.Anything, uint(7), uint(1), uint(3)).Return(nil).Once()
	var entry *audit.AuditLog
	fx.audW.EXPECT().CreateAuditLog(mock.Anything, mock.Anything).
		Run(func(_ *gorm.DB, e *audit.AuditLog) { entry = e }).Return(nil).Once()

	require.NoError(t, fx.svc.DeleteBranchPrice(context.Background(), 7, managerActor, 1, 3))

	require.Equal(t, "branch_price_removed", entry.Action)
	require.Equal(t, uint(14), *entry.ActorID)
	require.Nil(t, entry.ActorUserID)
}

func TestService_DeleteBranchPrice_ByAManagerOnAnotherBranch_IsForbidden(t *testing.T) {
	fx := newBranchPriceFixture(t)

	err := fx.svc.DeleteBranchPrice(context.Background(), 7, managerActor, 1, 4)

	requireRestErrorStatus(t, err, http.StatusForbidden)
}

// The Owner is unchanged: any branch of the org.
func TestService_SetBranchPrice_ByTheOwner_MayWriteAnyBranch(t *testing.T) {
	fx := newBranchPriceFixture(t)
	fx.repo.EXPECT().GetProduct(mock.Anything, uint(7), uint(1)).Return(pricedProduct(), nil).Once()
	fx.branches.EXPECT().GetBranch(mock.Anything, uint(7), uint(4)).Return(branch(4, "Yangon", identity.BranchStatusActive), nil).Once()
	fx.repo.EXPECT().GetBranchPrice(mock.Anything, uint(7), uint(4), uint(1)).Return(nil, nil).Once()
	fx.repo.EXPECT().UpsertBranchPrice(mock.Anything, mock.Anything).Return(nil).Once()
	fx.audW.EXPECT().CreateAuditLog(mock.Anything, mock.Anything).Return(nil).Once()

	_, err := fx.svc.SetBranchPrice(context.Background(), 7, ownerActor, 1, 4, SetBranchPriceRequest{Price: decPtr("7000")})

	require.NoError(t, err)
}

// ---- catalog values on the rows ----

// A till token can't read catalog prices anywhere else, so every row - the
// GET's and the PUT's - carries what a blank field falls back to.
func TestService_ListBranchPrices_RowsCarryTheCatalogPriceAndPercent(t *testing.T) {
	fx := newBranchPriceFixture(t)
	b := uint(3)
	fx.repo.EXPECT().GetProduct(mock.Anything, uint(7), uint(1)).Return(pricedProduct(), nil).Once()
	fx.branches.EXPECT().GetBranch(mock.Anything, uint(7), uint(3)).Return(branch(3, "Mandalay", identity.BranchStatusActive), nil).Once()
	fx.repo.EXPECT().ListBranchPricesByProduct(mock.Anything, uint(7), uint(1)).
		Return([]ProductBranchPrice{{ProductID: 1, BranchID: 3, Price: decPtr("7000"), SalePrice: dec("6300")}}, nil).Once()

	got, err := fx.svc.ListBranchPrices(context.Background(), 7, 1, &b)

	require.NoError(t, err)
	require.True(t, dec("6500").Equal(got[0].CatalogPrice), "the catalog's, not the override's 7,000")
	require.True(t, dec("10").Equal(got[0].CatalogDiscountPercent))
	require.True(t, dec("7000").Equal(got[0].EffectivePrice))
}

func TestService_SetBranchPrice_ResponseCarriesTheCatalogPriceAndPercent(t *testing.T) {
	fx := newBranchPriceFixture(t)
	fx.expectProductAndBranch(identity.BranchStatusActive)
	fx.repo.EXPECT().GetBranchPrice(mock.Anything, uint(7), uint(3), uint(1)).Return(nil, nil).Once()
	fx.repo.EXPECT().UpsertBranchPrice(mock.Anything, mock.Anything).Return(nil).Once()
	fx.audW.EXPECT().CreateAuditLog(mock.Anything, mock.Anything).Return(nil).Once()

	got, err := fx.svc.SetBranchPrice(context.Background(), 7, managerActor, 1, 3, SetBranchPriceRequest{Price: decPtr("7000")})

	require.NoError(t, err)
	require.True(t, dec("6500").Equal(got.CatalogPrice))
	require.True(t, dec("10").Equal(got.CatalogDiscountPercent))
	body, err := json.Marshal(got)
	require.NoError(t, err)
	require.Contains(t, string(body), `"catalog_price":"6500"`)
	require.Contains(t, string(body), `"catalog_discount_percent":"10"`)
}
