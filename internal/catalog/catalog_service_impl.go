package catalog

import (
	"context"
	"fmt"
	"image"
	_ "image/gif"  // registers GIF decoding with image.DecodeConfig
	_ "image/jpeg" // registers JPEG decoding with image.DecodeConfig
	_ "image/png"  // registers PNG decoding with image.DecodeConfig
	"io"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"shagan_pos/internal/audit"
	"shagan_pos/internal/common"
	"shagan_pos/internal/identity"
	"shagan_pos/internal/storage"
)

// DefaultImageURLTTL is how long a presigned product-image URL stays valid -
// long enough for a client to load the image, short enough that a
// leaked/cached link doesn't work forever.
const DefaultImageURLTTL = 15 * time.Minute

// validateProductMoney enforces the business rules decimal.Decimal can't get
// from struct tags (see CreateProductRequest's doc): Price must be positive,
// and the discount and tax percents must each be within 0-100 (a discount over
// 100 would make the sale price negative; a tax over 100 is not a rate).
func validateProductMoney(price, discountPercent, taxPercent decimal.Decimal) error {
	if !price.IsPositive() {
		return common.BadRequestError("price must be greater than zero")
	}
	if discountPercent.IsNegative() || discountPercent.GreaterThan(decimal.NewFromInt(100)) {
		return common.BadRequestError("discount_percent must be between 0 and 100")
	}
	if taxPercent.IsNegative() || taxPercent.GreaterThan(decimal.NewFromInt(100)) {
		return common.BadRequestError("tax_percent must be between 0 and 100")
	}
	return nil
}

// productTax is the tax inside one unit at the sale price. The rate is rounded
// to the 2 decimals it is stored with first, like the discount percent.
func productTax(salePrice, taxPercent decimal.Decimal) (percent, tax decimal.Decimal) {
	percent = taxPercent.Round(2)
	return percent, common.IncludedTax(salePrice, percent)
}

// productPricing is the sale price and per-unit discount amount the server
// derives from a price and a catalog percent. The percent is rounded to the 2
// decimals it is stored with first, so the stored percent and the stored
// sale price always agree.
func productPricing(price, discountPercent decimal.Decimal) (percent, salePrice, discount decimal.Decimal) {
	percent = discountPercent.Round(2)
	salePrice = common.DiscountedUnitPrice(price, percent)
	return percent, salePrice, price.Sub(salePrice)
}

type Service struct {
	repo     Repository
	branches BranchLookup
	audit    AuditWriter
	db       common.Transactioner
	storage  storage.Storage
}

func NewService(repo Repository, branches BranchLookup, auditWriter AuditWriter, db common.Transactioner, store storage.Storage) *Service {
	return &Service{repo: repo, branches: branches, audit: auditWriter, db: db, storage: store}
}

var _ Interface = (*Service)(nil)

func (s *Service) ListProducts(ctx context.Context, orgID uint, branchID *uint) ([]ProductResult, error) {
	products, err := s.repo.ListProducts(ctx, orgID)
	if err != nil {
		return nil, err
	}
	results, err := s.attachImages(ctx, products)
	if err != nil {
		return nil, err
	}
	if err := s.resolveForBranch(ctx, orgID, branchID, results); err != nil {
		return nil, err
	}
	return results, nil
}

func (s *Service) GetProduct(ctx context.Context, orgID uint, id uint, branchID *uint) (*ProductResult, error) {
	product, err := s.repo.GetProduct(ctx, orgID, id)
	if err != nil {
		return nil, err
	}
	return s.productResult(ctx, orgID, branchID, product)
}

// productResult is the single-product tail shared by GetProduct and
// GetProductByBarcode: images, then pricing for branchID.
func (s *Service) productResult(ctx context.Context, orgID uint, branchID *uint, product *Product) (*ProductResult, error) {
	results, err := s.attachImages(ctx, []Product{*product})
	if err != nil {
		return nil, err
	}
	if err := s.resolveForBranch(ctx, orgID, branchID, results); err != nil {
		return nil, err
	}
	return &results[0], nil
}

// resolveForBranch rewrites results in place to what each product costs at
// branchID (the branch's override where one exists, else the catalog value)
// and records where it came from. A nil branchID leaves the catalog row as it
// is and sets no price_source. The branch must belong to orgID - a cross-org
// id is a not-found, not a silent fall back to catalog prices.
func (s *Service) resolveForBranch(ctx context.Context, orgID uint, branchID *uint, results []ProductResult) error {
	if branchID == nil {
		return nil
	}
	if _, err := s.branches.GetBranch(ctx, orgID, *branchID); err != nil {
		return err
	}
	products := make([]Product, len(results))
	for i := range results {
		products[i] = results[i].Product
	}
	pricings, err := ResolveBranchPricingBatch(ctx, s.repo, orgID, *branchID, products)
	if err != nil {
		return err
	}
	for i := range results {
		applyPricing(&results[i].Product, pricings[i])
		results[i].PriceSource = pricings[i].Source
	}
	return nil
}

// attachImages assembles each product's ProductImageResult list - fetching
// every image row for the given products in one query, then generating a
// temporary signed URL per image (see DefaultImageURLTTL), since StorageKey
// alone isn't usable by a client (the bucket is private).
func (s *Service) attachImages(ctx context.Context, products []Product) ([]ProductResult, error) {
	results := make([]ProductResult, len(products))
	if len(products) == 0 {
		return results, nil
	}

	ids := make([]uint, len(products))
	for i, p := range products {
		ids[i] = p.ID
	}

	images, err := s.repo.ListProductImagesByProductIDs(ctx, ids)
	if err != nil {
		return nil, err
	}

	byProduct := make(map[uint][]ProductImage, len(products))
	for _, img := range images {
		byProduct[img.ProductID] = append(byProduct[img.ProductID], img)
	}

	for i, p := range products {
		imgResults := make([]ProductImageResult, 0, len(byProduct[p.ID]))
		for _, img := range byProduct[p.ID] {
			url, err := s.storage.PresignedURL(ctx, img.StorageKey, DefaultImageURLTTL)
			if err != nil {
				return nil, common.SystemError("failed to generate image URL")
			}
			imgResults = append(imgResults, ProductImageResult{
				ID:     img.ID,
				URL:    url,
				Width:  img.Width,
				Height: img.Height,
			})
		}
		results[i] = ProductResult{Product: p, Images: imgResults}
	}

	return results, nil
}

func (s *Service) GetProductByBarcode(ctx context.Context, orgID uint, code string, branchID *uint) (*ProductResult, error) {
	product, err := s.repo.GetProductByBarcode(ctx, orgID, code)
	if err != nil {
		return nil, err
	}
	return s.productResult(ctx, orgID, branchID, product)
}

// CreateProduct confirms in.CategoryID belongs to orgID (same
// not-found-not-forbidden reasoning as identity's branch-ownership checks -
// a product can't be attached to another org's category), enforces the
// price/discount/tax rules, decodes the uploaded image's real pixel
// dimensions, then creates the Product and its ProductImage together as one
// atomic unit of work: a product without its required image should never
// exist. A duplicate barcode within the same org (ux_products_org_barcode)
// surfaces as common.ConflictError - same pattern as CreateCategory's
// duplicate-name handling.
func (s *Service) CreateProduct(ctx context.Context, orgID uint, in CreateProductRequest, file io.ReadSeeker, fileSize int64, contentType, filename string) (*Product, error) {
	if _, err := s.repo.GetCategory(ctx, orgID, in.CategoryID); err != nil {
		return nil, err
	}

	if err := validateProductMoney(in.Price, in.DiscountPercent, in.TaxPercent); err != nil {
		return nil, err
	}
	percent, salePrice, discount := productPricing(in.Price, in.DiscountPercent)
	taxPercent, tax := productTax(salePrice, in.TaxPercent)

	// image.DecodeConfig only reads the header (not the full pixel data) to
	// get Width/Height, but it still consumes bytes from file - rewind
	// before the full content gets uploaded below.
	cfg, _, err := image.DecodeConfig(file)
	if err != nil {
		return nil, common.BadRequestError("uploaded file is not a valid image")
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, common.SystemError("failed to rewind uploaded image")
	}

	var result *Product
	err = s.db.Transaction(func(tx *gorm.DB) error {
		product := Product{
			OrgID:      orgID,
			CategoryID: in.CategoryID,
			Name:       in.Name,
			Barcode:    in.Barcode,
			Price:      in.Price,
			Threshold:  in.Threshold,
			IsActive:   in.IsActive,
			Modifier:   in.Modifier,

			DiscountPercent: percent,
			SalePrice:       salePrice,
			Discount:        discount,
			TaxPercent:      taxPercent,
			Tax:             tax,
		}
		if err := s.repo.CreateProduct(tx, &product); err != nil {
			if common.IsDuplicateError(err) {
				return common.ConflictError("a product with this barcode already exists")
			}
			return err
		}

		// keyed by the new product's ID, so it's only known once the row
		// above exists.
		key := fmt.Sprintf("products/%d/%s", product.ID, filename)
		if err := s.storage.Upload(ctx, key, file, fileSize, contentType); err != nil {
			return common.SystemError("failed to upload product image")
		}

		productImage := ProductImage{
			ProductID:  product.ID,
			StorageKey: key,
			Width:      cfg.Width,
			Height:     cfg.Height,
		}
		if err := s.repo.CreateProductImage(tx, &productImage); err != nil {
			// the DB write failed, so tx rolls back the Product row - but
			// that can't undo the object storage upload above (a different
			// system, outside this SQL transaction), so clean it up here to
			// avoid leaving an orphaned object with no row referencing it.
			_ = s.storage.Delete(ctx, key)
			return err
		}

		result = &product
		return nil
	})
	if err != nil {
		return nil, err
	}

	return result, nil
}

func (s *Service) UpdateProduct(ctx context.Context, orgID uint, id uint, in UpdateProductRequest, file io.ReadSeeker, fileSize int64, contentType, filename string) (*Product, error) {
	product, err := s.repo.GetProduct(ctx, orgID, id)
	if err != nil {
		return nil, err
	}

	if in.CategoryID != nil {
		if _, err := s.repo.GetCategory(ctx, orgID, *in.CategoryID); err != nil {
			return nil, err
		}
	}

	// Price and the percent decide the sale price together, so either one
	// changing recomputes it from the (new or existing) other.
	pricingChanged := in.Price != nil || in.DiscountPercent != nil
	price, percent, taxPercent := product.Price, product.DiscountPercent, product.TaxPercent
	if in.Price != nil {
		price = *in.Price
	}
	if in.DiscountPercent != nil {
		percent = *in.DiscountPercent
	}
	if in.TaxPercent != nil {
		taxPercent = *in.TaxPercent
	}
	if pricingChanged || in.TaxPercent != nil {
		if err := validateProductMoney(price, percent, taxPercent); err != nil {
			return nil, err
		}
	}
	if in.CostPrice != nil && in.CostPrice.IsNegative() {
		return nil, common.BadRequestError("cost_price must be zero or greater")
	}

	var cfg image.Config
	if file != nil {
		var err error
		// image.DecodeConfig only reads the header (not the full pixel
		// data) to get Width/Height, but it still consumes bytes from file -
		// rewind before the full content gets uploaded below.
		cfg, _, err = image.DecodeConfig(file)
		if err != nil {
			return nil, common.BadRequestError("uploaded file is not a valid image")
		}
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			return nil, common.SystemError("failed to rewind uploaded image")
		}
	}

	// fetched before the transaction - only needed to clean up the old
	// storage object after a successful commit (see below), not part of the
	// atomic write itself.
	var oldImages []ProductImage
	if file != nil {
		oldImages, err = s.repo.ListProductImagesByProductIDs(ctx, []uint{id})
		if err != nil {
			return nil, err
		}
	}

	updates := map[string]any{}
	if in.CategoryID != nil {
		updates["category_id"] = *in.CategoryID
	}
	if in.Name != nil {
		updates["name"] = *in.Name
	}
	if in.Barcode != nil {
		updates["barcode"] = *in.Barcode
	}
	if pricingChanged {
		storedPercent, salePrice, discount := productPricing(price, percent)
		if in.Price != nil {
			updates["price"] = *in.Price
		}
		if in.DiscountPercent != nil {
			updates["discount_percent"] = storedPercent
		}
		updates["sale_price"] = salePrice
		updates["discount"] = discount
	}
	// The tax inside a unit follows what the customer pays, so a change to the
	// price, the discount % or the rate itself all recompute it. When pricing
	// did not change, the stored sale price is the one to take it from.
	if pricingChanged || in.TaxPercent != nil {
		salePrice := product.SalePrice
		if pricingChanged {
			_, salePrice, _ = productPricing(price, percent)
		}
		storedRate, tax := productTax(salePrice, taxPercent)
		if in.TaxPercent != nil {
			updates["tax_percent"] = storedRate
		}
		updates["tax"] = tax
	}
	if in.Threshold != nil {
		updates["threshold"] = *in.Threshold
	}
	if in.CostPrice != nil {
		updates["cost_price"] = *in.CostPrice
	}
	if in.IsActive != nil {
		updates["is_active"] = *in.IsActive
	}
	if in.Modifier != nil {
		updates["modifier"] = *in.Modifier
	}

	// A catalog price or percent change moves the sale price of every
	// override that inherits the changed field - see recomputeBranchPrices.
	// Read before the transaction, like oldImages above.
	var overrides []ProductBranchPrice
	if pricingChanged {
		overrides, err = s.repo.ListBranchPricesByProduct(ctx, orgID, id)
		if err != nil {
			return nil, err
		}
	}

	var newKey string
	if len(updates) > 0 || file != nil {
		err := s.db.Transaction(func(tx *gorm.DB) error {
			if len(updates) > 0 {
				if err := s.repo.UpdateProduct(tx, id, updates); err != nil {
					if common.IsDuplicateError(err) {
						return common.ConflictError("a product with this barcode already exists")
					}
					return err
				}
			}
			if pricingChanged {
				updated := *product
				updated.Price, updated.DiscountPercent = price, percent
				if err := s.recomputeBranchPrices(tx, &updated, overrides); err != nil {
					return err
				}
			}

			if file == nil {
				return nil
			}

			if err := s.repo.DeleteProductImagesByProductID(tx, id); err != nil {
				return err
			}

			// suffixed with a UUID, not just id+filename like CreateProduct -
			// a replace can reuse the exact same filename as the image it's
			// replacing, which would otherwise collide with (and overwrite)
			// the old object before it's safe to remove.
			newKey = fmt.Sprintf("products/%d/%s-%s", id, uuid.NewString(), filename)
			if err := s.storage.Upload(ctx, newKey, file, fileSize, contentType); err != nil {
				return common.SystemError("failed to upload product image")
			}

			productImage := ProductImage{
				ProductID:  id,
				StorageKey: newKey,
				Width:      cfg.Width,
				Height:     cfg.Height,
			}
			if err := s.repo.CreateProductImage(tx, &productImage); err != nil {
				_ = s.storage.Delete(ctx, newKey)
				return err
			}

			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	// only remove the old object once the new one is durably committed -
	// otherwise a failure above would leave the product with no valid image
	// at all.
	if file != nil {
		for _, img := range oldImages {
			_ = s.storage.Delete(ctx, img.StorageKey)
		}
	}

	return s.repo.GetProduct(ctx, orgID, id)
}

func (s *Service) DeleteProduct(ctx context.Context, orgID uint, id uint) error {
	if _, err := s.repo.GetProduct(ctx, orgID, id); err != nil {
		return err
	}

	inUse, err := s.repo.ComboItemsExistForProduct(ctx, id)
	if err != nil {
		return err
	}
	if inUse {
		return common.ConflictError("product is still in use by one or more combos")
	}

	// A hard delete would leave sale lines, stock, ledger entries and
	// purchase-order lines pointing at a row that no longer exists (none of
	// those tables has a DB-level FK), so history screens would lose the
	// product's name. Retire it instead.
	hasHistory, err := s.repo.ProductHasHistory(ctx, id)
	if err != nil {
		return err
	}
	if hasHistory {
		return common.ConflictError("product has sales, stock or purchase history and can't be deleted - set is_active to false to retire it instead")
	}

	// fetched before the transaction - only needed to clean up the storage
	// objects after a successful commit (see below), not part of the
	// atomic write itself.
	images, err := s.repo.ListProductImagesByProductIDs(ctx, []uint{id})
	if err != nil {
		return err
	}

	if err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := s.repo.DeleteProductImagesByProductID(tx, id); err != nil {
			return err
		}
		// overrides have no FK either, so they go with the product
		if err := s.repo.DeleteBranchPricesByProductID(tx, id); err != nil {
			return err
		}
		return s.repo.DeleteProduct(tx, id)
	}); err != nil {
		return err
	}

	for _, img := range images {
		_ = s.storage.Delete(ctx, img.StorageKey)
	}

	return nil
}

func (s *Service) ListCategories(ctx context.Context, orgID uint) ([]Category, error) {
	return s.repo.ListCategories(ctx, orgID)
}

// CreateCategory returns common.ConflictError if org_id+name_i18n collides
// with an existing category (see the ux_categories_org_name unique index on
// Category) - the repository just reports whatever error the database gives
// it; deciding what that error means to the caller is the service's job.
func (s *Service) CreateCategory(ctx context.Context, orgID uint, in CreateCategoryRequest) (*Category, error) {
	category := Category{
		OrgID:    orgID,
		NameI18n: in.NameI18n,
	}
	if err := s.repo.CreateCategory(ctx, &category); err != nil {
		if common.IsDuplicateError(err) {
			return nil, common.ConflictError("a category with this name already exists")
		}
		return nil, err
	}
	return &category, nil
}

// UpdateCategory confirms the category exists AND belongs to orgID before
// touching anything (same not-found-not-forbidden reasoning as
// identity.UpdateBranch), decides which fields actually changed, and returns
// common.ConflictError if the new name collides with another category in the
// same org.
func (s *Service) UpdateCategory(ctx context.Context, orgID uint, id uint, in UpdateCategoryRequest) (*Category, error) {
	if _, err := s.repo.GetCategory(ctx, orgID, id); err != nil {
		return nil, err
	}

	updates := map[string]any{}
	if in.NameI18n != nil {
		updates["name_i18n"] = *in.NameI18n
	}

	if len(updates) > 0 {
		if err := s.repo.UpdateCategory(ctx, id, updates); err != nil {
			if common.IsDuplicateError(err) {
				return nil, common.ConflictError("a category with this name already exists")
			}
			return nil, err
		}
	}

	return s.repo.GetCategory(ctx, orgID, id)
}

// DeleteCategory confirms the category exists AND belongs to orgID (same
// not-found-not-forbidden reasoning as UpdateCategory), then blocks the
// delete with common.ConflictError if any product still references it -
// deleting out from under a product would leave it pointing at a category
// that no longer exists.
func (s *Service) DeleteCategory(ctx context.Context, orgID uint, id uint) error {
	if _, err := s.repo.GetCategory(ctx, orgID, id); err != nil {
		return err
	}

	inUse, err := s.repo.ProductsExistForCategory(ctx, id)
	if err != nil {
		return err
	}
	if inUse {
		return common.ConflictError("category is still in use by one or more products")
	}

	return s.repo.DeleteCategory(ctx, id)
}

func (s *Service) ListCombos(ctx context.Context, orgID uint) ([]ComboResult, error) {
	combos, err := s.repo.ListCombos(ctx, orgID)
	if err != nil {
		return nil, err
	}
	return s.attachComboDetails(ctx, combos)
}

// attachComboDetails assembles each combo's component items and
// ComboImageResult list, same reasoning as attachImages.
func (s *Service) attachComboDetails(ctx context.Context, combos []Combo) ([]ComboResult, error) {
	results := make([]ComboResult, len(combos))
	if len(combos) == 0 {
		return results, nil
	}

	ids := make([]uint, len(combos))
	for i, c := range combos {
		ids[i] = c.ID
	}

	comboItems, err := s.repo.ListComboItemsByComboIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	itemsByCombo := make(map[uint][]ComboItemResult, len(combos))
	for _, it := range comboItems {
		itemsByCombo[it.ComboID] = append(itemsByCombo[it.ComboID], ComboItemResult{ProductID: it.ProductID, Qty: it.Qty})
	}

	images, err := s.repo.ListComboImagesByComboIDs(ctx, ids)
	if err != nil {
		return nil, err
	}

	byCombo := make(map[uint][]ComboImage, len(combos))
	for _, img := range images {
		byCombo[img.ComboID] = append(byCombo[img.ComboID], img)
	}

	for i, c := range combos {
		imgResults := make([]ComboImageResult, 0, len(byCombo[c.ID]))
		for _, img := range byCombo[c.ID] {
			url, err := s.storage.PresignedURL(ctx, img.StorageKey, DefaultImageURLTTL)
			if err != nil {
				return nil, common.SystemError("failed to generate image URL")
			}
			imgResults = append(imgResults, ComboImageResult{
				ID:     img.ID,
				URL:    url,
				Width:  img.Width,
				Height: img.Height,
			})
		}
		items := itemsByCombo[c.ID]
		if items == nil {
			items = []ComboItemResult{}
		}
		results[i] = ComboResult{Combo: c, Items: items, Images: imgResults}
	}

	return results, nil
}

// CreateCombo enforces that Price is actually positive, ExpiresAt is
// actually in the future (decimal.Decimal/time.Time zero-value struct tags
// can't do either - see CreateComboRequest's doc), and no ProductID repeats
// across in.Items, confirms every item's ProductID belongs to orgID, then
// creates the Combo, its ComboItems, and (if file is non-nil) its
// ComboImage together as one atomic unit of work - same reasoning as
// CreateProduct/ProductImage, except the image itself is optional here.
func (s *Service) CreateCombo(ctx context.Context, orgID uint, in CreateComboRequest, file io.ReadSeeker, fileSize int64, contentType, filename string) (*Combo, error) {
	if !in.Price.IsPositive() {
		return nil, common.BadRequestError("price must be greater than zero")
	}
	if !in.ExpiresAt.After(time.Now()) {
		return nil, common.BadRequestError("expires_at must be in the future")
	}

	seen := make(map[uint]struct{}, len(in.Items))
	for _, item := range in.Items {
		if _, dup := seen[item.ProductID]; dup {
			return nil, common.BadRequestError("duplicate product_id in items")
		}
		seen[item.ProductID] = struct{}{}
	}

	for _, item := range in.Items {
		if _, err := s.repo.GetProduct(ctx, orgID, item.ProductID); err != nil {
			return nil, err
		}
	}

	var cfg image.Config
	if file != nil {
		var err error
		// image.DecodeConfig only reads the header (not the full pixel
		// data) to get Width/Height, but it still consumes bytes from file -
		// rewind before the full content gets uploaded below.
		cfg, _, err = image.DecodeConfig(file)
		if err != nil {
			return nil, common.BadRequestError("uploaded file is not a valid image")
		}
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			return nil, common.SystemError("failed to rewind uploaded image")
		}
	}

	var result *Combo
	err := s.db.Transaction(func(tx *gorm.DB) error {
		combo := Combo{
			OrgID:     orgID,
			Name:      in.Name,
			Price:     in.Price,
			ExpiresAt: in.ExpiresAt,
		}
		if err := s.repo.CreateCombo(tx, &combo); err != nil {
			return err
		}

		items := make([]ComboItem, len(in.Items))
		for i, item := range in.Items {
			items[i] = ComboItem{ComboID: combo.ID, ProductID: item.ProductID, Qty: item.Qty}
		}
		if err := s.repo.CreateComboItems(tx, items); err != nil {
			return err
		}

		if file == nil {
			result = &combo
			return nil
		}

		// keyed by the new combo's ID, so it's only known once the row
		// above exists.
		key := fmt.Sprintf("combos/%d/%s", combo.ID, filename)
		if err := s.storage.Upload(ctx, key, file, fileSize, contentType); err != nil {
			return common.SystemError("failed to upload combo image")
		}

		comboImage := ComboImage{
			ComboID:    combo.ID,
			StorageKey: key,
			Width:      cfg.Width,
			Height:     cfg.Height,
		}
		if err := s.repo.CreateComboImage(tx, &comboImage); err != nil {
			// same reasoning as CreateProduct's cleanup: the DB write
			// failing rolls back the SQL rows, but can't undo the object
			// storage upload above, so clean it up here to avoid leaving an
			// orphaned object with no row referencing it.
			_ = s.storage.Delete(ctx, key)
			return err
		}

		result = &combo
		return nil
	})
	if err != nil {
		return nil, err
	}

	return result, nil
}

func (s *Service) UpdateCombo(ctx context.Context, orgID uint, id uint, in UpdateComboRequest, file io.ReadSeeker, fileSize int64, contentType, filename string) (*Combo, error) {
	combo, err := s.repo.GetCombo(ctx, orgID, id)
	if err != nil {
		return nil, err
	}

	if in.Price != nil || in.ExpiresAt != nil {
		price, expiresAt := combo.Price, combo.ExpiresAt
		if in.Price != nil {
			price = *in.Price
		}
		if in.ExpiresAt != nil {
			expiresAt = *in.ExpiresAt
		}
		if !price.IsPositive() {
			return nil, common.BadRequestError("price must be greater than zero")
		}
		if !expiresAt.After(time.Now()) {
			return nil, common.BadRequestError("expires_at must be in the future")
		}
	}

	var cfg image.Config
	if file != nil {
		var err error
		// image.DecodeConfig only reads the header (not the full pixel
		// data) to get Width/Height, but it still consumes bytes from file -
		// rewind before the full content gets uploaded below.
		cfg, _, err = image.DecodeConfig(file)
		if err != nil {
			return nil, common.BadRequestError("uploaded file is not a valid image")
		}
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			return nil, common.SystemError("failed to rewind uploaded image")
		}
	}

	// fetched before the transaction - only needed to clean up the old
	// storage object after a successful commit (see below), not part of the
	// atomic write itself.
	var oldImages []ComboImage
	if file != nil {
		oldImages, err = s.repo.ListComboImagesByComboIDs(ctx, []uint{id})
		if err != nil {
			return nil, err
		}
	}

	updates := map[string]any{}
	if in.Name != nil {
		updates["name"] = *in.Name
	}
	if in.Price != nil {
		updates["price"] = *in.Price
	}
	if in.ExpiresAt != nil {
		updates["expires_at"] = *in.ExpiresAt
	}

	if len(updates) > 0 || file != nil {
		err := s.db.Transaction(func(tx *gorm.DB) error {
			if len(updates) > 0 {
				if err := s.repo.UpdateCombo(tx, id, updates); err != nil {
					return err
				}
			}

			if file == nil {
				return nil
			}

			if err := s.repo.DeleteComboImagesByComboID(tx, id); err != nil {
				return err
			}

			// suffixed with a UUID, same collision reasoning as
			// UpdateProduct's image replace.
			newKey := fmt.Sprintf("combos/%d/%s-%s", id, uuid.NewString(), filename)
			if err := s.storage.Upload(ctx, newKey, file, fileSize, contentType); err != nil {
				return common.SystemError("failed to upload combo image")
			}

			comboImage := ComboImage{
				ComboID:    id,
				StorageKey: newKey,
				Width:      cfg.Width,
				Height:     cfg.Height,
			}
			if err := s.repo.CreateComboImage(tx, &comboImage); err != nil {
				_ = s.storage.Delete(ctx, newKey)
				return err
			}

			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	// only remove the old object once the new one is durably committed -
	// otherwise a failure above would leave the combo with no valid image
	// at all.
	if file != nil {
		for _, img := range oldImages {
			_ = s.storage.Delete(ctx, img.StorageKey)
		}
	}

	return s.repo.GetCombo(ctx, orgID, id)
}

func (s *Service) DeleteCombo(ctx context.Context, orgID uint, id uint) error {
	if _, err := s.repo.GetCombo(ctx, orgID, id); err != nil {
		return err
	}

	// fetched before the transaction - only needed to clean up the storage
	// objects after a successful commit (see below), not part of the
	// atomic write itself.
	images, err := s.repo.ListComboImagesByComboIDs(ctx, []uint{id})
	if err != nil {
		return err
	}

	if err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := s.repo.DeleteComboImagesByComboID(tx, id); err != nil {
			return err
		}
		if err := s.repo.DeleteComboItemsByComboID(tx, id); err != nil {
			return err
		}
		return s.repo.DeleteCombo(tx, id)
	}); err != nil {
		return err
	}

	for _, img := range images {
		_ = s.storage.Delete(ctx, img.StorageKey)
	}

	return nil
}

// recomputeBranchPrices refreshes the stored sale price of a product's
// overrides after its catalog price or percent changed: an override that
// inherits the changed field now sells at a new price, and one that sets both
// is unaffected (its recomputed value equals the stored one, so it's skipped).
// product carries the NEW catalog values. Runs inside UpdateProduct's
// transaction.
func (s *Service) recomputeBranchPrices(tx *gorm.DB, product *Product, overrides []ProductBranchPrice) error {
	for _, o := range overrides {
		salePrice := overrideSalePrice(product, o.Price, o.DiscountPercent)
		if salePrice.Equal(o.SalePrice) {
			continue
		}
		if err := s.repo.UpdateBranchPriceSalePrice(tx, o.ID, salePrice); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) ListBranchPrices(ctx context.Context, orgID uint, productID uint, branchID *uint) ([]BranchPriceResult, error) {
	product, err := s.repo.GetProduct(ctx, orgID, productID)
	if err != nil {
		return nil, err
	}
	var branchList []identity.Branch
	if branchID != nil {
		branch, err := s.branches.GetBranch(ctx, orgID, *branchID)
		if err != nil {
			return nil, err
		}
		branchList = []identity.Branch{*branch}
	} else if branchList, err = s.branches.ListBranches(ctx, orgID); err != nil {
		return nil, err
	}
	overrides, err := s.repo.ListBranchPricesByProduct(ctx, orgID, productID)
	if err != nil {
		return nil, err
	}
	byBranch := make(map[uint]*ProductBranchPrice, len(overrides))
	for i := range overrides {
		byBranch[overrides[i].BranchID] = &overrides[i]
	}
	results := make([]BranchPriceResult, len(branchList))
	for i, b := range branchList {
		results[i] = branchPriceResult(product, b, byBranch[b.ID])
	}
	return results, nil
}

// branchPriceResult is one branch's row: the override itself (null fields
// inherit) and what the branch effectively charges.
func branchPriceResult(product *Product, branch identity.Branch, override *ProductBranchPrice) BranchPriceResult {
	pr := EffectivePricing(product, override)
	res := BranchPriceResult{
		BranchID: branch.ID, BranchName: branch.Name,
		CatalogPrice: product.Price, CatalogDiscountPercent: product.DiscountPercent,
		EffectivePrice: pr.Price, EffectiveDiscountPercent: pr.DiscountPercent, EffectiveSalePrice: pr.SalePrice,
		PriceSource: pr.Source,
	}
	if override != nil {
		res.Price, res.DiscountPercent = override.Price, override.DiscountPercent
		updatedAt := override.UpdatedAt
		res.UpdatedAt = &updatedAt
	}
	return res
}

// branchPriceAudit is the before/after snapshot of an override for the audit
// log; nil (no override) is recorded as JSON null.
func branchPriceAudit(o *ProductBranchPrice) any {
	if o == nil {
		return nil
	}
	return map[string]any{"price": o.Price, "discount_percent": o.DiscountPercent, "sale_price": o.SalePrice}
}

// checkBranchPriceActor enforces the own-branch rule for a caller bound to a
// branch (a manager at a till). Done first, before any lookup, so a manager
// probing another branch's - or another org's - id learns nothing.
func checkBranchPriceActor(actor BranchPriceActor, branchID uint) error {
	if actor.BranchID != nil && *actor.BranchID != branchID {
		return common.ForbiddenError("you can only change your own branch's prices")
	}
	return nil
}

// audit builds the audit entry for one branch-price write, recording a
// manager as the staff actor and an org-wide account as the user actor.
func (a BranchPriceActor) audit(orgID, productID, branchID uint, action string, before, after any) *audit.AuditLog {
	return &audit.AuditLog{
		OrgID: orgID, ActorID: a.StaffID, ActorUserID: a.UserID, BranchID: &branchID,
		Entity: "product_branch_price", EntityID: strconv.FormatUint(uint64(productID), 10), Action: action,
		Before: audit.ToJSON(before), After: audit.ToJSON(after),
	}
}

func (s *Service) SetBranchPrice(ctx context.Context, orgID uint, actor BranchPriceActor, productID uint, branchID uint, in SetBranchPriceRequest) (*BranchPriceResult, error) {
	if err := checkBranchPriceActor(actor, branchID); err != nil {
		return nil, err
	}
	product, err := s.repo.GetProduct(ctx, orgID, productID)
	if err != nil {
		return nil, err
	}
	branch, err := s.branches.GetBranch(ctx, orgID, branchID)
	if err != nil {
		return nil, err
	}
	// nothing left to override: the branch goes back to the catalog price
	if in.Price == nil && in.DiscountPercent == nil {
		if err := s.DeleteBranchPrice(ctx, orgID, actor, productID, branchID); err != nil {
			return nil, err
		}
		res := branchPriceResult(product, *branch, nil)
		return &res, nil
	}
	if branch.Status != identity.BranchStatusActive {
		return nil, common.BadRequestError("branch is not active")
	}
	var price, percent *decimal.Decimal
	if in.Price != nil {
		if !in.Price.IsPositive() {
			return nil, common.BadRequestError("price must be greater than zero")
		}
		p := in.Price.Round(2)
		price = &p
	}
	if in.DiscountPercent != nil {
		if in.DiscountPercent.IsNegative() || in.DiscountPercent.GreaterThan(decimal.NewFromInt(100)) {
			return nil, common.BadRequestError("discount_percent must be between 0 and 100")
		}
		p := in.DiscountPercent.Round(2)
		percent = &p
	}

	before, err := s.repo.GetBranchPrice(ctx, orgID, branchID, productID)
	if err != nil {
		return nil, err
	}
	row := ProductBranchPrice{
		OrgID: orgID, ProductID: productID, BranchID: branchID,
		Price: price, DiscountPercent: percent,
		SalePrice:       overrideSalePrice(product, price, percent),
		UpdatedByUserID: actor.UserID, UpdatedByStaffID: actor.StaffID,
	}
	err = s.db.Transaction(func(tx *gorm.DB) error {
		if err := s.repo.UpsertBranchPrice(tx, &row); err != nil {
			return err
		}
		return s.audit.CreateAuditLog(tx, actor.audit(orgID, productID, branchID, "branch_price_set", branchPriceAudit(before), branchPriceAudit(&row)))
	})
	if err != nil {
		return nil, err
	}
	res := branchPriceResult(product, *branch, &row)
	return &res, nil
}

func (s *Service) DeleteBranchPrice(ctx context.Context, orgID uint, actor BranchPriceActor, productID uint, branchID uint) error {
	if err := checkBranchPriceActor(actor, branchID); err != nil {
		return err
	}
	if _, err := s.repo.GetProduct(ctx, orgID, productID); err != nil {
		return err
	}
	if _, err := s.branches.GetBranch(ctx, orgID, branchID); err != nil {
		return err
	}
	before, err := s.repo.GetBranchPrice(ctx, orgID, branchID, productID)
	if err != nil {
		return err
	}
	if before == nil {
		return nil
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := s.repo.DeleteBranchPrice(tx, orgID, productID, branchID); err != nil {
			return err
		}
		return s.audit.CreateAuditLog(tx, actor.audit(orgID, productID, branchID, "branch_price_removed", branchPriceAudit(before), nil))
	})
}
