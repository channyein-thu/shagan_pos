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
