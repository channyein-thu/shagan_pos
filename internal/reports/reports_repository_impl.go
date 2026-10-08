package reports

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

type RepositoryImpl struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) Repository {
	return &RepositoryImpl{db: db}
}

var _ Repository = (*RepositoryImpl)(nil)

// completedSalesWhere is the shared population filter every headline figure
// is built from - status NOT IN (voided, open): a voided sale is excluded
// entirely (a void erases the whole transaction, see docs/WORKFLOWS.md),
// and an open sale is an unfinished cart, not a real transaction yet.
const completedSalesWhere = "status NOT IN ('voided', 'open') AND completed_at >= ? AND completed_at < ?"

func (r *RepositoryImpl) SalesAggregate(ctx context.Context, orgID uint, branchID *uint, from, to time.Time) (SalesAggregate, error) {
	var row SalesAggregate
	q := r.db.WithContext(ctx).Table("sales").
		Select("COALESCE(SUM(subtotal), 0) AS gross, COALESCE(SUM(discount), 0) AS discounts, COUNT(*) AS count").
		Where("org_id = ? AND "+completedSalesWhere, orgID, from, to)
	if branchID != nil {
		q = q.Where("branch_id = ?", *branchID)
	}
	if err := q.Scan(&row).Error; err != nil {
		return SalesAggregate{}, err
	}
	return row, nil
}

func (r *RepositoryImpl) ReturnsTotal(ctx context.Context, orgID uint, branchID *uint, from, to time.Time) (decimal.Decimal, error) {
	var row struct{ Total decimal.Decimal }
	q := r.db.WithContext(ctx).Table("returns").
		Select("COALESCE(SUM(returns.refund_total), 0) AS total").
		Joins("JOIN sales ON sales.id = returns.sale_id").
		Where("sales.org_id = ? AND returns.created_at >= ? AND returns.created_at < ?", orgID, from, to)
	if branchID != nil {
		q = q.Where("sales.branch_id = ?", *branchID)
	}
	if err := q.Scan(&row).Error; err != nil {
		return decimal.Zero, err
	}
	return row.Total, nil
}

func (r *RepositoryImpl) HourlyTrend(ctx context.Context, orgID uint, branchID *uint, dayStart, dayEnd time.Time) ([]TrendBucket, error) {
	var rows []TrendBucket
	q := r.db.WithContext(ctx).Table("sales").
		Select("to_char(date_trunc('hour', completed_at), 'HH24:00') AS period, "+
			"COALESCE(SUM(subtotal - discount), 0) AS revenue, COUNT(*) AS count").
		Where("org_id = ? AND "+completedSalesWhere, orgID, dayStart, dayEnd).
		Group("date_trunc('hour', completed_at)").
		Order("date_trunc('hour', completed_at)")
	if branchID != nil {
		q = q.Where("branch_id = ?", *branchID)
	}
	if err := q.Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// dateTruncUnit maps Granularity to the Postgres date_trunc unit it buckets
// by. Only ever called with one of the three Granularity constants (the
// service validates the query param against them first), so it's safe to
// interpolate directly into SQL - never raw client input.
func dateTruncUnit(g Granularity) string {
	switch g {
	case GranularityWeekly:
		return "week"
	case GranularityMonthly:
		return "month"
	default:
		return "day"
	}
}

func (r *RepositoryImpl) Trend(ctx context.Context, orgID uint, branchID *uint, from, to time.Time, granularity Granularity) ([]TrendBucket, error) {
	trunc := "date_trunc('" + dateTruncUnit(granularity) + "', completed_at)"
	var rows []TrendBucket
	q := r.db.WithContext(ctx).Table("sales").
		Select("to_char("+trunc+", 'YYYY-MM-DD') AS period, "+
			"COALESCE(SUM(subtotal - discount), 0) AS revenue, COUNT(*) AS count").
		Where("org_id = ? AND "+completedSalesWhere, orgID, from, to).
		Group(trunc).
		Order(trunc)
	if branchID != nil {
		q = q.Where("branch_id = ?", *branchID)
	}
	if err := q.Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *RepositoryImpl) BranchBreakdown(ctx context.Context, orgID uint, branchID *uint, from, to time.Time) ([]BranchBreakdown, error) {
	var rows []BranchBreakdown
	q := r.db.WithContext(ctx).Table("sales").
		Select("sales.branch_id AS branch_id, branches.name AS branch_name, "+
			"COALESCE(SUM(sales.subtotal - sales.discount), 0) AS net_sales").
		Joins("JOIN branches ON branches.id = sales.branch_id").
		Where("sales.org_id = ? AND sales."+completedSalesWhere, orgID, from, to).
		Group("sales.branch_id, branches.name").
		Order("net_sales DESC")
	if branchID != nil {
		q = q.Where("sales.branch_id = ?", *branchID)
	}
	if err := q.Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *RepositoryImpl) CategoryBreakdown(ctx context.Context, orgID uint, branchID *uint, from, to time.Time) ([]CategoryBreakdown, error) {
	var rows []CategoryBreakdown
	q := r.db.WithContext(ctx).Table("sale_items").
		Select("products.category_id AS category_id, categories.name_i18n AS category_name, "+
			"COALESCE(SUM(sale_items.line_total), 0) AS net_sales").
		Joins("JOIN sales ON sales.id = sale_items.sale_id").
		Joins("JOIN products ON products.id = sale_items.product_id").
		Joins("JOIN categories ON categories.id = products.category_id").
		Where("sales.org_id = ? AND sales."+completedSalesWhere, orgID, from, to).
		Group("products.category_id, categories.name_i18n").
		Order("net_sales DESC")
	if branchID != nil {
		q = q.Where("sales.branch_id = ?", *branchID)
	}
	if err := q.Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *RepositoryImpl) PaymentMethodBreakdown(ctx context.Context, orgID uint, branchID *uint, from, to time.Time) ([]PaymentMethodBreakdown, error) {
	var rows []PaymentMethodBreakdown
	q := r.db.WithContext(ctx).Table("payments").
		Select("payments.method AS method, COALESCE(SUM(payments.amount), 0) AS amount, COUNT(*) AS count").
		Joins("JOIN sales ON sales.id = payments.sale_id").
		Where("sales.org_id = ? AND sales."+completedSalesWhere, orgID, from, to).
		Group("payments.method").
		Order("amount DESC")
	if branchID != nil {
		q = q.Where("sales.branch_id = ?", *branchID)
	}
	if err := q.Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *RepositoryImpl) ListTransactions(ctx context.Context, orgID uint, branchID *uint, from, to time.Time, page, pageSize int) ([]TransactionSummary, int64, error) {
	base := r.db.WithContext(ctx).Table("sales").
		Where("org_id = ? AND "+completedSalesWhere, orgID, from, to)
	if branchID != nil {
		base = base.Where("branch_id = ?", *branchID)
	}

	var total int64
	if err := base.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var rows []TransactionSummary
	err := base.Session(&gorm.Session{}).
		Select("id, branch_id, staff_id, total, status, completed_at").
		Order("completed_at DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Scan(&rows).Error
	if err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

func (r *RepositoryImpl) PaymentMethodsBySale(ctx context.Context, saleIDs []uuid.UUID) (map[uuid.UUID][]string, error) {
	out := make(map[uuid.UUID][]string, len(saleIDs))
	if len(saleIDs) == 0 {
		return out, nil
	}
	var rows []struct {
		SaleID uuid.UUID
		Method string
	}
	err := r.db.WithContext(ctx).Table("payments").
		Select("DISTINCT sale_id, method").
		Where("sale_id IN ?", saleIDs).
		Order("sale_id, method").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		out[row.SaleID] = append(out[row.SaleID], row.Method)
	}
	return out, nil
}

func (r *RepositoryImpl) ProductSales(ctx context.Context, orgID uint, branchID *uint, from, to time.Time, categoryID *uint, limit *int) ([]ProductRevenue, error) {
	var rows []ProductRevenue
	q := r.db.WithContext(ctx).Table("sale_items").
		Select("sale_items.product_id AS product_id, products.name AS name, "+
			"COALESCE(SUM(sale_items.qty), 0) AS qty_sold, COALESCE(SUM(sale_items.line_total), 0) AS revenue").
		Joins("JOIN sales ON sales.id = sale_items.sale_id").
		Joins("JOIN products ON products.id = sale_items.product_id").
		Where("sales.org_id = ? AND sales."+completedSalesWhere, orgID, from, to).
		Group("sale_items.product_id, products.name").
		Order("revenue DESC")
	if branchID != nil {
		q = q.Where("sales.branch_id = ?", *branchID)
	}
	if categoryID != nil {
		q = q.Where("products.category_id = ?", *categoryID)
	}
	if limit != nil {
		q = q.Limit(*limit)
	}
	if err := q.Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *RepositoryImpl) ProductCounts(ctx context.Context, orgID uint, branchID *uint) (ProductCounts, error) {
	var counts ProductCounts

	// Products are org-wide (see catalog.Product's doc), so a branch filter
	// here means "products actually stocked at this branch" - counted via a
	// distinct join through stock_levels, same shape as lowQ/outQ below -
	// rather than a direct products.branch_id column, which no longer
	// exists.
	var total int64
	if branchID != nil {
		totalQ := r.db.WithContext(ctx).Table("stock_levels").
			Joins("JOIN products ON products.id = stock_levels.product_id").
			Where("products.org_id = ? AND stock_levels.branch_id = ?", orgID, *branchID).
			Distinct("products.id")
		if err := totalQ.Count(&total).Error; err != nil {
			return ProductCounts{}, err
		}
	} else {
		if err := r.db.WithContext(ctx).Table("products").Where("org_id = ?", orgID).Count(&total).Error; err != nil {
			return ProductCounts{}, err
		}
	}
	counts.TotalProducts = int(total)

	lowQ := r.db.WithContext(ctx).Table("stock_levels").
		Joins("JOIN products ON products.id = stock_levels.product_id").
		Where("products.org_id = ? AND stock_levels.qty <= products.threshold", orgID)
	if branchID != nil {
		lowQ = lowQ.Where("stock_levels.branch_id = ?", *branchID)
	}
	var low int64
	if err := lowQ.Count(&low).Error; err != nil {
		return ProductCounts{}, err
	}
	counts.LowStockCount = int(low)

	outQ := r.db.WithContext(ctx).Table("stock_levels").
		Joins("JOIN products ON products.id = stock_levels.product_id").
		Where("products.org_id = ? AND stock_levels.qty <= 0", orgID)
	if branchID != nil {
		outQ = outQ.Where("stock_levels.branch_id = ?", *branchID)
	}
	var out int64
	if err := outQ.Count(&out).Error; err != nil {
		return ProductCounts{}, err
	}
	counts.OutOfStockCount = int(out)

	return counts, nil
}

func (r *RepositoryImpl) COGS(ctx context.Context, orgID uint, branchID *uint, from, to time.Time) (decimal.Decimal, error) {
	var gross struct{ Total decimal.Decimal }
	grossQ := r.db.WithContext(ctx).Table("sale_items").
		Select("COALESCE(SUM(sale_items.unit_cost * sale_items.qty), 0) AS total").
		Joins("JOIN sales ON sales.id = sale_items.sale_id").
		Where("sales.org_id = ? AND sales."+completedSalesWhere, orgID, from, to)
	if branchID != nil {
		grossQ = grossQ.Where("sales.branch_id = ?", *branchID)
	}
	if err := grossQ.Scan(&gross).Error; err != nil {
		return decimal.Zero, err
	}

	var returned struct{ Total decimal.Decimal }
	returnedQ := r.db.WithContext(ctx).Table("return_items").
		Select("COALESCE(SUM(sale_items.unit_cost * return_items.qty), 0) AS total").
		Joins("JOIN sale_items ON sale_items.id = return_items.sale_item_id").
		Joins("JOIN returns ON returns.id = return_items.return_id").
		Joins("JOIN sales ON sales.id = sale_items.sale_id").
		Where("sales.org_id = ? AND returns.created_at >= ? AND returns.created_at < ?", orgID, from, to)
	if branchID != nil {
		returnedQ = returnedQ.Where("sales.branch_id = ?", *branchID)
	}
	if err := returnedQ.Scan(&returned).Error; err != nil {
		return decimal.Zero, err
	}

	return gross.Total.Sub(returned.Total), nil
}

func (r *RepositoryImpl) Expenses(ctx context.Context, branchIDs []uint, from, to time.Time) (decimal.Decimal, error) {
	var row struct{ Total decimal.Decimal }
	err := r.db.WithContext(ctx).Table("expenses").
		Select("COALESCE(SUM(amount), 0) AS total").
		Where("branch_id IN (?) AND date >= ? AND date < ?", branchIDs, from, to).
		Scan(&row).Error
	if err != nil {
		return decimal.Zero, err
	}
	return row.Total, nil
}
