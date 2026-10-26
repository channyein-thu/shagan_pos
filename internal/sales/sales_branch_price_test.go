package sales

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"shagan_pos/internal/audit"
	"shagan_pos/internal/catalog"
	"shagan_pos/internal/common"
	"shagan_pos/internal/identity"
	"shagan_pos/internal/inventory"
)

// Product 1 is 6,500 at the catalog's 10% (sale price 5,900 after Kyat
// rounding). Branch 3 - the fixture's selling branch - overrides its price to
// 7,000 and inherits the 10%, so it sells at 6,300; branch 4 has no override.
func branch3Override() *catalog.ProductBranchPrice {
	price := d("7000")
	return &catalog.ProductBranchPrice{
		OrgID: 7, ProductID: 1, BranchID: 3, Price: &price,
		SalePrice: common.DiscountedUnitPrice(price, d("10")),
	}
}

// A till whose catalog still shows the catalog price is refused at a branch
// that overrides it: it must refresh.
func TestService_CreateSale_AtTheCatalogPrice_IsConflictAtABranchWithAnOverride(t *testing.T) {
	fx := newSaleFixture(t)
	fx.expectNothingWritten()
	fx.hasBranchPrice(catalogProduct(1, "6500", "10"), branch3Override())
	item := line(1, 1)
	item.UnitPrice = d("6500")

	_, err := fx.create(oneSale([]CreateSaleItemRequest{item}, "5900"))

	requireRestErrorStatus(t, err, http.StatusConflict)
	require.Contains(t, err.Error(), "price changed, refresh the catalog")
}

// At the override price the sale is accepted, snapshots the BRANCH price and
// percent on the line, and is totalled from the branch's sale price.
func TestService_CreateSale_AtTheOverridePrice_IsPricedFromTheBranch(t *testing.T) {
	fx := newSaleFixture(t)
	fx.hasBranchPrice(catalogProduct(1, "6500", "10"), branch3Override())
	got := fx.expectPersist()
	item := line(1, 1)
	item.UnitPrice = d("7000")

	_, err := fx.create(oneSale([]CreateSaleItemRequest{item}, "6300"))

	require.NoError(t, err)
	require.True(t, d("7000").Equal(got.items[0].UnitPrice))
	require.True(t, d("10").Equal(got.items[0].CatalogDiscountPercent), "the percent is inherited from the catalog")
	require.True(t, d("700").Equal(got.items[0].CatalogDiscount), "7,000 less the 6,300 sale price")
	require.True(t, d("6300").Equal(got.items[0].LineTotal))
}

// A price-less request line is also priced from the branch.
func TestService_CreateSale_WithoutAClientPrice_IsPricedFromTheBranch(t *testing.T) {
	fx := newSaleFixture(t)
	fx.hasBranchPrice(catalogProduct(1, "6500", "10"), branch3Override())
	got := fx.expectPersist()

	_, err := fx.create(oneSale([]CreateSaleItemRequest{line(1, 2)}, "12600"))

	require.NoError(t, err)
	require.True(t, d("7000").Equal(got.items[0].UnitPrice))
	require.True(t, d("12600").Equal(got.items[0].LineTotal))
}

// The override belongs to branch 3 only: at branch 4 the catalog price is the
// right one and the branch-3 price is the stale one.
func TestService_CreateSale_AtAnotherBranch_TheCatalogPriceStillApplies(t *testing.T) {
	fx := newSaleFixture(t)
	fx.hasBranchPrice(catalogProduct(1, "6500", "10"), branch3Override())
	var items []SaleItem
	fx.repo.EXPECT().GetSale(mock.Anything, uint(7), mock.Anything).Return(nil, common.NotFoundError("sale not found")).Twice()
	fx.repo.EXPECT().RequireOpenShift(mock.Anything, uint(7), uint(4), uint(9)).Return(nil).Once()
	fx.repo.EXPECT().CreateSale(mock.Anything, mock.Anything).Return(nil).Once()
	fx.repo.EXPECT().CreateSaleItems(mock.Anything, mock.Anything).Run(func(_ *gorm.DB, i []SaleItem) { items = i }).Return(nil).Once()
	fx.repo.EXPECT().CreatePayments(mock.Anything, mock.Anything).Return(nil).Once()
	fx.inv.EXPECT().GetStockLevel(mock.Anything, mock.Anything, uint(4)).Return(&inventory.StockLevel{ID: 51, Qty: 100}, nil).Maybe()
	fx.inv.EXPECT().UpdateStockLevelQty(mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	fx.inv.EXPECT().CreateInventoryLedgerEntry(mock.Anything, mock.Anything).Return(nil).Maybe()
	fx.audW.EXPECT().CreateAuditLog(mock.Anything, mock.Anything).Return(nil).Maybe()
	atBranch4 := func(unitPrice, total string) error {
		item := line(1, 1)
		item.UnitPrice = d(unitPrice)
		_, _, err := fx.svc.CreateSale(context.Background(), 7, 4, seller, oneSale([]CreateSaleItemRequest{item}, total), false)
		return err
	}

	require.NoError(t, atBranch4("6500", "5900"))
	require.True(t, d("6500").Equal(items[0].UnitPrice))

	// ...and the branch-3 price is now the stale one
	err := atBranch4("7000", "6300")
	requireRestErrorStatus(t, err, http.StatusConflict)
}

// A price override on its own leaves the percent to the catalog, and a percent
// override on its own leaves the price: each falls back independently.
func TestService_CreateSale_PercentOnlyOverride_KeepsTheCatalogPrice(t *testing.T) {
	fx := newSaleFixture(t)
	pct := d("20")
	fx.hasBranchPrice(catalogProduct(1, "6500", "10"), &catalog.ProductBranchPrice{
		OrgID: 7, ProductID: 1, BranchID: 3, DiscountPercent: &pct, SalePrice: common.DiscountedUnitPrice(d("6500"), pct),
	})
	got := fx.expectPersist()

	_, err := fx.create(oneSale([]CreateSaleItemRequest{line(1, 1)}, "5200"))

	require.NoError(t, err)
	require.True(t, d("6500").Equal(got.items[0].UnitPrice))
	require.True(t, d("20").Equal(got.items[0].CatalogDiscountPercent))
	require.True(t, d("5200").Equal(got.items[0].LineTotal))
}

// ---- offline-queued sales ----

// The till's cache holds the catalog price while its branch overrides it: the
// sale stands as rung up and is flagged against the BRANCH's price.
func TestService_CreateSale_Queued_AgainstABranchOverride_IsAMismatchOfTheBranchPrice(t *testing.T) {
	fx := newSaleFixture(t)
	fx.hasBranchPrice(catalogProduct(1, "6500", "10"), branch3Override())
	got := fx.expectPersist()
	pct := d("10")
	item := line(1, 1)
	item.UnitPrice = d("6500")
	item.CatalogDiscountPercent = &pct

	sale, _, err := fx.createQueued(oneSale([]CreateSaleItemRequest{item}, "5900"))

	require.NoError(t, err)
	require.True(t, d("6500").Equal(got.items[0].UnitPrice), "the till's price, as rung up")
	require.Len(t, sale.PriceMismatches, 1)
	m := sale.PriceMismatches[0]
	require.True(t, d("6500").Equal(m.ClientUnitPrice))
	require.True(t, d("7000").Equal(m.CurrentUnitPrice), "the branch's price, not the catalog's")
	require.True(t, d("10").Equal(m.CurrentPercent))
}

// A till that already synced the branch price rings up no mismatch.
func TestService_CreateSale_Queued_AtTheBranchPrice_HasNoMismatch(t *testing.T) {
	fx := newSaleFixture(t)
	fx.hasBranchPrice(catalogProduct(1, "6500", "10"), branch3Override())
	fx.expectPersist()
	pct := d("10")
	item := line(1, 1)
	item.UnitPrice = d("7000")
	item.CatalogDiscountPercent = &pct

	sale, _, err := fx.createQueued(oneSale([]CreateSaleItemRequest{item}, "6300"))

	require.NoError(t, err)
	require.Empty(t, sale.PriceMismatches)
}

// ---- discount approvals ----

// The approval is for a % off the branch's price, and the prices it reports
// back to the till are the branch's.
func TestService_CreateDiscountApproval_UsesTheBranchPrice(t *testing.T) {
	fx := newApprovalFixture(t)
	fx.repo.EXPECT().FindOpenShift(mock.Anything, uint(7), uint(70)).Return(&OpenShift{ID: 9, BranchID: 3}, nil).Once()
	fx.products.EXPECT().GetProduct(mock.Anything, uint(7), uint(1)).Return(catalogProduct(1, "6500", "0"), nil).Once()
	price := d("7000")
	fx.products.EXPECT().GetBranchPrice(mock.Anything, uint(7), uint(3), uint(1)).
		Return(&catalog.ProductBranchPrice{OrgID: 7, ProductID: 1, BranchID: 3, Price: &price, SalePrice: price}, nil).Once()
	fx.repo.EXPECT().CreateDiscountApproval(mock.Anything, mock.Anything).Return(nil).Once()
	fx.audW.EXPECT().CreateAuditLog(mock.Anything, mock.Anything).Run(func(_ *gorm.DB, _ *audit.AuditLog) {}).Return(nil).Once()
	// self-approval: the seller may approve their own discounts
	fx.staff.EXPECT().GetStaff(mock.Anything, uint(7), uint(17)).Return(&identity.Staff{ID: 17, Name: "Ko Zaw"}, nil).Once()

	got, err := fx.svc.CreateDiscountApproval(context.Background(), 7, 3, selfApprovingSeller(), CreateDiscountApprovalRequest{
		ProductID: 1, Percent: d("10"), Qty: 1, Reason: "damaged box",
	})

	require.NoError(t, err)
	require.True(t, d("7000").Equal(got.UnitPrice), "the branch price, not the catalog's 6,500")
	require.True(t, d("6300").Equal(got.UnitSalePrice), "7,000 less 10%")
}
