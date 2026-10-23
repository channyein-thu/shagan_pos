//go:build cgo

package catalog

import (
	"fmt"
	"strings"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func newMigrateTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	name := strings.NewReplacer("/", "_", " ", "_").Replace(t.Name())
	db, err := gorm.Open(
		sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", name)),
		&gorm.Config{Logger: logger.Default.LogMode(logger.Silent)},
	)
	require.NoError(t, err)
	return db
}

// legacyProductsTable is products as it was before the percent: a fixed
// per-unit amount discount and no sale_price.
func legacyProductsTable(t *testing.T, db *gorm.DB) {
	t.Helper()
	require.NoError(t, db.Exec(`CREATE TABLE products (
		id integer primary key autoincrement, org_id integer not null, category_id integer not null,
		name text not null, barcode text not null, price decimal(10,2) not null, discount decimal(10,2) not null,
		tax decimal(10,2) not null, threshold integer not null, is_active boolean not null, modifier text,
		cost_price decimal(10,2) not null default 0, created_at datetime, updated_at datetime)`).Error)
}

func insertLegacy(t *testing.T, db *gorm.DB, barcode, price, discount string) {
	t.Helper()
	require.NoError(t, db.Exec(`INSERT INTO products (org_id, category_id, name, barcode, price, discount, tax, threshold, is_active)
		VALUES (1, 1, ?, ?, ?, ?, 0, 1, 1)`, barcode, barcode, price, discount).Error)
}

func insertLegacyWithTax(t *testing.T, db *gorm.DB, barcode, price, discount, tax string) {
	t.Helper()
	require.NoError(t, db.Exec(`INSERT INTO products (org_id, category_id, name, barcode, price, discount, tax, threshold, is_active)
		VALUES (1, 1, ?, ?, ?, ?, ?, 1, 1)`, barcode, barcode, price, discount, tax).Error)
}

func loadProduct(t *testing.T, db *gorm.DB, barcode string) Product {
	t.Helper()
	var p Product
	require.NoError(t, db.Where("barcode = ?", barcode).First(&p).Error)
	return p
}

// Existing products must keep their exact prices: today's sale price is
// price - discount, and the percent is just that discount expressed as a %.
// The old amount is never re-rounded to K 100.
func TestAutoMigrate_BackfillsLegacyProductsKeepingExactPrices(t *testing.T) {
	db := newMigrateTestDB(t)
	legacyProductsTable(t, db)
	insertLegacy(t, db, "A", "6500.00", "300.00") // 4.62%
	insertLegacy(t, db, "B", "6750.00", "0.00")   // no discount
	insertLegacy(t, db, "C", "1250.50", "125.05") // exactly 10%
	insertLegacy(t, db, "D", "500.00", "500.00")  // fully free

	require.NoError(t, AutoMigrate(db))

	a, b, c, d := loadProduct(t, db, "A"), loadProduct(t, db, "B"), loadProduct(t, db, "C"), loadProduct(t, db, "D")
	require.True(t, decimal.RequireFromString("6200").Equal(a.SalePrice), "A sale price %s", a.SalePrice)
	require.True(t, decimal.RequireFromString("4.62").Equal(a.DiscountPercent), "A percent %s", a.DiscountPercent)
	require.True(t, decimal.RequireFromString("6750").Equal(b.SalePrice))
	require.True(t, b.DiscountPercent.IsZero())
	require.True(t, decimal.RequireFromString("1125.45").Equal(c.SalePrice), "C sale price %s", c.SalePrice)
	require.True(t, decimal.RequireFromString("10").Equal(c.DiscountPercent), "C percent %s", c.DiscountPercent)
	require.True(t, d.SalePrice.IsZero())
	require.True(t, decimal.RequireFromString("100").Equal(d.DiscountPercent))
	// the per-unit amount (and so price - sale_price) is untouched
	require.True(t, decimal.RequireFromString("300").Equal(a.Discount))
	require.True(t, a.Price.Sub(a.SalePrice).Equal(a.Discount), "price - sale_price still equals the old discount")
}

func TestAutoMigrate_BackfillRunTwiceDoesNotDisturbAnEditedProduct(t *testing.T) {
	db := newMigrateTestDB(t)
	legacyProductsTable(t, db)
	insertLegacy(t, db, "A", "6500.00", "0.00")
	require.NoError(t, AutoMigrate(db))
	// the Owner later sets 10%: the server stores 5,900 / 600
	require.NoError(t, db.Model(&Product{}).Where("barcode = ?", "A").Updates(map[string]any{
		"discount_percent": 10, "sale_price": 5900, "discount": 600,
	}).Error)

	require.NoError(t, AutoMigrate(db))

	a := loadProduct(t, db, "A")
	require.True(t, decimal.NewFromInt(10).Equal(a.DiscountPercent), "percent stays the Owner's 10, not re-derived from the amount: %s", a.DiscountPercent)
	require.True(t, decimal.NewFromInt(5900).Equal(a.SalePrice))
}

func TestAutoMigrate_OnAFreshDatabaseIsANoOpBackfill(t *testing.T) {
	db := newMigrateTestDB(t)

	require.NoError(t, AutoMigrate(db))
	require.NoError(t, AutoMigrate(db))
}

// Tax V1: a product's old fixed tax amount becomes a rate (tax / price x 100),
// and the PRICE IS LEFT ALONE - the shelf price now simply contains the tax.
// A tax-free product gets 0%. The informational per-unit tax is re-derived as
// the part of the sale price that is tax.
func TestAutoMigrate_ConvertsTheOldTaxAmountToAPercentWithoutTouchingPrices(t *testing.T) {
	db := newMigrateTestDB(t)
	legacyProductsTable(t, db)
	insertLegacyWithTax(t, db, "A", "800.00", "0.00", "40.00")     // 5.00%
	insertLegacyWithTax(t, db, "B", "6500.00", "300.00", "325.00") // 5.00% of the LIST price; sells at 6,200
	insertLegacyWithTax(t, db, "C", "1250.50", "0.00", "100.00")   // 8.00% (7.9968 -> 8.00)
	insertLegacyWithTax(t, db, "D", "400.00", "100.00", "0.00")    // tax-free

	require.NoError(t, AutoMigrate(db))

	a, b, c, d := loadProduct(t, db, "A"), loadProduct(t, db, "B"), loadProduct(t, db, "C"), loadProduct(t, db, "D")
	require.True(t, decimal.RequireFromString("5").Equal(a.TaxPercent), "A rate %s", a.TaxPercent)
	require.True(t, decimal.RequireFromString("5").Equal(b.TaxPercent), "B rate %s", b.TaxPercent)
	require.True(t, decimal.RequireFromString("8").Equal(c.TaxPercent), "C rate %s", c.TaxPercent)
	require.True(t, d.TaxPercent.IsZero())

	// prices (and the sale price Discount V1 derived) did not move
	require.True(t, decimal.RequireFromString("800").Equal(a.Price))
	require.True(t, decimal.RequireFromString("800").Equal(a.SalePrice))
	require.True(t, decimal.RequireFromString("6500").Equal(b.Price))
	require.True(t, decimal.RequireFromString("6200").Equal(b.SalePrice))
	require.True(t, decimal.RequireFromString("300").Equal(d.SalePrice))

	// the informational per-unit tax is now the tax INSIDE the sale price
	require.True(t, decimal.RequireFromString("38.10").Equal(a.Tax), "A tax %s", a.Tax)  // 800 x 5 / 105
	require.True(t, decimal.RequireFromString("295.24").Equal(b.Tax), "B tax %s", b.Tax) // 6,200 x 5 / 105
	require.True(t, d.Tax.IsZero())
}

// It must not run again: after the Owner edits the rate, a restart cannot
// turn it back into whatever the old amount implied.
func TestAutoMigrate_TaxConversionRunsOnlyOnce(t *testing.T) {
	db := newMigrateTestDB(t)
	legacyProductsTable(t, db)
	insertLegacyWithTax(t, db, "A", "800.00", "0.00", "40.00")
	require.NoError(t, AutoMigrate(db))
	// the Owner sets 7% and the server stores 800 x 7 / 107
	require.NoError(t, db.Model(&Product{}).Where("barcode = ?", "A").Updates(map[string]any{"tax_percent": 7, "tax": "52.34"}).Error)
	// and a product still holding an old amount appears later (e.g. a demo-data load)
	insertLegacyWithTax(t, db, "B", "800.00", "0.00", "40.00")

	require.NoError(t, AutoMigrate(db))
	require.NoError(t, AutoMigrate(db))

	a, b := loadProduct(t, db, "A"), loadProduct(t, db, "B")
	require.True(t, decimal.NewFromInt(7).Equal(a.TaxPercent), "the Owner's 7%% survives a restart (got %s)", a.TaxPercent)
	require.True(t, decimal.RequireFromString("52.34").Equal(a.Tax))
	require.True(t, b.TaxPercent.IsZero(), "a later row is not converted by a second pass (got %s)", b.TaxPercent)
}

// An old amount bigger than the price would be a rate over 100%: it is capped
// rather than stored as nonsense the product form would then reject.
func TestAutoMigrate_TaxConversionCapsAtOneHundredPercent(t *testing.T) {
	db := newMigrateTestDB(t)
	legacyProductsTable(t, db)
	insertLegacyWithTax(t, db, "A", "100.00", "0.00", "250.00")

	require.NoError(t, AutoMigrate(db))

	require.True(t, decimal.NewFromInt(100).Equal(loadProduct(t, db, "A").TaxPercent))
}
