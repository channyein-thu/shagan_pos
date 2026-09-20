package catalog

import (
	"context"
	"fmt"
	"image"
	_ "image/gif"  // registers GIF decoding with image.DecodeConfig
	_ "image/jpeg" // registers JPEG decoding with image.DecodeConfig
	_ "image/png"  // registers PNG decoding with image.DecodeConfig
	"io"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"shagan_pos/internal/common"
	"shagan_pos/internal/storage"
)

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

func (s *Service) ListProducts(ctx context.Context) ([]Product, error) {
	return s.repo.ListProducts(ctx)
}

func (s *Service) GetProduct(ctx context.Context, id uint) (*Product, error) {
	return s.repo.GetProduct(ctx, id)
}

func (s *Service) GetProductByBarcode(ctx context.Context, code string) (*Product, error) {
	return s.repo.GetProductByBarcode(ctx, code)
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

func (s *Service) ListCombos(ctx context.Context) ([]Combo, error) {
	return s.repo.ListCombos(ctx)
}

func (s *Service) CreateCombo(ctx context.Context, in CreateComboRequest) (*Combo, error) {
	return s.repo.CreateCombo(ctx, in)
}

func (s *Service) UpdateCombo(ctx context.Context, id uint, in UpdateComboRequest) (*Combo, error) {
	return s.repo.UpdateCombo(ctx, id, in)
}

func (s *Service) DeleteCombo(ctx context.Context, id uint) error {
	return s.repo.DeleteCombo(ctx, id)
}
