package sales

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"shagan_pos/internal/catalog"
	"shagan_pos/internal/common"
)

// A combo is only worth what the SERVER says: it must exist, be unexpired, be
// rung up as exactly its components, and its bundle discount is computed from
// the combo's price. The client's own "discount" on a combo line is never read
// - that was the hole (a fake combo_id with discount = the price was a free sale).

const testComboID = uint(5)

func inAnHour() time.Time { return time.Now().Add(time.Hour) }

func comboComponent(comboID, productID uint, qty int) catalog.ComboItem {
	return catalog.ComboItem{ComboID: comboID, ProductID: productID, Qty: qty}
}

// comboLine is one component line of combo 5. claimed is the "discount" the
// till claims for it, which the server must ignore.
func comboLine(productID uint, qty int, claimed string) CreateSaleItemRequest {
	id := testComboID
	return CreateSaleItemRequest{ProductID: productID, ComboID: &id, NameSnapshot: "Item", Qty: qty, Discount: d(claimed)}
}

// hasCombo stubs combo 5 of org 7 and its components, each looked up once.
func (fx *saleFixture) hasCombo(price string, expiresAt time.Time, components ...catalog.ComboItem) {
	fx.combos.EXPECT().GetCombo(mock.Anything, uint(7), testComboID).
		Return(&catalog.Combo{ID: testComboID, OrgID: 7, Name: "Bundle", Price: d(price), ExpiresAt: expiresAt}, nil).Once()
	fx.combos.EXPECT().ListComboItemsByComboIDs(mock.Anything, []uint{testComboID}).Return(components, nil).Once()
}

// hasRiceAndOil is the usual bundle: product 1 (K 6,000) + product 2 (K 4,000)
// sold together for K 9,000.
func (fx *saleFixture) hasRiceAndOil() {
	fx.hasProducts(catalogProduct(1, "6000", "0"), catalogProduct(2, "4000", "0"))
	fx.hasCombo("9000", inAnHour(), comboComponent(testComboID, 1, 1), comboComponent(testComboID, 2, 1))
}

func (fx *saleFixture) createQueuedSale(in CreateSaleRequest) (*Sale, error) {
	sale, _, err := fx.svc.CreateSale(context.Background(), 7, 3, seller, in, true)
	return sale, err
}

// ---- the hole: a fake combo, or a real one with an invented discount ----

// The original exploit: any combo_id plus a "discount" equal to the price.
func TestService_CreateSale_ComboThatDoesNotExist_IsNotFoundNotAFreeSale(t *testing.T) {
	fx := newSaleFixture(t)
	fx.expectNothingWritten()
	fx.hasProducts(catalogProduct(1, "6000", "0"))
	fake := uint(99999)
	fx.combos.EXPECT().GetCombo(mock.Anything, uint(7), fake).Return(nil, common.NotFoundError("combo not found")).Once()
	item := CreateSaleItemRequest{ProductID: 1, ComboID: &fake, NameSnapshot: "Rice", Qty: 1, Discount: d("6000")}

	_, err := fx.create(oneSale([]CreateSaleItemRequest{item}, "0"))

	requireRestErrorStatus(t, err, http.StatusNotFound)
}

// Another org's combo is indistinguishable from a missing one: GetCombo is
// asked for the caller's org only, and answers 404.
func TestService_CreateSale_ComboOfAnotherOrg_IsNotFound(t *testing.T) {
	fx := newSaleFixture(t)
	fx.expectNothingWritten()
	fx.hasProducts(catalogProduct(1, "6000", "0"))
	fx.combos.EXPECT().GetCombo(mock.Anything, uint(7), testComboID).Return(nil, common.NotFoundError("combo not found")).Once()

	_, err := fx.create(oneSale([]CreateSaleItemRequest{comboLine(1, 1, "0")}, "6000"))

	requireRestErrorStatus(t, err, http.StatusNotFound)
}

// A real combo with every component present but each line claiming it is free:
// the claim is ignored and the bundle costs what the combo says.
func TestService_CreateSale_RealComboWithAFullPriceDiscountClaim_IsStillChargedTheComboPrice(t *testing.T) {
	fx := newSaleFixture(t)
	fx.hasRiceAndOil()
	got := fx.expectPersist()

	_, err := fx.create(oneSale([]CreateSaleItemRequest{comboLine(1, 1, "6000"), comboLine(2, 1, "4000")}, "9000"))

	require.NoError(t, err)
	require.True(t, d("9000").Equal(got.sale.Total), "got %s", got.sale.Total)
	require.True(t, d("1000").Equal(got.sale.Discount))
}

// Selling only the cheap or only one component under the combo id to grab the
// bundle discount is not a bundle.
func TestService_CreateSale_ComboWithOnlyOneOfItsLinesAndAFullDiscountClaim_IsBadRequest(t *testing.T) {
	fx := newSaleFixture(t)
	fx.expectNothingWritten()
	fx.hasRiceAndOil()

	_, err := fx.create(oneSale([]CreateSaleItemRequest{comboLine(1, 1, "6000")}, "0"))

	requireRestErrorStatus(t, err, http.StatusBadRequest)
}

// ---- the lines must be the combo ----

func TestService_CreateSale_ComboLineMissingAComponent_IsBadRequest(t *testing.T) {
	fx := newSaleFixture(t)
	fx.expectNothingWritten()
	fx.hasRiceAndOil()

	_, err := fx.create(oneSale([]CreateSaleItemRequest{comboLine(1, 1, "0")}, "6000"))

	requireRestErrorStatus(t, err, http.StatusBadRequest)
	require.Contains(t, err.Error(), "combo 5: line for product 2 is missing")
}

func TestService_CreateSale_ComboLineForAProductNotInTheCombo_IsBadRequest(t *testing.T) {
	fx := newSaleFixture(t)
	fx.expectNothingWritten()
	fx.hasRiceAndOil()
	fx.hasProducts(catalogProduct(3, "100", "0"))

	_, err := fx.create(oneSale([]CreateSaleItemRequest{comboLine(1, 1, "0"), comboLine(2, 1, "0"), comboLine(3, 1, "0")}, "9100"))

	requireRestErrorStatus(t, err, http.StatusBadRequest)
	require.Contains(t, err.Error(), "product 3 is not part of the bundle")
}

func TestService_CreateSale_ComboProductListedTwice_IsBadRequest(t *testing.T) {
	fx := newSaleFixture(t)
	fx.expectNothingWritten()
	fx.hasRiceAndOil()

	_, err := fx.create(oneSale([]CreateSaleItemRequest{comboLine(1, 1, "0"), comboLine(1, 1, "0"), comboLine(2, 1, "0")}, "15000"))

	requireRestErrorStatus(t, err, http.StatusBadRequest)
	require.Contains(t, err.Error(), "listed more than once")
}

func TestService_CreateSale_ComboQuantityNotAMultipleOfTheBundle_IsBadRequest(t *testing.T) {
	fx := newSaleFixture(t)
	fx.expectNothingWritten()
	fx.hasProducts(catalogProduct(1, "6000", "0"), catalogProduct(2, "4000", "0"))
	fx.hasCombo("14000", inAnHour(), comboComponent(testComboID, 1, 2), comboComponent(testComboID, 2, 1))

	_, err := fx.create(oneSale([]CreateSaleItemRequest{comboLine(1, 3, "0"), comboLine(2, 1, "0")}, "22000"))

	requireRestErrorStatus(t, err, http.StatusBadRequest)
	require.Contains(t, err.Error(), "quantities don't match the bundle")
}

// One bundle of rice but two of oil is not a whole number of bundles.
func TestService_CreateSale_ComboComponentsWithDifferentBundleCounts_IsBadRequest(t *testing.T) {
	fx := newSaleFixture(t)
	fx.expectNothingWritten()
	fx.hasRiceAndOil()

	_, err := fx.create(oneSale([]CreateSaleItemRequest{comboLine(1, 1, "0"), comboLine(2, 2, "0")}, "14000"))

	requireRestErrorStatus(t, err, http.StatusBadRequest)
	require.Contains(t, err.Error(), "quantities don't match the bundle")
}

func TestService_CreateSale_ExpiredCombo_IsConflict(t *testing.T) {
	fx := newSaleFixture(t)
	fx.expectNothingWritten()
	fx.hasProducts(catalogProduct(1, "6000", "0"), catalogProduct(2, "4000", "0"))
	fx.hasCombo("9000", time.Now().Add(-time.Hour), comboComponent(testComboID, 1, 1), comboComponent(testComboID, 2, 1))

	_, err := fx.create(oneSale([]CreateSaleItemRequest{comboLine(1, 1, "0"), comboLine(2, 1, "0")}, "9000"))

	requireRestErrorStatus(t, err, http.StatusConflict)
	require.Contains(t, err.Error(), "combo has expired")
}

// ---- pricing: the lines add up to exactly the combo price ----

func TestService_CreateSale_Combo_OneBundleAddsUpToTheComboPrice(t *testing.T) {
	fx := newSaleFixture(t)
	fx.hasRiceAndOil()
	got := fx.expectPersist()

	_, err := fx.create(oneSale([]CreateSaleItemRequest{comboLine(1, 1, "0"), comboLine(2, 1, "0")}, "9000"))

	require.NoError(t, err)
	// 1,000 off K 10,000, shared 6:4 by normal price
	require.True(t, d("600").Equal(got.items[0].ComboDiscount))
	require.True(t, d("400").Equal(got.items[1].ComboDiscount))
	require.True(t, d("5400").Equal(got.items[0].LineTotal))
	require.True(t, d("3600").Equal(got.items[1].LineTotal))
	require.True(t, got.items[0].CatalogDiscount.IsZero() && got.items[0].ManualDiscount.IsZero())
	require.True(t, d("10000").Equal(got.sale.Subtotal))
	require.True(t, d("1000").Equal(got.sale.Discount))
	require.True(t, d("9000").Equal(got.sale.Total))
}

func TestService_CreateSale_Combo_ThreeBundlesAddUpToThreeTimesTheComboPrice(t *testing.T) {
	fx := newSaleFixture(t)
	fx.hasProducts(catalogProduct(1, "6000", "0"), catalogProduct(2, "4000", "0"))
	fx.hasCombo("9000", inAnHour(), comboComponent(testComboID, 1, 2), comboComponent(testComboID, 2, 1))
	got := fx.expectPersist()

	// one bundle is 2 rice + 1 oil: 16,000 at normal prices for K 9,000... times 3
	_, err := fx.create(oneSale([]CreateSaleItemRequest{comboLine(1, 6, "0"), comboLine(2, 3, "0")}, "27000"))

	require.NoError(t, err)
	require.True(t, d("27000").Equal(got.sale.Total))
	require.True(t, d("27000").Equal(got.items[0].LineTotal.Add(got.items[1].LineTotal)))
}

// Three equal lines can't split K 1,000 into thirds: the odd 0.01 lands on the
// first (largest, tied) line so the bundle still totals exactly K 2,000.
func TestService_CreateSale_Combo_RoundingRemainderKeepsTheBundleAtTheComboPrice(t *testing.T) {
	fx := newSaleFixture(t)
	fx.hasProducts(catalogProduct(1, "1000", "0"), catalogProduct(2, "1000", "0"), catalogProduct(3, "1000", "0"))
	fx.hasCombo("2000", inAnHour(), comboComponent(testComboID, 1, 1), comboComponent(testComboID, 2, 1), comboComponent(testComboID, 3, 1))
	got := fx.expectPersist()

	_, err := fx.create(oneSale([]CreateSaleItemRequest{comboLine(1, 1, "0"), comboLine(2, 1, "0"), comboLine(3, 1, "0")}, "2000"))

	require.NoError(t, err)
	require.True(t, d("333.34").Equal(got.items[0].ComboDiscount), "got %s", got.items[0].ComboDiscount)
	require.True(t, d("333.33").Equal(got.items[1].ComboDiscount))
	require.True(t, d("2000").Equal(got.items[0].LineTotal.Add(got.items[1].LineTotal).Add(got.items[2].LineTotal)))
	require.True(t, d("2000").Equal(got.sale.Total))
}

func TestAllocateBundleDiscount_RemainderGoesToTheLargestLine_FirstOnATie(t *testing.T) {
	// 0.01 off K 500 splits to nothing by rounding; the cent goes to the first
	// of the two largest lines.
	shares := allocateBundleDiscount([]decimal.Decimal{d("100"), d("200"), d("200")}, d("499.99"))

	require.True(t, d("0").Equal(shares[0]))
	require.True(t, d("0.01").Equal(shares[1]))
	require.True(t, d("0").Equal(shares[2]))
}

// A bundle priced above its parts never raises the price and never gives a
// negative discount.
func TestService_CreateSale_ComboPricedAboveItsParts_GivesNoDiscount(t *testing.T) {
	fx := newSaleFixture(t)
	fx.hasProducts(catalogProduct(1, "6000", "0"), catalogProduct(2, "4000", "0"))
	fx.hasCombo("12000", inAnHour(), comboComponent(testComboID, 1, 1), comboComponent(testComboID, 2, 1))
	got := fx.expectPersist()

	_, err := fx.create(oneSale([]CreateSaleItemRequest{comboLine(1, 1, "0"), comboLine(2, 1, "0")}, "10000"))

	require.NoError(t, err)
	require.True(t, got.sale.Discount.IsZero())
	require.True(t, d("10000").Equal(got.sale.Total))
}

// A combo is priced as a bundle: the product's own catalog % does not stack on
// top of it.
func TestService_CreateSale_ComboLine_DoesNotAlsoGetTheCatalogPercent(t *testing.T) {
	fx := newSaleFixture(t)
	fx.hasProducts(catalogProduct(1, "1000", "10"))
	fx.hasCombo("950", inAnHour(), comboComponent(testComboID, 1, 1))
	got := fx.expectPersist()

	_, err := fx.create(oneSale([]CreateSaleItemRequest{comboLine(1, 1, "0")}, "950"))

	require.NoError(t, err)
	require.True(t, got.items[0].CatalogDiscount.IsZero())
	require.True(t, d("0").Equal(got.items[0].CatalogDiscountPercent))
	require.True(t, d("50").Equal(got.items[0].Discount))
	require.True(t, d("950").Equal(got.items[0].LineTotal))
}

// Tax is the tax inside what the customer pays for the line, i.e. after the
// bundle share, at that product's own rate.
func TestService_CreateSale_ComboLines_AreTaxedAfterTheirShareAtEachProductsOwnRate(t *testing.T) {
	fx := newSaleFixture(t)
	fx.hasProducts(taxed(catalogProduct(1, "6000", "0"), "5"), catalogProduct(2, "4000", "0"))
	fx.hasCombo("9000", inAnHour(), comboComponent(testComboID, 1, 1), comboComponent(testComboID, 2, 1))
	got := fx.expectPersist()

	_, err := fx.create(oneSale([]CreateSaleItemRequest{comboLine(1, 1, "0"), comboLine(2, 1, "0")}, "9000"))

	require.NoError(t, err)
	// 5,400 x 5 / 105
	require.True(t, d("257.14").Equal(got.items[0].Tax), "got %s", got.items[0].Tax)
	require.True(t, got.items[1].Tax.IsZero())
	require.True(t, d("257.14").Equal(got.sale.Tax))
}

// The same product on a combo line and on an ordinary line: each priced on its
// own terms.
func TestService_CreateSale_ComboLineAndAPlainLineOfTheSameProduct_AreBothPriced(t *testing.T) {
	fx := newSaleFixture(t)
	fx.hasRiceAndOil()
	got := fx.expectPersist()

	_, err := fx.create(oneSale([]CreateSaleItemRequest{comboLine(1, 1, "0"), comboLine(2, 1, "0"), line(1, 2)}, "21000"))

	require.NoError(t, err)
	require.True(t, got.items[2].ComboID == nil)
	require.True(t, d("12000").Equal(got.items[2].LineTotal))
	require.True(t, d("21000").Equal(got.sale.Total))
}

// The bundle is allocated by what each component costs at THIS branch, but it
// still totals the combo price.
func TestService_CreateSale_Combo_UsesTheBranchPriceToAllocateButStillTotalsTheComboPrice(t *testing.T) {
	fx := newSaleFixture(t)
	override := d("7000")
	fx.hasBranchPrice(catalogProduct(1, "6000", "0"), &catalog.ProductBranchPrice{ProductID: 1, BranchID: 3, Price: &override, SalePrice: override})
	fx.hasProducts(catalogProduct(2, "4000", "0"))
	fx.hasCombo("9000", inAnHour(), comboComponent(testComboID, 1, 1), comboComponent(testComboID, 2, 1))
	got := fx.expectPersist()

	_, err := fx.create(oneSale([]CreateSaleItemRequest{comboLine(1, 1, "0"), comboLine(2, 1, "0")}, "9000"))

	require.NoError(t, err)
	require.True(t, d("7000").Equal(got.items[0].UnitPrice))
	require.True(t, d("1272.73").Equal(got.items[0].ComboDiscount), "got %s", got.items[0].ComboDiscount)
	require.True(t, d("727.27").Equal(got.items[1].ComboDiscount))
	require.True(t, d("9000").Equal(got.sale.Total))
}

// Combos are loaded once per distinct combo_id, and their components in one
// query, however many lines the request has.
func TestService_CreateSale_Combos_AreLookedUpOncePerDistinctCombo(t *testing.T) {
	fx := newSaleFixture(t)
	fx.hasProducts(catalogProduct(1, "600", "0"), catalogProduct(2, "400", "0"), catalogProduct(3, "300", "0"))
	other := uint(6)
	fx.combos.EXPECT().GetCombo(mock.Anything, uint(7), testComboID).
		Return(&catalog.Combo{ID: testComboID, OrgID: 7, Price: d("800"), ExpiresAt: inAnHour()}, nil).Once()
	fx.combos.EXPECT().GetCombo(mock.Anything, uint(7), other).
		Return(&catalog.Combo{ID: other, OrgID: 7, Price: d("300"), ExpiresAt: inAnHour()}, nil).Once()
	fx.combos.EXPECT().ListComboItemsByComboIDs(mock.Anything, []uint{testComboID, other}).Return([]catalog.ComboItem{
		comboComponent(testComboID, 1, 1), comboComponent(testComboID, 3, 1), comboComponent(other, 2, 1),
	}, nil).Once()
	got := fx.expectPersist()
	b := CreateSaleItemRequest{ProductID: 2, ComboID: &other, NameSnapshot: "Oil", Qty: 1}

	_, err := fx.create(oneSale([]CreateSaleItemRequest{comboLine(1, 1, "0"), comboLine(3, 1, "0"), b}, "1100"))

	require.NoError(t, err)
	// combo 5: 900 normal for 800; combo 6: 400 normal for 300
	require.True(t, d("1100").Equal(got.sale.Total))
	require.True(t, d("200").Equal(got.sale.Discount))
}

// ---- offline-queued sales ----

// A fake combo can't be ingested either; only that sale errors.
func TestService_CreateSale_Queued_ComboThatDoesNotExist_IsRefused(t *testing.T) {
	fx := newSaleFixture(t)
	fx.expectNothingWritten()
	fx.hasProducts(catalogProduct(1, "6000", "0"))
	fx.combos.EXPECT().GetCombo(mock.Anything, uint(7), testComboID).Return(nil, common.NotFoundError("combo not found")).Once()

	_, err := fx.createQueuedSale(oneSale([]CreateSaleItemRequest{comboLine(1, 1, "6000")}, "0"))

	requireRestErrorStatus(t, err, http.StatusBadRequest)
	require.Contains(t, err.Error(), "combo 5 does not exist")
}

func TestService_CreateSale_Queued_ComboLinesThatDoNotMatchTheCombo_AreRefused(t *testing.T) {
	fx := newSaleFixture(t)
	fx.expectNothingWritten()
	fx.hasRiceAndOil()

	_, err := fx.createQueuedSale(oneSale([]CreateSaleItemRequest{comboLine(1, 1, "6000")}, "0"))

	requireRestErrorStatus(t, err, http.StatusBadRequest)
	require.Contains(t, err.Error(), "line for product 2 is missing")
}

// The combo changed price while the till was offline: the sale is taken, the
// bundle is priced at the combo's CURRENT price, and the difference is
// reported. The till collected its own total, which is accepted.
func TestService_CreateSale_Queued_ComboWhosePriceChanged_IsAcceptedAtTheCurrentPriceAndReported(t *testing.T) {
	fx := newSaleFixture(t)
	fx.hasRiceAndOil() // now K 9,000
	got := fx.expectPersist()
	// the till's cache still had it at K 9,500: 500 off in total
	a := comboLine(1, 1, "300")
	a.UnitPrice = d("6000")
	b := comboLine(2, 1, "200")
	b.UnitPrice = d("4000")

	sale, err := fx.createQueuedSale(oneSale([]CreateSaleItemRequest{a, b}, "9500"))

	require.NoError(t, err)
	require.True(t, d("9000").Equal(got.sale.Total), "priced at the combo's current price")
	require.Len(t, sale.PriceMismatches, 1)
	m := sale.PriceMismatches[0]
	require.NotNil(t, m.ComboID)
	require.Equal(t, testComboID, *m.ComboID)
	require.True(t, d("9500").Equal(*m.ClientBundleTotal))
	require.True(t, d("9000").Equal(*m.CurrentBundleTotal))
}

// A till that rang the combo up at the current price has nothing to report.
func TestService_CreateSale_Queued_ComboAtTheCurrentPrice_HasNoMismatch(t *testing.T) {
	fx := newSaleFixture(t)
	fx.hasRiceAndOil()
	fx.expectPersist()
	a := comboLine(1, 1, "600")
	a.UnitPrice = d("6000")
	b := comboLine(2, 1, "400")
	b.UnitPrice = d("4000")

	sale, err := fx.createQueuedSale(oneSale([]CreateSaleItemRequest{a, b}, "9000"))

	require.NoError(t, err)
	require.Empty(t, sale.PriceMismatches)
}

// The free-sale attempt offline: the claimed discount is not read. The sale is
// booked at the real bundle price (not for free) and the shortfall is reported
// as a conflict a manager sees.
func TestService_CreateSale_Queued_ComboWithAFullDiscountClaim_IsStillBookedAtTheComboPrice(t *testing.T) {
	fx := newSaleFixture(t)
	fx.hasRiceAndOil()
	got := fx.expectPersist()
	a := comboLine(1, 1, "6000")
	a.UnitPrice = d("6000")
	b := comboLine(2, 1, "4000")
	b.UnitPrice = d("4000")

	sale, err := fx.createQueuedSale(oneSale([]CreateSaleItemRequest{a, b}, "0"))

	require.NoError(t, err)
	require.True(t, d("9000").Equal(got.sale.Total), "the claim must not make the sale free")
	require.Len(t, sale.PriceMismatches, 1)
	require.True(t, sale.PriceMismatches[0].ClientBundleTotal.IsZero())
	require.True(t, d("9000").Equal(*sale.PriceMismatches[0].CurrentBundleTotal))
}

// A till can't shrink a bundle by inventing a low unit price either: combo
// lines are priced from the server's prices, not the till's cached ones.
func TestService_CreateSale_Queued_ComboLinesIgnoreTheTillsUnitPrice(t *testing.T) {
	fx := newSaleFixture(t)
	fx.hasRiceAndOil()
	got := fx.expectPersist()
	a := comboLine(1, 1, "0")
	a.UnitPrice = d("1")
	b := comboLine(2, 1, "0")
	b.UnitPrice = d("1")

	_, err := fx.createQueuedSale(oneSale([]CreateSaleItemRequest{a, b}, "2"))

	require.NoError(t, err)
	require.True(t, d("9000").Equal(got.sale.Total))
	require.True(t, d("6000").Equal(got.items[0].UnitPrice))
}

// Payments that match neither the server's total nor the till's own are still
// refused.
func TestService_CreateSale_Queued_PaymentsMatchingNeitherTotal_IsBadRequest(t *testing.T) {
	fx := newSaleFixture(t)
	fx.expectNothingWritten()
	fx.hasRiceAndOil()
	a := comboLine(1, 1, "300")
	a.UnitPrice = d("6000")
	b := comboLine(2, 1, "200")
	b.UnitPrice = d("4000")

	_, err := fx.createQueuedSale(oneSale([]CreateSaleItemRequest{a, b}, "500"))

	requireRestErrorStatus(t, err, http.StatusBadRequest)
}

// The combo expired while the till was offline, but the sale was rung up
// before it did: honoured.
func TestService_CreateSale_Queued_ComboSoldBeforeItExpired_IsAcceptedAfterwards(t *testing.T) {
	fx := newSaleFixture(t)
	fx.hasProducts(catalogProduct(1, "6000", "0"), catalogProduct(2, "4000", "0"))
	fx.hasCombo("9000", time.Now().Add(-time.Hour), comboComponent(testComboID, 1, 1), comboComponent(testComboID, 2, 1))
	fx.expectPersist()
	in := oneSale([]CreateSaleItemRequest{comboLine(1, 1, "0"), comboLine(2, 1, "0")}, "9000")
	rungUp := time.Now().Add(-2 * time.Hour)
	in.RungUpAt = &rungUp

	_, err := fx.createQueuedSale(in)

	require.NoError(t, err)
}

func TestService_CreateSale_Queued_ComboSoldAfterItExpired_IsRefused(t *testing.T) {
	fx := newSaleFixture(t)
	fx.expectNothingWritten()
	fx.hasProducts(catalogProduct(1, "6000", "0"), catalogProduct(2, "4000", "0"))
	fx.hasCombo("9000", time.Now().Add(-time.Hour), comboComponent(testComboID, 1, 1), comboComponent(testComboID, 2, 1))
	in := oneSale([]CreateSaleItemRequest{comboLine(1, 1, "0"), comboLine(2, 1, "0")}, "9000")
	rungUp := time.Now().Add(-30 * time.Minute)
	in.RungUpAt = &rungUp

	_, err := fx.createQueuedSale(in)

	requireRestErrorStatus(t, err, http.StatusBadRequest)
	require.Contains(t, err.Error(), "had expired")
}

// Without a rung-up time (or with one in the future, which a till clock can
// produce) the sale counts as rung up when the server got it.
func TestService_CreateSale_Queued_WithoutARungUpTime_ExpiryIsJudgedAtIngest(t *testing.T) {
	fx := newSaleFixture(t)
	fx.expectNothingWritten()
	fx.hasProducts(catalogProduct(1, "6000", "0"), catalogProduct(2, "4000", "0"))
	fx.hasCombo("9000", time.Now().Add(-time.Hour), comboComponent(testComboID, 1, 1), comboComponent(testComboID, 2, 1))
	in := oneSale([]CreateSaleItemRequest{comboLine(1, 1, "0"), comboLine(2, 1, "0")}, "9000")
	future := time.Now().Add(24 * time.Hour)
	in.RungUpAt = &future

	_, err := fx.createQueuedSale(in)

	requireRestErrorStatus(t, err, http.StatusBadRequest)
}

// RungUpAt belongs to queued sales only: a live sale can't back-date itself
// into an expired combo.
func TestService_CreateSale_Live_IgnoresRungUpAt(t *testing.T) {
	fx := newSaleFixture(t)
	fx.expectNothingWritten()
	fx.hasProducts(catalogProduct(1, "6000", "0"), catalogProduct(2, "4000", "0"))
	fx.hasCombo("9000", time.Now().Add(-time.Hour), comboComponent(testComboID, 1, 1), comboComponent(testComboID, 2, 1))
	in := oneSale([]CreateSaleItemRequest{comboLine(1, 1, "0"), comboLine(2, 1, "0")}, "9000")
	rungUp := time.Now().Add(-2 * time.Hour)
	in.RungUpAt = &rungUp

	_, err := fx.create(in)

	requireRestErrorStatus(t, err, http.StatusConflict)
}
