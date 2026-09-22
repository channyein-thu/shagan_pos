package catalog

import (
	"context"
	"fmt"
	"image"
	_ "image/gif"  // registers GIF decoding with image.DecodeConfig
	_ "image/jpeg" // registers JPEG decoding with image.DecodeConfig
	_ "image/png"  // registers PNG decoding with image.DecodeConfig
	"io"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"shagan_pos/internal/common"
	"shagan_pos/internal/storage"
)

// DefaultImageURLTTL is how long a presigned product-image URL stays valid -
// long enough for a client to load the image, short enough that a
// leaked/cached link doesn't work forever.
const DefaultImageURLTTL = 15 * time.Minute

// validateProductMoney enforces the business rules decimal.Decimal can't get
// from struct tags (see CreateProductRequest's doc): Price must be positive,
// Discount/Tax must not be negative, and Discount must not exceed Price
// (which would make the effective price negative). Tax is stored exactly as
// given - it's typed directly, never computed.
func validateProductMoney(price, discount, tax decimal.Decimal) error {
	if !price.IsPositive() {
		return common.BadRequestError("price must be greater than zero")
	}
	if discount.IsNegative() {
		return common.BadRequestError("discount must not be negative")
	}
	if tax.IsNegative() {
		return common.BadRequestError("tax must not be negative")
	}
	if discount.GreaterThan(price) {
		return common.BadRequestError("discount must not exceed price")
	}
	return nil
}

type Service struct {
	repo     Repository
	branches BranchLookup
	db       common.Transactioner
	storage  storage.Storage
}

func NewService(repo Repository, branches BranchLookup, db common.Transactioner, store storage.Storage) *Service {
	return &Service{repo: repo, branches: branches, db: db, storage: store}
}

var _ Interface = (*Service)(nil)

func (s *Service) ListProducts(ctx context.Context, orgID uint, branchID *uint) ([]ProductResult, error) {
	products, err := s.repo.ListProducts(ctx, orgID, branchID)
	if err != nil {
		return nil, err
	}
	return s.attachImages(ctx, products)
}

func (s *Service) GetProduct(ctx context.Context, orgID uint, id uint) (*ProductResult, error) {
	product, err := s.repo.GetProduct(ctx, orgID, id)
	if err != nil {
		return nil, err
	}
	results, err := s.attachImages(ctx, []Product{*product})
	if err != nil {
		return nil, err
	}
	return &results[0], nil
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

func (s *Service) GetProductByBarcode(ctx context.Context, orgID uint, branchID uint, code string) (*ProductResult, error) {
	product, err := s.repo.GetProductByBarcode(ctx, orgID, branchID, code)
	if err != nil {
		return nil, err
	}
	results, err := s.attachImages(ctx, []Product{*product})
	if err != nil {
		return nil, err
	}
	return &results[0], nil
}

// CreateProduct confirms in.BranchID and in.CategoryID both belong to orgID
// (same not-found-not-forbidden reasoning as identity's branch-ownership
// checks - a product can't be attached to another org's branch or category),
// enforces the price/discount/tax rules, decodes the uploaded image's real
// pixel dimensions, then creates the Product and its ProductImage together as
// one atomic unit of work: a product without its required image should never
// exist. A duplicate barcode within the same branch (ux_products_branch_barcode)
// surfaces as common.ConflictError - same pattern as CreateCategory's
// duplicate-name handling.
func (s *Service) CreateProduct(ctx context.Context, orgID uint, in CreateProductRequest, file io.ReadSeeker, fileSize int64, contentType, filename string) (*Product, error) {
	if _, err := s.branches.GetBranch(ctx, orgID, in.BranchID); err != nil {
		return nil, err
	}
	if _, err := s.repo.GetCategory(ctx, orgID, in.CategoryID); err != nil {
		return nil, err
	}

	if err := validateProductMoney(in.Price, in.Discount, in.Tax); err != nil {
		return nil, err
	}

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
			BranchID:   in.BranchID,
			CategoryID: in.CategoryID,
			Name:       in.Name,
			Barcode:    in.Barcode,
			Price:      in.Price,
			Discount:   in.Discount,
			Tax:        in.Tax,
			Threshold:  in.Threshold,
			IsActive:   in.IsActive,
			Modifier:   in.Modifier,
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

func (s *Service) UpdateProduct(ctx context.Context, id uint, in UpdateProductRequest) (*Product, error) {
	return s.repo.UpdateProduct(ctx, id, in)
}

func (s *Service) DeleteProduct(ctx context.Context, id uint) error {
	return s.repo.DeleteProduct(ctx, id)
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

func (s *Service) UploadMedia(ctx context.Context) (*ProductImage, error) {
	return s.repo.UploadMedia(ctx)
}

func (s *Service) ListCombos(ctx context.Context, orgID uint) ([]Combo, error) {
	return s.repo.ListCombos(ctx, orgID)
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

func (s *Service) UpdateCombo(ctx context.Context, id uint, in UpdateComboRequest) (*Combo, error) {
	return s.repo.UpdateCombo(ctx, id, in)
}

func (s *Service) DeleteCombo(ctx context.Context, id uint) error {
	return s.repo.DeleteCombo(ctx, id)
}
