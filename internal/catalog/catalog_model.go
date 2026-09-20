package catalog

import (
	"time"

	"github.com/shopspring/decimal"
)

// TODO: relationships (belongs-to/has-many) are intentionally omitted here;
// wire them up as needed in repository.go queries.

// Product maps to the "Products" table in the ERD. Every product belongs to
// exactly one branch - there is no "shared across all branches" mode; the
// same real-world item at two branches is two separate Product rows. Barcode
// is unique per BranchID (not per OrgID) - see ux_products_branch_barcode -
// so the same barcode is expected to exist once per branch.
type Product struct {
	ID         uint            `gorm:"primaryKey;autoIncrement" json:"id"`
	OrgID      uint            `gorm:"index;not null" json:"org_id"`
	BranchID   uint            `gorm:"not null;uniqueIndex:ux_products_branch_barcode" json:"branch_id"`
	CategoryID uint            `gorm:"index;not null" json:"category_id"`
	Name       string          `gorm:"size:255;not null" json:"name"`
	Barcode    string          `gorm:"size:255;uniqueIndex:ux_products_branch_barcode;not null" json:"barcode"`
	Price      decimal.Decimal `gorm:"type:decimal(10,2);not null" json:"price"`
	Discount   decimal.Decimal `gorm:"type:decimal(10,2);not null" json:"discount"`
	Tax        decimal.Decimal `gorm:"type:decimal(10,2);not null" json:"tax"`
	Threshold  int             `gorm:"not null" json:"threshold"`
	IsActive   bool            `gorm:"not null" json:"is_active"`
	Modifier   *string         `gorm:"size:255" json:"modifier"`
	CreatedAt  time.Time       `gorm:"autoCreateTime;not null" json:"created_at"`
	UpdatedAt  time.Time       `gorm:"autoUpdateTime;not null" json:"updated_at"`
}

// Category maps to the "categories" table in the ERD. NameI18n is unique per
// OrgID - two categories in the same org can't share a name (case-sensitive;
// different orgs can reuse the same name freely).
type Category struct {
	ID       uint   `gorm:"primaryKey;autoIncrement" json:"id"`
	OrgID    uint   `gorm:"index;not null;uniqueIndex:ux_categories_org_name" json:"org_id"`
	NameI18n string `gorm:"size:255;not null;uniqueIndex:ux_categories_org_name" json:"name_i18n"`
}

// ProductImage maps to the "Product_images" table in the ERD.
type ProductImage struct {
	ID         uint   `gorm:"primaryKey;autoIncrement" json:"id"`
	ProductID  uint   `gorm:"index;not null" json:"product_id"`
	StorageKey string `gorm:"size:500;not null" json:"storage_key"`
	Width      int    `gorm:"not null" json:"width"`
	Height     int    `gorm:"not null" json:"height"`
}

// Combo maps to the "Combos" table in the ERD.
type Combo struct {
	ID        uint            `gorm:"primaryKey;autoIncrement" json:"id"`
	OrgID     uint            `gorm:"index;not null" json:"org_id"`
	Name      string          `gorm:"size:255;not null" json:"name"`
	Price     decimal.Decimal `gorm:"type:decimal(10,2);not null" json:"price"`
	ExpiresAt time.Time       `gorm:"not null" json:"expires_at"`
}

// ComboItem maps to the "Combo_items" table in the ERD.
type ComboItem struct {
	ID        uint `gorm:"primaryKey;autoIncrement" json:"id"`
	ProductID uint `gorm:"index;not null" json:"product_id"`
	ComboID   uint `gorm:"index;not null" json:"combo_id"`
	Qty       int  `gorm:"not null" json:"qty"`
}
