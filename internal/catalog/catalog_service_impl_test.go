package catalog

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"image"
	"image/png"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"shagan_pos/internal/common"
	"shagan_pos/internal/identity"
)

func requireRestErrorStatus(t *testing.T, err error, status int) {
	t.Helper()
	var restErr common.RestError
	require.True(t, errors.As(err, &restErr), "expected a common.RestError, got %T: %v", err, err)
	require.Equal(t, status, restErr.Status)
}

// fakeTransactioner runs fc directly against a nil *gorm.DB, with no real
// database transaction - sufficient for unit tests that only exercise
// service-level orchestration against mocked dependencies. See identity's
// equivalent for the same reasoning; *gorm.DB satisfies common.Transactioner
// natively in production.
type fakeTransactioner struct{}

func (fakeTransactioner) Transaction(fc func(tx *gorm.DB) error, _ ...*sql.TxOptions) error {
	return fc(nil)
}

// testProductImageBytes returns real, valid PNG bytes at the given
// dimensions - image.DecodeConfig (used by Service.CreateProduct) needs to
// actually decode a real image header, which isn't something a mock can
// stand in for.
func testProductImageBytes(t *testing.T, width, height int) []byte {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, width, height))))
	return buf.Bytes()
}

func TestService_ListCategories_DelegatesToRepository(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	store := NewMockStorage(t)
	svc := NewService(repo, branches, fakeTransactioner{}, store)

	want := []Category{{ID: 1, OrgID: 7, NameI18n: "Food"}, {ID: 3, OrgID: 7, NameI18n: "Drinks"}}
	repo.EXPECT().ListCategories(mock.Anything, uint(7)).Return(want, nil).Once()

	got, err := svc.ListCategories(context.Background(), 7)
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func TestService_ListCategories_PropagatesRepositoryError(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	store := NewMockStorage(t)
	svc := NewService(repo, branches, fakeTransactioner{}, store)

	wantErr := common.SystemError("db read failed")
	repo.EXPECT().ListCategories(mock.Anything, uint(7)).Return(nil, wantErr).Once()

	_, err := svc.ListCategories(context.Background(), 7)
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusInternalServerError)
}

func TestService_CreateCategory_BuildsModelAndPersists(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	store := NewMockStorage(t)
	svc := NewService(repo, branches, fakeTransactioner{}, store)

	in := CreateCategoryRequest{NameI18n: "Beverages"}

	repo.EXPECT().
		CreateCategory(mock.Anything, mock.MatchedBy(func(c *Category) bool {
			return c.OrgID == 7 && c.NameI18n == "Beverages"
		})).
		Run(func(_ context.Context, c *Category) { c.ID = 1 }).
		Return(nil).
		Once()

	got, err := svc.CreateCategory(context.Background(), 7, in)
	require.NoError(t, err)
	require.Equal(t, uint(1), got.ID)
	require.Equal(t, uint(7), got.OrgID)
	require.Equal(t, "Beverages", got.NameI18n)
}

func TestService_CreateCategory_UnexpectedRepositoryError_PropagatesAsIs(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	store := NewMockStorage(t)
	svc := NewService(repo, branches, fakeTransactioner{}, store)

	in := CreateCategoryRequest{NameI18n: "Beverages"}
	dbErr := errors.New("connection refused")
	repo.EXPECT().CreateCategory(mock.Anything, mock.Anything).Return(dbErr).Once()

	_, err := svc.CreateCategory(context.Background(), 7, in)
	require.ErrorIs(t, err, dbErr)
}

// TestService_CreateCategory_DuplicateName_ReturnsConflict exercises the real
// translation logic (unlike a test that just mocks the repo returning a
// pre-made common.ConflictError): the mock hands back the same *pgconn.PgError
// shape Postgres would actually raise for the ux_categories_org_name unique
// constraint, and the service is what's responsible for recognizing it.
func TestService_CreateCategory_DuplicateName_ReturnsConflict(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	store := NewMockStorage(t)
	svc := NewService(repo, branches, fakeTransactioner{}, store)

	in := CreateCategoryRequest{NameI18n: "Beverages"}
	dbErr := &pgconn.PgError{Code: "23505"}
	repo.EXPECT().CreateCategory(mock.Anything, mock.Anything).Return(dbErr).Once()

	_, err := svc.CreateCategory(context.Background(), 7, in)
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusConflict)

	var restErr common.RestError
	errors.As(err, &restErr)
	require.Equal(t, "a category with this name already exists", restErr.Message)
}

func TestService_UpdateCategory_HappyPath_UpdatesAndReturns(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	store := NewMockStorage(t)
	svc := NewService(repo, branches, fakeTransactioner{}, store)

	existing := &Category{ID: 1, OrgID: 7, NameI18n: "Food"}
	updated := &Category{ID: 1, OrgID: 7, NameI18n: "Groceries"}
	name := "Groceries"
	in := UpdateCategoryRequest{NameI18n: &name}

	repo.EXPECT().GetCategory(mock.Anything, uint(7), uint(1)).Return(existing, nil).Once()
	repo.EXPECT().UpdateCategory(mock.Anything, uint(1), map[string]any{"name_i18n": "Groceries"}).Return(nil).Once()
	repo.EXPECT().GetCategory(mock.Anything, uint(7), uint(1)).Return(updated, nil).Once()

	got, err := svc.UpdateCategory(context.Background(), 7, 1, in)
	require.NoError(t, err)
	require.Same(t, updated, got)
}

func TestService_UpdateCategory_NoFieldsProvided_SkipsWrite(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	store := NewMockStorage(t)
	svc := NewService(repo, branches, fakeTransactioner{}, store)

	existing := &Category{ID: 1, OrgID: 7, NameI18n: "Food"}
	in := UpdateCategoryRequest{}

	repo.EXPECT().GetCategory(mock.Anything, uint(7), uint(1)).Return(existing, nil).Twice()
	// UpdateCategory must never be called - no .EXPECT() set up for it means
	// the mock fails the test if it is.

	got, err := svc.UpdateCategory(context.Background(), 7, 1, in)
	require.NoError(t, err)
	require.Same(t, existing, got)
}

func TestService_UpdateCategory_PropagatesNotFound(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	store := NewMockStorage(t)
	svc := NewService(repo, branches, fakeTransactioner{}, store)

	wantErr := common.NotFoundError("category not found")
	repo.EXPECT().GetCategory(mock.Anything, uint(7), uint(999)).Return(nil, wantErr).Once()
	// UpdateCategory must never be called on a not-found category.

	_, err := svc.UpdateCategory(context.Background(), 7, 999, UpdateCategoryRequest{})
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusNotFound)
}

func TestService_UpdateCategory_DuplicateName_ReturnsConflict(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	store := NewMockStorage(t)
	svc := NewService(repo, branches, fakeTransactioner{}, store)

	existing := &Category{ID: 1, OrgID: 7, NameI18n: "Food"}
	name := "Drinks"
	in := UpdateCategoryRequest{NameI18n: &name}

	repo.EXPECT().GetCategory(mock.Anything, uint(7), uint(1)).Return(existing, nil).Once()
	repo.EXPECT().
		UpdateCategory(mock.Anything, uint(1), map[string]any{"name_i18n": "Drinks"}).
		Return(&pgconn.PgError{Code: "23505"}).
		Once()

	_, err := svc.UpdateCategory(context.Background(), 7, 1, in)
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusConflict)
}

func TestService_DeleteCategory_HappyPath_Deletes(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	store := NewMockStorage(t)
	svc := NewService(repo, branches, fakeTransactioner{}, store)

	existing := &Category{ID: 1, OrgID: 7, NameI18n: "Food"}
	repo.EXPECT().GetCategory(mock.Anything, uint(7), uint(1)).Return(existing, nil).Once()
	repo.EXPECT().ProductsExistForCategory(mock.Anything, uint(1)).Return(false, nil).Once()
	repo.EXPECT().DeleteCategory(mock.Anything, uint(1)).Return(nil).Once()

	err := svc.DeleteCategory(context.Background(), 7, 1)
	require.NoError(t, err)
}

func TestService_DeleteCategory_InUseByProducts_ReturnsConflict(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	store := NewMockStorage(t)
	svc := NewService(repo, branches, fakeTransactioner{}, store)

	existing := &Category{ID: 1, OrgID: 7, NameI18n: "Food"}
	repo.EXPECT().GetCategory(mock.Anything, uint(7), uint(1)).Return(existing, nil).Once()
	repo.EXPECT().ProductsExistForCategory(mock.Anything, uint(1)).Return(true, nil).Once()
	// DeleteCategory must never be called once a product reference is found -
	// no .EXPECT() set up for it means the mock fails the test if it is.

	err := svc.DeleteCategory(context.Background(), 7, 1)
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusConflict)
}

func TestService_DeleteCategory_PropagatesNotFound(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	store := NewMockStorage(t)
	svc := NewService(repo, branches, fakeTransactioner{}, store)

	wantErr := common.NotFoundError("category not found")
	repo.EXPECT().GetCategory(mock.Anything, uint(7), uint(999)).Return(nil, wantErr).Once()
	// Neither ProductsExistForCategory nor DeleteCategory must be called on a
	// not-found category.

	err := svc.DeleteCategory(context.Background(), 7, 999)
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusNotFound)
}

func validCreateProductRequest() CreateProductRequest {
	modifier := "none"
	return CreateProductRequest{
		BranchID:   5,
		CategoryID: 3,
		Name:       "Iced Coffee",
		Barcode:    "8850001234567",
		Price:      decimal.NewFromFloat(3.50),
		Discount:   decimal.NewFromFloat(0.50),
		Tax:        decimal.NewFromFloat(0.20),
		Threshold:  10,
		IsActive:   true,
		Modifier:   &modifier,
	}
}

func TestService_CreateProduct_HappyPath_CreatesProductAndImage(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	store := NewMockStorage(t)
	svc := NewService(repo, branches, fakeTransactioner{}, store)

	in := validCreateProductRequest()
	imgBytes := testProductImageBytes(t, 2, 3)
	file := bytes.NewReader(imgBytes)

	branches.EXPECT().GetBranch(mock.Anything, uint(7), uint(5)).Return(&identity.Branch{ID: 5, OrgID: 7}, nil).Once()
	repo.EXPECT().GetCategory(mock.Anything, uint(7), uint(3)).Return(&Category{ID: 3, OrgID: 7}, nil).Once()
	repo.EXPECT().
		CreateProduct(mock.Anything, mock.MatchedBy(func(p *Product) bool {
			return p.OrgID == 7 && p.BranchID == 5 && p.CategoryID == 3 && p.Barcode == "8850001234567"
		})).
		Run(func(_ *gorm.DB, p *Product) { p.ID = 1 }).
		Return(nil).
		Once()
	store.EXPECT().
		Upload(mock.Anything, "products/1/photo.png", mock.Anything, int64(len(imgBytes)), "image/png").
		Return(nil).
		Once()
	repo.EXPECT().
		CreateProductImage(mock.Anything, mock.MatchedBy(func(img *ProductImage) bool {
			return img.ProductID == 1 && img.StorageKey == "products/1/photo.png" && img.Width == 2 && img.Height == 3
		})).
		Return(nil).
		Once()

	got, err := svc.CreateProduct(context.Background(), 7, in, file, int64(len(imgBytes)), "image/png", "photo.png")
	require.NoError(t, err)
	require.Equal(t, uint(1), got.ID)
	require.Equal(t, uint(7), got.OrgID)
}

func TestService_CreateProduct_NilModifier_PersistsAsNull(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	store := NewMockStorage(t)
	svc := NewService(repo, branches, fakeTransactioner{}, store)

	in := validCreateProductRequest()
	in.Modifier = nil
	imgBytes := testProductImageBytes(t, 2, 3)

	branches.EXPECT().GetBranch(mock.Anything, uint(7), uint(5)).Return(&identity.Branch{ID: 5, OrgID: 7}, nil).Once()
	repo.EXPECT().GetCategory(mock.Anything, uint(7), uint(3)).Return(&Category{ID: 3, OrgID: 7}, nil).Once()
	repo.EXPECT().
		CreateProduct(mock.Anything, mock.MatchedBy(func(p *Product) bool {
			return p.Modifier == nil
		})).
		Run(func(_ *gorm.DB, p *Product) { p.ID = 1 }).
		Return(nil).
		Once()
	store.EXPECT().Upload(mock.Anything, "products/1/photo.png", mock.Anything, int64(len(imgBytes)), "image/png").Return(nil).Once()
	repo.EXPECT().CreateProductImage(mock.Anything, mock.Anything).Return(nil).Once()

	got, err := svc.CreateProduct(context.Background(), 7, in, bytes.NewReader(imgBytes), int64(len(imgBytes)), "image/png", "photo.png")
	require.NoError(t, err)
	require.Nil(t, got.Modifier)
}

func TestService_CreateProduct_InvalidImage_ReturnsBadRequest(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	store := NewMockStorage(t)
	svc := NewService(repo, branches, fakeTransactioner{}, store)

	branches.EXPECT().GetBranch(mock.Anything, uint(7), uint(5)).Return(&identity.Branch{ID: 5, OrgID: 7}, nil).Once()
	repo.EXPECT().GetCategory(mock.Anything, uint(7), uint(3)).Return(&Category{ID: 3, OrgID: 7}, nil).Once()
	// CreateProduct must never be called - not a decodable image.

	notAnImage := bytes.NewReader([]byte("this is not an image"))
	_, err := svc.CreateProduct(context.Background(), 7, validCreateProductRequest(), notAnImage, int64(notAnImage.Len()), "image/png", "photo.png")
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusBadRequest)
}

func TestService_CreateProduct_BranchNotInOrg_PropagatesNotFound(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	store := NewMockStorage(t)
	svc := NewService(repo, branches, fakeTransactioner{}, store)

	wantErr := common.NotFoundError("branch not found")
	branches.EXPECT().GetBranch(mock.Anything, uint(7), uint(5)).Return(nil, wantErr).Once()
	// Neither GetCategory nor CreateProduct must be called for a branch
	// that isn't ours - no .EXPECT() set up for either means the mock
	// fails the test if it is. file is never read, so a placeholder is fine.

	_, err := svc.CreateProduct(context.Background(), 7, validCreateProductRequest(), bytes.NewReader(nil), 0, "image/png", "photo.png")
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusNotFound)
}

func TestService_CreateProduct_CategoryNotInOrg_PropagatesNotFound(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	store := NewMockStorage(t)
	svc := NewService(repo, branches, fakeTransactioner{}, store)

	branches.EXPECT().GetBranch(mock.Anything, uint(7), uint(5)).Return(&identity.Branch{ID: 5, OrgID: 7}, nil).Once()
	wantErr := common.NotFoundError("category not found")
	repo.EXPECT().GetCategory(mock.Anything, uint(7), uint(3)).Return(nil, wantErr).Once()
	// CreateProduct must never be called for a category that isn't ours -
	// no .EXPECT() set up for it means the mock fails the test if it is.

	_, err := svc.CreateProduct(context.Background(), 7, validCreateProductRequest(), bytes.NewReader(nil), 0, "image/png", "photo.png")
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusNotFound)
}

func TestService_CreateProduct_PriceNotPositive_RejectsBeforeTouchingRepository(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	store := NewMockStorage(t)
	svc := NewService(repo, branches, fakeTransactioner{}, store)

	branches.EXPECT().GetBranch(mock.Anything, uint(7), uint(5)).Return(&identity.Branch{ID: 5, OrgID: 7}, nil).Once()
	repo.EXPECT().GetCategory(mock.Anything, uint(7), uint(3)).Return(&Category{ID: 3, OrgID: 7}, nil).Once()
	// CreateProduct must never be called - price fails validation first.

	in := validCreateProductRequest()
	in.Price = decimal.Zero

	_, err := svc.CreateProduct(context.Background(), 7, in, bytes.NewReader(nil), 0, "image/png", "photo.png")
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusBadRequest)
}

func TestService_CreateProduct_NegativeDiscount_RejectsBeforeTouchingRepository(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	store := NewMockStorage(t)
	svc := NewService(repo, branches, fakeTransactioner{}, store)

	branches.EXPECT().GetBranch(mock.Anything, uint(7), uint(5)).Return(&identity.Branch{ID: 5, OrgID: 7}, nil).Once()
	repo.EXPECT().GetCategory(mock.Anything, uint(7), uint(3)).Return(&Category{ID: 3, OrgID: 7}, nil).Once()

	in := validCreateProductRequest()
	in.Discount = decimal.NewFromFloat(-0.01)

	_, err := svc.CreateProduct(context.Background(), 7, in, bytes.NewReader(nil), 0, "image/png", "photo.png")
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusBadRequest)
}

func TestService_CreateProduct_NegativeTax_RejectsBeforeTouchingRepository(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	store := NewMockStorage(t)
	svc := NewService(repo, branches, fakeTransactioner{}, store)

	branches.EXPECT().GetBranch(mock.Anything, uint(7), uint(5)).Return(&identity.Branch{ID: 5, OrgID: 7}, nil).Once()
	repo.EXPECT().GetCategory(mock.Anything, uint(7), uint(3)).Return(&Category{ID: 3, OrgID: 7}, nil).Once()

	in := validCreateProductRequest()
	in.Tax = decimal.NewFromFloat(-0.01)

	_, err := svc.CreateProduct(context.Background(), 7, in, bytes.NewReader(nil), 0, "image/png", "photo.png")
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusBadRequest)
}

func TestService_CreateProduct_DiscountExceedsPrice_RejectsBeforeTouchingRepository(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	store := NewMockStorage(t)
	svc := NewService(repo, branches, fakeTransactioner{}, store)

	branches.EXPECT().GetBranch(mock.Anything, uint(7), uint(5)).Return(&identity.Branch{ID: 5, OrgID: 7}, nil).Once()
	repo.EXPECT().GetCategory(mock.Anything, uint(7), uint(3)).Return(&Category{ID: 3, OrgID: 7}, nil).Once()

	in := validCreateProductRequest()
	in.Price = decimal.NewFromFloat(3.50)
	in.Discount = decimal.NewFromFloat(3.51)

	_, err := svc.CreateProduct(context.Background(), 7, in, bytes.NewReader(nil), 0, "image/png", "photo.png")
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusBadRequest)
}

func TestService_CreateProduct_DuplicateBarcode_ReturnsConflict(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	store := NewMockStorage(t)
	svc := NewService(repo, branches, fakeTransactioner{}, store)

	imgBytes := testProductImageBytes(t, 2, 3)
	branches.EXPECT().GetBranch(mock.Anything, uint(7), uint(5)).Return(&identity.Branch{ID: 5, OrgID: 7}, nil).Once()
	repo.EXPECT().GetCategory(mock.Anything, uint(7), uint(3)).Return(&Category{ID: 3, OrgID: 7}, nil).Once()
	repo.EXPECT().
		CreateProduct(mock.Anything, mock.Anything).
		Return(&pgconn.PgError{Code: "23505"}).
		Once()
	// Neither storage.Upload nor CreateProductImage must be called once the
	// product insert itself fails.

	_, err := svc.CreateProduct(context.Background(), 7, validCreateProductRequest(), bytes.NewReader(imgBytes), int64(len(imgBytes)), "image/png", "photo.png")
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusConflict)
}

func TestService_CreateProduct_UnexpectedRepositoryError_PropagatesAsIs(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	store := NewMockStorage(t)
	svc := NewService(repo, branches, fakeTransactioner{}, store)

	imgBytes := testProductImageBytes(t, 2, 3)
	branches.EXPECT().GetBranch(mock.Anything, uint(7), uint(5)).Return(&identity.Branch{ID: 5, OrgID: 7}, nil).Once()
	repo.EXPECT().GetCategory(mock.Anything, uint(7), uint(3)).Return(&Category{ID: 3, OrgID: 7}, nil).Once()
	dbErr := errors.New("connection refused")
	repo.EXPECT().CreateProduct(mock.Anything, mock.Anything).Return(dbErr).Once()

	_, err := svc.CreateProduct(context.Background(), 7, validCreateProductRequest(), bytes.NewReader(imgBytes), int64(len(imgBytes)), "image/png", "photo.png")
	require.ErrorIs(t, err, dbErr)
}

func TestService_CreateProduct_ImageUploadFails_RollsBackProduct(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	store := NewMockStorage(t)
	svc := NewService(repo, branches, fakeTransactioner{}, store)

	imgBytes := testProductImageBytes(t, 2, 3)
	branches.EXPECT().GetBranch(mock.Anything, uint(7), uint(5)).Return(&identity.Branch{ID: 5, OrgID: 7}, nil).Once()
	repo.EXPECT().GetCategory(mock.Anything, uint(7), uint(3)).Return(&Category{ID: 3, OrgID: 7}, nil).Once()
	repo.EXPECT().
		CreateProduct(mock.Anything, mock.Anything).
		Run(func(_ *gorm.DB, p *Product) { p.ID = 1 }).
		Return(nil).
		Once()
	store.EXPECT().
		Upload(mock.Anything, "products/1/photo.png", mock.Anything, int64(len(imgBytes)), "image/png").
		Return(errors.New("storage unavailable")).
		Once()
	// CreateProductImage must never be called once the upload itself fails.

	_, err := svc.CreateProduct(context.Background(), 7, validCreateProductRequest(), bytes.NewReader(imgBytes), int64(len(imgBytes)), "image/png", "photo.png")
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusInternalServerError)
}

func TestService_CreateProduct_ImageRowFails_CleansUpUploadedObject(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	store := NewMockStorage(t)
	svc := NewService(repo, branches, fakeTransactioner{}, store)

	imgBytes := testProductImageBytes(t, 2, 3)
	branches.EXPECT().GetBranch(mock.Anything, uint(7), uint(5)).Return(&identity.Branch{ID: 5, OrgID: 7}, nil).Once()
	repo.EXPECT().GetCategory(mock.Anything, uint(7), uint(3)).Return(&Category{ID: 3, OrgID: 7}, nil).Once()
	repo.EXPECT().
		CreateProduct(mock.Anything, mock.Anything).
		Run(func(_ *gorm.DB, p *Product) { p.ID = 1 }).
		Return(nil).
		Once()
	store.EXPECT().
		Upload(mock.Anything, "products/1/photo.png", mock.Anything, int64(len(imgBytes)), "image/png").
		Return(nil).
		Once()
	dbErr := errors.New("db write failed")
	repo.EXPECT().CreateProductImage(mock.Anything, mock.Anything).Return(dbErr).Once()
	// the now-orphaned object (no ProductImage row references it) must be
	// cleaned up - see Service.CreateProduct's comment on why a SQL
	// transaction rollback alone can't undo the storage upload.
	store.EXPECT().Delete(mock.Anything, "products/1/photo.png").Return(nil).Once()

	_, err := svc.CreateProduct(context.Background(), 7, validCreateProductRequest(), bytes.NewReader(imgBytes), int64(len(imgBytes)), "image/png", "photo.png")
	require.ErrorIs(t, err, dbErr)
}
