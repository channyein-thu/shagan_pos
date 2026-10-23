package catalog

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"shagan_pos/internal/common"
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
	store := NewMockStorage(t)
	svc := NewService(repo, fakeTransactioner{}, store)

	want := []Category{{ID: 1, OrgID: 7, NameI18n: "Food"}, {ID: 3, OrgID: 7, NameI18n: "Drinks"}}
	repo.EXPECT().ListCategories(mock.Anything, uint(7)).Return(want, nil).Once()

	got, err := svc.ListCategories(context.Background(), 7)
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func TestService_ListCategories_PropagatesRepositoryError(t *testing.T) {
	repo := NewMockRepository(t)
	store := NewMockStorage(t)
	svc := NewService(repo, fakeTransactioner{}, store)

	wantErr := common.SystemError("db read failed")
	repo.EXPECT().ListCategories(mock.Anything, uint(7)).Return(nil, wantErr).Once()

	_, err := svc.ListCategories(context.Background(), 7)
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusInternalServerError)
}

func TestService_ListCombos_DelegatesToRepository(t *testing.T) {
	repo := NewMockRepository(t)
	store := NewMockStorage(t)
	svc := NewService(repo, fakeTransactioner{}, store)

	combos := []Combo{{ID: 1, OrgID: 7, Name: "Breakfast Combo"}, {ID: 2, OrgID: 7, Name: "Lunch Combo"}}
	repo.EXPECT().ListCombos(mock.Anything, uint(7)).Return(combos, nil).Once()
	repo.EXPECT().ListComboItemsByComboIDs(mock.Anything, []uint{1, 2}).Return(nil, nil).Once()
	repo.EXPECT().ListComboImagesByComboIDs(mock.Anything, []uint{1, 2}).Return(nil, nil).Once()

	got, err := svc.ListCombos(context.Background(), 7)
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Equal(t, uint(1), got[0].ID)
	require.Empty(t, got[0].Images)
	require.Equal(t, uint(2), got[1].ID)
	require.Empty(t, got[1].Images)
}

// backend-recommendations #3: a till can't expand a combo into per-product
// sale lines without the component list.
func TestService_ListCombos_AttachesComponentItemsPerCombo(t *testing.T) {
	repo := NewMockRepository(t)
	store := NewMockStorage(t)
	svc := NewService(repo, fakeTransactioner{}, store)

	combos := []Combo{{ID: 1, OrgID: 7, Name: "Breakfast"}, {ID: 2, OrgID: 7, Name: "Lunch"}, {ID: 3, OrgID: 7, Name: "Legacy, no rows"}}
	// One query for every combo, not one per combo.
	repo.EXPECT().ListComboItemsByComboIDs(mock.Anything, []uint{1, 2, 3}).Return([]ComboItem{
		{ID: 10, ComboID: 1, ProductID: 5, Qty: 1},
		{ID: 11, ComboID: 1, ProductID: 6, Qty: 2},
		{ID: 12, ComboID: 2, ProductID: 7, Qty: 3},
	}, nil).Once()
	repo.EXPECT().ListComboImagesByComboIDs(mock.Anything, []uint{1, 2, 3}).Return(nil, nil).Once()
	repo.EXPECT().ListCombos(mock.Anything, uint(7)).Return(combos, nil).Once()

	got, err := svc.ListCombos(context.Background(), 7)
	require.NoError(t, err)
	require.Equal(t, []ComboItemResult{{ProductID: 5, Qty: 1}, {ProductID: 6, Qty: 2}}, got[0].Items)
	require.Equal(t, []ComboItemResult{{ProductID: 7, Qty: 3}}, got[1].Items)
	require.Equal(t, []ComboItemResult{}, got[2].Items, "never null, even for a combo with no item rows")
}

func TestService_ListCombos_ItemsLookupFails_PropagatesBeforeImages(t *testing.T) {
	repo := NewMockRepository(t)
	store := NewMockStorage(t)
	svc := NewService(repo, fakeTransactioner{}, store)

	wantErr := common.SystemError("db read failed")
	repo.EXPECT().ListCombos(mock.Anything, uint(7)).Return([]Combo{{ID: 1, OrgID: 7}}, nil).Once()
	repo.EXPECT().ListComboItemsByComboIDs(mock.Anything, []uint{1}).Return(nil, wantErr).Once()

	_, err := svc.ListCombos(context.Background(), 7)
	requireRestErrorStatus(t, err, http.StatusInternalServerError)
}

func TestService_ListCombos_AttachesImagesWithPresignedURLs(t *testing.T) {
	repo := NewMockRepository(t)
	store := NewMockStorage(t)
	svc := NewService(repo, fakeTransactioner{}, store)

	combos := []Combo{{ID: 1, OrgID: 7, Name: "Breakfast Combo"}}
	images := []ComboImage{{ID: 10, ComboID: 1, StorageKey: "combos/1/photo.png", Width: 2, Height: 3}}
	repo.EXPECT().ListCombos(mock.Anything, uint(7)).Return(combos, nil).Once()
	repo.EXPECT().ListComboItemsByComboIDs(mock.Anything, []uint{1}).Return(nil, nil).Once()
	repo.EXPECT().ListComboImagesByComboIDs(mock.Anything, []uint{1}).Return(images, nil).Once()
	store.EXPECT().
		PresignedURL(mock.Anything, "combos/1/photo.png", DefaultImageURLTTL).
		Return("https://minio.local/signed/photo.png", nil).
		Once()

	got, err := svc.ListCombos(context.Background(), 7)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Len(t, got[0].Images, 1)
	require.Equal(t, ComboImageResult{ID: 10, URL: "https://minio.local/signed/photo.png", Width: 2, Height: 3}, got[0].Images[0])
}

func TestService_ListCombos_PresignedURLFails_ReturnsSystemError(t *testing.T) {
	repo := NewMockRepository(t)
	store := NewMockStorage(t)
	svc := NewService(repo, fakeTransactioner{}, store)

	combos := []Combo{{ID: 1, OrgID: 7, Name: "Breakfast Combo"}}
	images := []ComboImage{{ID: 10, ComboID: 1, StorageKey: "combos/1/photo.png"}}
	repo.EXPECT().ListCombos(mock.Anything, uint(7)).Return(combos, nil).Once()
	repo.EXPECT().ListComboItemsByComboIDs(mock.Anything, []uint{1}).Return(nil, nil).Once()
	repo.EXPECT().ListComboImagesByComboIDs(mock.Anything, []uint{1}).Return(images, nil).Once()
	store.EXPECT().
		PresignedURL(mock.Anything, "combos/1/photo.png", DefaultImageURLTTL).
		Return("", errors.New("storage unavailable")).
		Once()

	_, err := svc.ListCombos(context.Background(), 7)
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusInternalServerError)
}

func TestService_ListCombos_PropagatesRepositoryError(t *testing.T) {
	repo := NewMockRepository(t)
	store := NewMockStorage(t)
	svc := NewService(repo, fakeTransactioner{}, store)

	wantErr := common.SystemError("db read failed")
	repo.EXPECT().ListCombos(mock.Anything, uint(7)).Return(nil, wantErr).Once()
	// ListComboImagesByComboIDs must never be called once the combo list
	// itself fails - no .EXPECT() set up for it means the mock fails the
	// test if it is.

	_, err := svc.ListCombos(context.Background(), 7)
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusInternalServerError)
}

func TestService_CreateCategory_BuildsModelAndPersists(t *testing.T) {
	repo := NewMockRepository(t)
	store := NewMockStorage(t)
	svc := NewService(repo, fakeTransactioner{}, store)

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
	store := NewMockStorage(t)
	svc := NewService(repo, fakeTransactioner{}, store)

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
	store := NewMockStorage(t)
	svc := NewService(repo, fakeTransactioner{}, store)

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
	store := NewMockStorage(t)
	svc := NewService(repo, fakeTransactioner{}, store)

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
	store := NewMockStorage(t)
	svc := NewService(repo, fakeTransactioner{}, store)

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
	store := NewMockStorage(t)
	svc := NewService(repo, fakeTransactioner{}, store)

	wantErr := common.NotFoundError("category not found")
	repo.EXPECT().GetCategory(mock.Anything, uint(7), uint(999)).Return(nil, wantErr).Once()
	// UpdateCategory must never be called on a not-found category.

	_, err := svc.UpdateCategory(context.Background(), 7, 999, UpdateCategoryRequest{})
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusNotFound)
}

func TestService_UpdateCategory_DuplicateName_ReturnsConflict(t *testing.T) {
	repo := NewMockRepository(t)
	store := NewMockStorage(t)
	svc := NewService(repo, fakeTransactioner{}, store)

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
	store := NewMockStorage(t)
	svc := NewService(repo, fakeTransactioner{}, store)

	existing := &Category{ID: 1, OrgID: 7, NameI18n: "Food"}
	repo.EXPECT().GetCategory(mock.Anything, uint(7), uint(1)).Return(existing, nil).Once()
	repo.EXPECT().ProductsExistForCategory(mock.Anything, uint(1)).Return(false, nil).Once()
	repo.EXPECT().DeleteCategory(mock.Anything, uint(1)).Return(nil).Once()

	err := svc.DeleteCategory(context.Background(), 7, 1)
	require.NoError(t, err)
}

func TestService_DeleteCategory_InUseByProducts_ReturnsConflict(t *testing.T) {
	repo := NewMockRepository(t)
	store := NewMockStorage(t)
	svc := NewService(repo, fakeTransactioner{}, store)

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
	store := NewMockStorage(t)
	svc := NewService(repo, fakeTransactioner{}, store)

	wantErr := common.NotFoundError("category not found")
	repo.EXPECT().GetCategory(mock.Anything, uint(7), uint(999)).Return(nil, wantErr).Once()
	// Neither ProductsExistForCategory nor DeleteCategory must be called on a
	// not-found category.

	err := svc.DeleteCategory(context.Background(), 7, 999)
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusNotFound)
}

func TestService_ListProducts_OrgWide_DelegatesToRepository(t *testing.T) {
	repo := NewMockRepository(t)
	store := NewMockStorage(t)
	svc := NewService(repo, fakeTransactioner{}, store)

	products := []Product{{ID: 1, OrgID: 7}, {ID: 2, OrgID: 7}}
	repo.EXPECT().ListProducts(mock.Anything, uint(7)).Return(products, nil).Once()
	repo.EXPECT().ListProductImagesByProductIDs(mock.Anything, []uint{1, 2}).Return(nil, nil).Once()

	got, err := svc.ListProducts(context.Background(), 7)
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Equal(t, uint(1), got[0].ID)
	require.Empty(t, got[0].Images)
	require.Equal(t, uint(2), got[1].ID)
	require.Empty(t, got[1].Images)
}

func TestService_ListProducts_AttachesImagesWithPresignedURLs(t *testing.T) {
	repo := NewMockRepository(t)
	store := NewMockStorage(t)
	svc := NewService(repo, fakeTransactioner{}, store)

	products := []Product{{ID: 1, OrgID: 7}}
	images := []ProductImage{{ID: 10, ProductID: 1, StorageKey: "products/1/photo.png", Width: 2, Height: 3}}
	repo.EXPECT().ListProducts(mock.Anything, uint(7)).Return(products, nil).Once()
	repo.EXPECT().ListProductImagesByProductIDs(mock.Anything, []uint{1}).Return(images, nil).Once()
	store.EXPECT().
		PresignedURL(mock.Anything, "products/1/photo.png", DefaultImageURLTTL).
		Return("https://minio.local/signed/photo.png", nil).
		Once()

	got, err := svc.ListProducts(context.Background(), 7)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Len(t, got[0].Images, 1)
	require.Equal(t, ProductImageResult{ID: 10, URL: "https://minio.local/signed/photo.png", Width: 2, Height: 3}, got[0].Images[0])
}

func TestService_ListProducts_PresignedURLFails_ReturnsSystemError(t *testing.T) {
	repo := NewMockRepository(t)
	store := NewMockStorage(t)
	svc := NewService(repo, fakeTransactioner{}, store)

	products := []Product{{ID: 1, OrgID: 7}}
	images := []ProductImage{{ID: 10, ProductID: 1, StorageKey: "products/1/photo.png"}}
	repo.EXPECT().ListProducts(mock.Anything, uint(7)).Return(products, nil).Once()
	repo.EXPECT().ListProductImagesByProductIDs(mock.Anything, []uint{1}).Return(images, nil).Once()
	store.EXPECT().
		PresignedURL(mock.Anything, "products/1/photo.png", DefaultImageURLTTL).
		Return("", errors.New("storage unavailable")).
		Once()

	_, err := svc.ListProducts(context.Background(), 7)
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusInternalServerError)
}

func TestService_ListProducts_PropagatesRepositoryError(t *testing.T) {
	repo := NewMockRepository(t)
	store := NewMockStorage(t)
	svc := NewService(repo, fakeTransactioner{}, store)

	wantErr := common.SystemError("db read failed")
	repo.EXPECT().ListProducts(mock.Anything, uint(7)).Return(nil, wantErr).Once()
	// ListProductImagesByProductIDs must never be called once the product
	// list itself fails - no .EXPECT() set up for it means the mock fails
	// the test if it is.

	_, err := svc.ListProducts(context.Background(), 7)
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusInternalServerError)
}

func TestService_GetProduct_DelegatesToRepository(t *testing.T) {
	repo := NewMockRepository(t)
	store := NewMockStorage(t)
	svc := NewService(repo, fakeTransactioner{}, store)

	want := &Product{ID: 1, OrgID: 7}
	repo.EXPECT().GetProduct(mock.Anything, uint(7), uint(1)).Return(want, nil).Once()
	repo.EXPECT().ListProductImagesByProductIDs(mock.Anything, []uint{1}).Return(nil, nil).Once()

	got, err := svc.GetProduct(context.Background(), 7, 1)
	require.NoError(t, err)
	require.Equal(t, uint(1), got.ID)
	require.Equal(t, uint(7), got.OrgID)
	require.Empty(t, got.Images)
}

func TestService_GetProduct_AttachesImagesWithPresignedURLs(t *testing.T) {
	repo := NewMockRepository(t)
	store := NewMockStorage(t)
	svc := NewService(repo, fakeTransactioner{}, store)

	want := &Product{ID: 1, OrgID: 7}
	images := []ProductImage{{ID: 10, ProductID: 1, StorageKey: "products/1/photo.png", Width: 2, Height: 3}}
	repo.EXPECT().GetProduct(mock.Anything, uint(7), uint(1)).Return(want, nil).Once()
	repo.EXPECT().ListProductImagesByProductIDs(mock.Anything, []uint{1}).Return(images, nil).Once()
	store.EXPECT().
		PresignedURL(mock.Anything, "products/1/photo.png", DefaultImageURLTTL).
		Return("https://minio.local/signed/photo.png", nil).
		Once()

	got, err := svc.GetProduct(context.Background(), 7, 1)
	require.NoError(t, err)
	require.Len(t, got.Images, 1)
	require.Equal(t, "https://minio.local/signed/photo.png", got.Images[0].URL)
}

func TestService_GetProduct_PropagatesNotFound(t *testing.T) {
	repo := NewMockRepository(t)
	store := NewMockStorage(t)
	svc := NewService(repo, fakeTransactioner{}, store)

	wantErr := common.NotFoundError("product not found")
	repo.EXPECT().GetProduct(mock.Anything, uint(7), uint(999)).Return(nil, wantErr).Once()

	_, err := svc.GetProduct(context.Background(), 7, 999)
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusNotFound)
}

func TestService_GetProductByBarcode_DelegatesToRepository(t *testing.T) {
	repo := NewMockRepository(t)
	store := NewMockStorage(t)
	svc := NewService(repo, fakeTransactioner{}, store)

	want := &Product{ID: 1, OrgID: 7, Barcode: "8850001234567"}
	repo.EXPECT().GetProductByBarcode(mock.Anything, uint(7), "8850001234567").Return(want, nil).Once()
	repo.EXPECT().ListProductImagesByProductIDs(mock.Anything, []uint{1}).Return(nil, nil).Once()

	got, err := svc.GetProductByBarcode(context.Background(), 7, "8850001234567")
	require.NoError(t, err)
	require.Equal(t, uint(1), got.ID)
	require.Equal(t, "8850001234567", got.Barcode)
	require.Empty(t, got.Images)
}

func TestService_GetProductByBarcode_AttachesImagesWithPresignedURLs(t *testing.T) {
	repo := NewMockRepository(t)
	store := NewMockStorage(t)
	svc := NewService(repo, fakeTransactioner{}, store)

	want := &Product{ID: 1, OrgID: 7, Barcode: "8850001234567"}
	images := []ProductImage{{ID: 10, ProductID: 1, StorageKey: "products/1/photo.png", Width: 2, Height: 3}}
	repo.EXPECT().GetProductByBarcode(mock.Anything, uint(7), "8850001234567").Return(want, nil).Once()
	repo.EXPECT().ListProductImagesByProductIDs(mock.Anything, []uint{1}).Return(images, nil).Once()
	store.EXPECT().
		PresignedURL(mock.Anything, "products/1/photo.png", DefaultImageURLTTL).
		Return("https://minio.local/signed/photo.png", nil).
		Once()

	got, err := svc.GetProductByBarcode(context.Background(), 7, "8850001234567")
	require.NoError(t, err)
	require.Len(t, got.Images, 1)
	require.Equal(t, "https://minio.local/signed/photo.png", got.Images[0].URL)
}

func TestService_GetProductByBarcode_PropagatesNotFound(t *testing.T) {
	repo := NewMockRepository(t)
	store := NewMockStorage(t)
	svc := NewService(repo, fakeTransactioner{}, store)

	wantErr := common.NotFoundError("product not found")
	repo.EXPECT().GetProductByBarcode(mock.Anything, uint(7), "no-such-code").Return(nil, wantErr).Once()

	_, err := svc.GetProductByBarcode(context.Background(), 7, "no-such-code")
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusNotFound)
}

func validCreateProductRequest() CreateProductRequest {
	modifier := "none"
	return CreateProductRequest{
		CategoryID: 3,
		Name:       "Iced Coffee",
		Barcode:    "8850001234567",
		Price:      decimal.NewFromInt(6500),
		Tax:        decimal.NewFromFloat(0.20),
		Threshold:  10,
		IsActive:   true,
		Modifier:   &modifier,
	}
}

func TestService_CreateProduct_HappyPath_CreatesProductAndImage(t *testing.T) {
	repo := NewMockRepository(t)
	store := NewMockStorage(t)
	svc := NewService(repo, fakeTransactioner{}, store)

	in := validCreateProductRequest()
	imgBytes := testProductImageBytes(t, 2, 3)
	file := bytes.NewReader(imgBytes)

	repo.EXPECT().GetCategory(mock.Anything, uint(7), uint(3)).Return(&Category{ID: 3, OrgID: 7}, nil).Once()
	repo.EXPECT().
		CreateProduct(mock.Anything, mock.MatchedBy(func(p *Product) bool {
			return p.OrgID == 7 && p.CategoryID == 3 && p.Barcode == "8850001234567"
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
	store := NewMockStorage(t)
	svc := NewService(repo, fakeTransactioner{}, store)

	in := validCreateProductRequest()
	in.Modifier = nil
	imgBytes := testProductImageBytes(t, 2, 3)

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
	store := NewMockStorage(t)
	svc := NewService(repo, fakeTransactioner{}, store)

	repo.EXPECT().GetCategory(mock.Anything, uint(7), uint(3)).Return(&Category{ID: 3, OrgID: 7}, nil).Once()
	// CreateProduct must never be called - not a decodable image.

	notAnImage := bytes.NewReader([]byte("this is not an image"))
	_, err := svc.CreateProduct(context.Background(), 7, validCreateProductRequest(), notAnImage, int64(notAnImage.Len()), "image/png", "photo.png")
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusBadRequest)
}

func TestService_CreateProduct_CategoryNotInOrg_PropagatesNotFound(t *testing.T) {
	repo := NewMockRepository(t)
	store := NewMockStorage(t)
	svc := NewService(repo, fakeTransactioner{}, store)

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
	store := NewMockStorage(t)
	svc := NewService(repo, fakeTransactioner{}, store)

	repo.EXPECT().GetCategory(mock.Anything, uint(7), uint(3)).Return(&Category{ID: 3, OrgID: 7}, nil).Once()
	// CreateProduct must never be called - price fails validation first.

	in := validCreateProductRequest()
	in.Price = decimal.Zero

	_, err := svc.CreateProduct(context.Background(), 7, in, bytes.NewReader(nil), 0, "image/png", "photo.png")
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusBadRequest)
}

func TestService_CreateProduct_NegativeDiscountPercent_RejectsBeforeTouchingRepository(t *testing.T) {
	repo := NewMockRepository(t)
	store := NewMockStorage(t)
	svc := NewService(repo, fakeTransactioner{}, store)

	repo.EXPECT().GetCategory(mock.Anything, uint(7), uint(3)).Return(&Category{ID: 3, OrgID: 7}, nil).Once()

	in := validCreateProductRequest()
	in.DiscountPercent = decimal.NewFromFloat(-0.01)

	_, err := svc.CreateProduct(context.Background(), 7, in, bytes.NewReader(nil), 0, "image/png", "photo.png")
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusBadRequest)
}

func TestService_CreateProduct_NegativeTax_RejectsBeforeTouchingRepository(t *testing.T) {
	repo := NewMockRepository(t)
	store := NewMockStorage(t)
	svc := NewService(repo, fakeTransactioner{}, store)

	repo.EXPECT().GetCategory(mock.Anything, uint(7), uint(3)).Return(&Category{ID: 3, OrgID: 7}, nil).Once()

	in := validCreateProductRequest()
	in.Tax = decimal.NewFromFloat(-0.01)

	_, err := svc.CreateProduct(context.Background(), 7, in, bytes.NewReader(nil), 0, "image/png", "photo.png")
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusBadRequest)
}

func TestService_CreateProduct_DiscountPercentOver100_RejectsBeforeTouchingRepository(t *testing.T) {
	repo := NewMockRepository(t)
	store := NewMockStorage(t)
	svc := NewService(repo, fakeTransactioner{}, store)

	repo.EXPECT().GetCategory(mock.Anything, uint(7), uint(3)).Return(&Category{ID: 3, OrgID: 7}, nil).Once()

	in := validCreateProductRequest()
	in.DiscountPercent = decimal.NewFromFloat(100.01)

	_, err := svc.CreateProduct(context.Background(), 7, in, bytes.NewReader(nil), 0, "image/png", "photo.png")
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusBadRequest)
}

// createProductCapturing runs a CreateProduct and returns the Product the
// service handed to the repository.
func createProductCapturing(t *testing.T, in CreateProductRequest) Product {
	t.Helper()
	repo := NewMockRepository(t)
	store := NewMockStorage(t)
	svc := NewService(repo, fakeTransactioner{}, store)
	imgBytes := testProductImageBytes(t, 2, 3)
	var saved Product
	repo.EXPECT().GetCategory(mock.Anything, uint(7), uint(3)).Return(&Category{ID: 3, OrgID: 7}, nil).Once()
	repo.EXPECT().CreateProduct(mock.Anything, mock.Anything).
		Run(func(_ *gorm.DB, p *Product) { p.ID = 1; saved = *p }).Return(nil).Once()
	store.EXPECT().Upload(mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Once()
	repo.EXPECT().CreateProductImage(mock.Anything, mock.Anything).Return(nil).Once()
	_, err := svc.CreateProduct(context.Background(), 7, in, bytes.NewReader(imgBytes), int64(len(imgBytes)), "image/png", "photo.png")
	require.NoError(t, err)
	return saved
}

// The Owner types a percent; the server computes and stores the sale price
// (rounded up to K 100) and the per-unit discount amount.
func TestService_CreateProduct_DiscountPercent_StoresRoundedSalePriceAndAmount(t *testing.T) {
	in := validCreateProductRequest() // price 6,500
	in.DiscountPercent = decimal.NewFromInt(10)

	got := createProductCapturing(t, in)

	require.True(t, decimal.NewFromInt(10).Equal(got.DiscountPercent))
	require.True(t, decimal.NewFromInt(5900).Equal(got.SalePrice), "6,500 less 10%% is 5,850, rounded up to 5,900 (got %s)", got.SalePrice)
	require.True(t, decimal.NewFromInt(600).Equal(got.Discount), "Discount is price - sale_price (got %s)", got.Discount)
}

func TestService_CreateProduct_ZeroPercent_SalePriceEqualsPrice(t *testing.T) {
	in := validCreateProductRequest()
	in.Price = decimal.RequireFromString("6750") // not a multiple of 100: must not be re-rounded
	in.DiscountPercent = decimal.Zero

	got := createProductCapturing(t, in)

	require.True(t, decimal.RequireFromString("6750").Equal(got.SalePrice))
	require.True(t, got.Discount.IsZero())
}

// A small price whose discounted figure would round up past itself is capped
// at its own price - never charged more than the original.
func TestService_CreateProduct_SalePriceNeverExceedsPrice(t *testing.T) {
	in := validCreateProductRequest()
	in.Price = decimal.NewFromInt(150)
	in.DiscountPercent = decimal.NewFromInt(10) // 135 -> 200, capped to 150

	got := createProductCapturing(t, in)

	require.True(t, decimal.NewFromInt(150).Equal(got.SalePrice))
	require.True(t, got.Discount.IsZero())
}

func TestService_CreateProduct_HundredPercent_IsFree(t *testing.T) {
	in := validCreateProductRequest()
	in.DiscountPercent = decimal.NewFromInt(100)

	got := createProductCapturing(t, in)

	require.True(t, got.SalePrice.IsZero())
	require.True(t, got.Discount.Equal(got.Price))
}

func TestService_CreateProduct_DuplicateBarcode_ReturnsConflict(t *testing.T) {
	repo := NewMockRepository(t)
	store := NewMockStorage(t)
	svc := NewService(repo, fakeTransactioner{}, store)

	imgBytes := testProductImageBytes(t, 2, 3)
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
	store := NewMockStorage(t)
	svc := NewService(repo, fakeTransactioner{}, store)

	imgBytes := testProductImageBytes(t, 2, 3)
	repo.EXPECT().GetCategory(mock.Anything, uint(7), uint(3)).Return(&Category{ID: 3, OrgID: 7}, nil).Once()
	dbErr := errors.New("connection refused")
	repo.EXPECT().CreateProduct(mock.Anything, mock.Anything).Return(dbErr).Once()

	_, err := svc.CreateProduct(context.Background(), 7, validCreateProductRequest(), bytes.NewReader(imgBytes), int64(len(imgBytes)), "image/png", "photo.png")
	require.ErrorIs(t, err, dbErr)
}

func TestService_CreateProduct_ImageUploadFails_RollsBackProduct(t *testing.T) {
	repo := NewMockRepository(t)
	store := NewMockStorage(t)
	svc := NewService(repo, fakeTransactioner{}, store)

	imgBytes := testProductImageBytes(t, 2, 3)
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
	store := NewMockStorage(t)
	svc := NewService(repo, fakeTransactioner{}, store)

	imgBytes := testProductImageBytes(t, 2, 3)
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

func TestService_UpdateProduct_HappyPath_UpdatesAndReturns(t *testing.T) {
	repo := NewMockRepository(t)
	store := NewMockStorage(t)
	svc := NewService(repo, fakeTransactioner{}, store)

	existing := &Product{ID: 1, OrgID: 7, Price: decimal.NewFromInt(6500), Tax: decimal.NewFromFloat(0.20)}
	updated := &Product{ID: 1, OrgID: 7, Name: "Hot Coffee"}
	name := "Hot Coffee"
	in := UpdateProductRequest{Name: &name}

	repo.EXPECT().GetProduct(mock.Anything, uint(7), uint(1)).Return(existing, nil).Once()
	repo.EXPECT().UpdateProduct(mock.Anything, uint(1), map[string]any{"name": "Hot Coffee"}).Return(nil).Once()
	repo.EXPECT().GetProduct(mock.Anything, uint(7), uint(1)).Return(updated, nil).Once()

	got, err := svc.UpdateProduct(context.Background(), 7, 1, in, nil, 0, "", "")
	require.NoError(t, err)
	require.Same(t, updated, got)
}

func TestService_UpdateProduct_CostPrice_AppliesDirectly(t *testing.T) {
	repo := NewMockRepository(t)
	store := NewMockStorage(t)
	svc := NewService(repo, fakeTransactioner{}, store)

	existing := &Product{ID: 1, OrgID: 7}
	updated := &Product{ID: 1, OrgID: 7, CostPrice: decimal.NewFromFloat(4.25)}
	cost := decimal.NewFromFloat(4.25)
	in := UpdateProductRequest{CostPrice: &cost}

	repo.EXPECT().GetProduct(mock.Anything, uint(7), uint(1)).Return(existing, nil).Once()
	repo.EXPECT().UpdateProduct(mock.Anything, uint(1), map[string]any{"cost_price": cost}).Return(nil).Once()
	repo.EXPECT().GetProduct(mock.Anything, uint(7), uint(1)).Return(updated, nil).Once()

	got, err := svc.UpdateProduct(context.Background(), 7, 1, in, nil, 0, "", "")
	require.NoError(t, err)
	require.Same(t, updated, got)
}

func TestService_UpdateProduct_NegativeCostPrice_ReturnsBadRequest(t *testing.T) {
	repo := NewMockRepository(t)
	store := NewMockStorage(t)
	svc := NewService(repo, fakeTransactioner{}, store)

	existing := &Product{ID: 1, OrgID: 7}
	cost := decimal.NewFromFloat(-1.00)

	repo.EXPECT().GetProduct(mock.Anything, uint(7), uint(1)).Return(existing, nil).Once()
	// UpdateProduct must never be called - rejected before any write.

	_, err := svc.UpdateProduct(context.Background(), 7, 1, UpdateProductRequest{CostPrice: &cost}, nil, 0, "", "")
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusBadRequest)
}

func TestService_UpdateProduct_NoFieldsProvided_SkipsWrite(t *testing.T) {
	repo := NewMockRepository(t)
	store := NewMockStorage(t)
	svc := NewService(repo, fakeTransactioner{}, store)

	existing := &Product{ID: 1, OrgID: 7}

	repo.EXPECT().GetProduct(mock.Anything, uint(7), uint(1)).Return(existing, nil).Twice()
	// UpdateProduct must never be called - no .EXPECT() set up for it means
	// the mock fails the test if it is.

	got, err := svc.UpdateProduct(context.Background(), 7, 1, UpdateProductRequest{}, nil, 0, "", "")
	require.NoError(t, err)
	require.Same(t, existing, got)
}

func TestService_UpdateProduct_PropagatesNotFound(t *testing.T) {
	repo := NewMockRepository(t)
	store := NewMockStorage(t)
	svc := NewService(repo, fakeTransactioner{}, store)

	wantErr := common.NotFoundError("product not found")
	repo.EXPECT().GetProduct(mock.Anything, uint(7), uint(999)).Return(nil, wantErr).Once()
	// UpdateProduct must never be called on a not-found product.

	_, err := svc.UpdateProduct(context.Background(), 7, 999, UpdateProductRequest{}, nil, 0, "", "")
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusNotFound)
}

func TestService_UpdateProduct_DuplicateBarcode_ReturnsConflict(t *testing.T) {
	repo := NewMockRepository(t)
	store := NewMockStorage(t)
	svc := NewService(repo, fakeTransactioner{}, store)

	existing := &Product{ID: 1, OrgID: 7}
	barcode := "8850009999999"
	in := UpdateProductRequest{Barcode: &barcode}

	repo.EXPECT().GetProduct(mock.Anything, uint(7), uint(1)).Return(existing, nil).Once()
	repo.EXPECT().
		UpdateProduct(mock.Anything, uint(1), map[string]any{"barcode": "8850009999999"}).
		Return(&pgconn.PgError{Code: "23505"}).
		Once()

	_, err := svc.UpdateProduct(context.Background(), 7, 1, in, nil, 0, "", "")
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusConflict)
}

func TestService_UpdateProduct_CategoryNotInOrg_PropagatesNotFound(t *testing.T) {
	repo := NewMockRepository(t)
	store := NewMockStorage(t)
	svc := NewService(repo, fakeTransactioner{}, store)

	existing := &Product{ID: 1, OrgID: 7}
	categoryID := uint(9)
	in := UpdateProductRequest{CategoryID: &categoryID}

	repo.EXPECT().GetProduct(mock.Anything, uint(7), uint(1)).Return(existing, nil).Once()
	wantErr := common.NotFoundError("category not found")
	repo.EXPECT().GetCategory(mock.Anything, uint(7), uint(9)).Return(nil, wantErr).Once()
	// UpdateProduct must never be called for a category that isn't ours.

	_, err := svc.UpdateProduct(context.Background(), 7, 1, in, nil, 0, "", "")
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusNotFound)
}

func TestService_UpdateProduct_DiscountPercentOutOfRange_RejectsBeforeWriting(t *testing.T) {
	for name, pct := range map[string]string{"negative": "-1", "over 100": "100.5"} {
		t.Run(name, func(t *testing.T) {
			repo := NewMockRepository(t)
			store := NewMockStorage(t)
			svc := NewService(repo, fakeTransactioner{}, store)
			existing := &Product{ID: 1, OrgID: 7, Price: decimal.NewFromInt(6500)}
			value := decimal.RequireFromString(pct)
			repo.EXPECT().GetProduct(mock.Anything, uint(7), uint(1)).Return(existing, nil).Once()
			// UpdateProduct must never be called

			_, err := svc.UpdateProduct(context.Background(), 7, 1, UpdateProductRequest{DiscountPercent: &value}, nil, 0, "", "")
			require.Error(t, err)
			requireRestErrorStatus(t, err, http.StatusBadRequest)
		})
	}
}

// updateCapturing runs UpdateProduct on `existing` and returns the column
// updates the service handed to the repository.
func updateCapturing(t *testing.T, existing *Product, in UpdateProductRequest) map[string]any {
	t.Helper()
	repo := NewMockRepository(t)
	store := NewMockStorage(t)
	svc := NewService(repo, fakeTransactioner{}, store)
	var got map[string]any
	repo.EXPECT().GetProduct(mock.Anything, uint(7), uint(1)).Return(existing, nil).Twice()
	repo.EXPECT().UpdateProduct(mock.Anything, uint(1), mock.Anything).
		Run(func(_ *gorm.DB, _ uint, updates map[string]any) { got = updates }).Return(nil).Once()
	_, err := svc.UpdateProduct(context.Background(), 7, 1, in, nil, 0, "", "")
	require.NoError(t, err)
	return got
}

// requireUpdates compares a column-update map by the printed value of each
// entry: decimal.Decimal structs with different internal exponents (0 vs 0.00)
// are the same amount but not require.Equal-identical.
func requireUpdates(t *testing.T, want map[string]string, got map[string]any) {
	t.Helper()
	printed := make(map[string]string, len(got))
	for k, v := range got {
		printed[k] = fmt.Sprint(v)
	}
	require.Equal(t, want, printed)
}

func TestService_UpdateProduct_ChangingThePercent_RecomputesSalePriceAndDiscount(t *testing.T) {
	existing := &Product{ID: 1, OrgID: 7, Price: decimal.NewFromInt(6500), SalePrice: decimal.NewFromInt(6500)}
	pct := decimal.NewFromInt(10)

	got := updateCapturing(t, existing, UpdateProductRequest{DiscountPercent: &pct})

	requireUpdates(t, map[string]string{"discount_percent": "10", "sale_price": "5900", "discount": "600"}, got)
}

// A price change keeps the product's percent and moves the sale price with it.
func TestService_UpdateProduct_ChangingThePrice_RecomputesSalePriceFromTheExistingPercent(t *testing.T) {
	existing := &Product{ID: 1, OrgID: 7, Price: decimal.NewFromInt(6500), DiscountPercent: decimal.NewFromInt(10), SalePrice: decimal.NewFromInt(5900)}
	price := decimal.NewFromInt(7000)

	got := updateCapturing(t, existing, UpdateProductRequest{Price: &price})

	requireUpdates(t, map[string]string{"price": "7000", "sale_price": "6300", "discount": "700"}, got) // the percent itself is unchanged
}

func TestService_UpdateProduct_PriceAndPercentTogether_UseBothNewValues(t *testing.T) {
	existing := &Product{ID: 1, OrgID: 7, Price: decimal.NewFromInt(6500), DiscountPercent: decimal.NewFromInt(10)}
	price, pct := decimal.NewFromInt(7000), decimal.NewFromInt(50)

	got := updateCapturing(t, existing, UpdateProductRequest{Price: &price, DiscountPercent: &pct})

	requireUpdates(t, map[string]string{"price": "7000", "discount_percent": "50", "sale_price": "3500", "discount": "3500"}, got)
}

func TestService_UpdateProduct_PercentBackToZero_SalePriceEqualsPrice(t *testing.T) {
	existing := &Product{ID: 1, OrgID: 7, Price: decimal.NewFromInt(6500), DiscountPercent: decimal.NewFromInt(10), SalePrice: decimal.NewFromInt(5900)}
	zero := decimal.Zero

	got := updateCapturing(t, existing, UpdateProductRequest{DiscountPercent: &zero})

	requireUpdates(t, map[string]string{"discount_percent": "0", "sale_price": "6500", "discount": "0"}, got)
}

// Editing something unrelated must not touch a legacy product's pricing -
// its sale price was backfilled exactly and is not re-rounded.
func TestService_UpdateProduct_UnrelatedField_LeavesPricingColumnsAlone(t *testing.T) {
	existing := &Product{ID: 1, OrgID: 7, Price: decimal.NewFromInt(6500), DiscountPercent: decimal.RequireFromString("4.62"), SalePrice: decimal.NewFromInt(6200)}
	name := "Renamed"

	got := updateCapturing(t, existing, UpdateProductRequest{Name: &name})

	require.Equal(t, map[string]any{"name": "Renamed"}, got)
}

func TestService_UpdateProduct_WithImage_ReplacesOldImageAfterCommit(t *testing.T) {
	repo := NewMockRepository(t)
	store := NewMockStorage(t)
	svc := NewService(repo, fakeTransactioner{}, store)

	existing := &Product{ID: 1, OrgID: 7}
	oldImages := []ProductImage{{ID: 10, ProductID: 1, StorageKey: "products/1/old-uuid-photo.png"}}
	imgBytes := testProductImageBytes(t, 2, 3)

	repo.EXPECT().GetProduct(mock.Anything, uint(7), uint(1)).Return(existing, nil).Once()
	repo.EXPECT().ListProductImagesByProductIDs(mock.Anything, []uint{1}).Return(oldImages, nil).Once()
	repo.EXPECT().DeleteProductImagesByProductID(mock.Anything, uint(1)).Return(nil).Once()
	store.EXPECT().
		Upload(mock.Anything, mock.MatchedBy(func(key string) bool {
			return strings.HasPrefix(key, "products/1/") && strings.HasSuffix(key, "-new-photo.png")
		}), mock.Anything, int64(len(imgBytes)), "image/png").
		Return(nil).
		Once()
	repo.EXPECT().
		CreateProductImage(mock.Anything, mock.MatchedBy(func(img *ProductImage) bool {
			return img.ProductID == 1 && img.Width == 2 && img.Height == 3 && strings.HasSuffix(img.StorageKey, "-new-photo.png")
		})).
		Return(nil).
		Once()
	// the old object is only deleted after the new row is durably
	// committed - see Service.UpdateProduct's comment.
	store.EXPECT().Delete(mock.Anything, "products/1/old-uuid-photo.png").Return(nil).Once()
	repo.EXPECT().GetProduct(mock.Anything, uint(7), uint(1)).Return(&Product{ID: 1, OrgID: 7}, nil).Once()

	_, err := svc.UpdateProduct(context.Background(), 7, 1, UpdateProductRequest{}, bytes.NewReader(imgBytes), int64(len(imgBytes)), "image/png", "new-photo.png")
	require.NoError(t, err)
}

func TestService_UpdateProduct_InvalidImage_ReturnsBadRequest(t *testing.T) {
	repo := NewMockRepository(t)
	store := NewMockStorage(t)
	svc := NewService(repo, fakeTransactioner{}, store)

	existing := &Product{ID: 1, OrgID: 7}
	repo.EXPECT().GetProduct(mock.Anything, uint(7), uint(1)).Return(existing, nil).Once()
	// DeleteProductImagesByProductID/CreateProductImage must never be
	// called - not a decodable image.

	notAnImage := bytes.NewReader([]byte("this is not an image"))
	_, err := svc.UpdateProduct(context.Background(), 7, 1, UpdateProductRequest{}, notAnImage, int64(notAnImage.Len()), "image/png", "photo.png")
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusBadRequest)
}

func TestService_UpdateProduct_ImageUploadFails_RollsBackFieldChangesToo(t *testing.T) {
	repo := NewMockRepository(t)
	store := NewMockStorage(t)
	svc := NewService(repo, fakeTransactioner{}, store)

	existing := &Product{ID: 1, OrgID: 7}
	name := "Hot Coffee"
	imgBytes := testProductImageBytes(t, 2, 3)

	repo.EXPECT().GetProduct(mock.Anything, uint(7), uint(1)).Return(existing, nil).Once()
	repo.EXPECT().ListProductImagesByProductIDs(mock.Anything, []uint{1}).Return(nil, nil).Once()
	repo.EXPECT().UpdateProduct(mock.Anything, uint(1), map[string]any{"name": "Hot Coffee"}).Return(nil).Once()
	repo.EXPECT().DeleteProductImagesByProductID(mock.Anything, uint(1)).Return(nil).Once()
	store.EXPECT().Upload(mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(errors.New("storage unavailable")).Once()
	// CreateProductImage must never be called once the upload itself fails,
	// and the name update above rolls back with the rest of the
	// transaction - fakeTransactioner doesn't model rollback, but the real
	// db.Transaction would never commit the name change either.

	_, err := svc.UpdateProduct(context.Background(), 7, 1, UpdateProductRequest{Name: &name}, bytes.NewReader(imgBytes), int64(len(imgBytes)), "image/png", "photo.png")
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusInternalServerError)
}

func TestService_UpdateProduct_ImageRowFails_CleansUpUploadedObject(t *testing.T) {
	repo := NewMockRepository(t)
	store := NewMockStorage(t)
	svc := NewService(repo, fakeTransactioner{}, store)

	existing := &Product{ID: 1, OrgID: 7}
	imgBytes := testProductImageBytes(t, 2, 3)

	repo.EXPECT().GetProduct(mock.Anything, uint(7), uint(1)).Return(existing, nil).Once()
	repo.EXPECT().ListProductImagesByProductIDs(mock.Anything, []uint{1}).Return(nil, nil).Once()
	repo.EXPECT().DeleteProductImagesByProductID(mock.Anything, uint(1)).Return(nil).Once()
	store.EXPECT().Upload(mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Once()
	dbErr := errors.New("db write failed")
	repo.EXPECT().CreateProductImage(mock.Anything, mock.Anything).Return(dbErr).Once()
	store.EXPECT().Delete(mock.Anything, mock.Anything).Return(nil).Once()

	_, err := svc.UpdateProduct(context.Background(), 7, 1, UpdateProductRequest{}, bytes.NewReader(imgBytes), int64(len(imgBytes)), "image/png", "photo.png")
	require.ErrorIs(t, err, dbErr)
}

func TestService_DeleteProduct_HappyPath_DeletesProductAndImages(t *testing.T) {
	repo := NewMockRepository(t)
	store := NewMockStorage(t)
	svc := NewService(repo, fakeTransactioner{}, store)

	existing := &Product{ID: 1, OrgID: 7}
	images := []ProductImage{{ID: 10, ProductID: 1, StorageKey: "products/1/photo.png"}}

	repo.EXPECT().GetProduct(mock.Anything, uint(7), uint(1)).Return(existing, nil).Once()
	repo.EXPECT().ComboItemsExistForProduct(mock.Anything, uint(1)).Return(false, nil).Once()
	repo.EXPECT().ProductHasHistory(mock.Anything, uint(1)).Return(false, nil).Once()
	repo.EXPECT().ListProductImagesByProductIDs(mock.Anything, []uint{1}).Return(images, nil).Once()
	repo.EXPECT().DeleteProductImagesByProductID(mock.Anything, uint(1)).Return(nil).Once()
	repo.EXPECT().DeleteProduct(mock.Anything, uint(1)).Return(nil).Once()
	// the storage object is only removed after the DB delete commits - see
	// Service.DeleteProduct's comment.
	store.EXPECT().Delete(mock.Anything, "products/1/photo.png").Return(nil).Once()

	err := svc.DeleteProduct(context.Background(), 7, 1)
	require.NoError(t, err)
}

func TestService_DeleteProduct_InUseByCombo_ReturnsConflict(t *testing.T) {
	repo := NewMockRepository(t)
	store := NewMockStorage(t)
	svc := NewService(repo, fakeTransactioner{}, store)

	existing := &Product{ID: 1, OrgID: 7}
	repo.EXPECT().GetProduct(mock.Anything, uint(7), uint(1)).Return(existing, nil).Once()
	repo.EXPECT().ComboItemsExistForProduct(mock.Anything, uint(1)).Return(true, nil).Once()
	// Neither the image lookup nor DeleteProduct must be called once a
	// combo reference is found - no .EXPECT() set up for either means the
	// mock fails the test if it is.

	err := svc.DeleteProduct(context.Background(), 7, 1)
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusConflict)
}

// backend-recommendations #14: sale lines, stock, ledger and PO lines carry
// no FK to products, so a hard delete would orphan them - block it (409).
func TestService_DeleteProduct_WithHistory_ReturnsConflictAndDeletesNothing(t *testing.T) {
	repo := NewMockRepository(t)
	store := NewMockStorage(t)
	svc := NewService(repo, fakeTransactioner{}, store)

	existing := &Product{ID: 1, OrgID: 7}
	repo.EXPECT().GetProduct(mock.Anything, uint(7), uint(1)).Return(existing, nil).Once()
	repo.EXPECT().ComboItemsExistForProduct(mock.Anything, uint(1)).Return(false, nil).Once()
	repo.EXPECT().ProductHasHistory(mock.Anything, uint(1)).Return(true, nil).Once()
	// No image lookup, DB delete or storage delete: nothing may be touched.

	err := svc.DeleteProduct(context.Background(), 7, 1)
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusConflict)
	require.Contains(t, err.Error(), "is_active", "the error must point at the alternative")
}

func TestService_DeleteProduct_HistoryCheckFails_PropagatesWithoutDeleting(t *testing.T) {
	repo := NewMockRepository(t)
	store := NewMockStorage(t)
	svc := NewService(repo, fakeTransactioner{}, store)

	existing := &Product{ID: 1, OrgID: 7}
	dbErr := errors.New("db read failed")
	repo.EXPECT().GetProduct(mock.Anything, uint(7), uint(1)).Return(existing, nil).Once()
	repo.EXPECT().ComboItemsExistForProduct(mock.Anything, uint(1)).Return(false, nil).Once()
	repo.EXPECT().ProductHasHistory(mock.Anything, uint(1)).Return(false, dbErr).Once()

	err := svc.DeleteProduct(context.Background(), 7, 1)
	require.ErrorIs(t, err, dbErr)
}

func TestService_DeleteProduct_PropagatesNotFound(t *testing.T) {
	repo := NewMockRepository(t)
	store := NewMockStorage(t)
	svc := NewService(repo, fakeTransactioner{}, store)

	wantErr := common.NotFoundError("product not found")
	repo.EXPECT().GetProduct(mock.Anything, uint(7), uint(999)).Return(nil, wantErr).Once()
	// Neither ComboItemsExistForProduct nor DeleteProduct must be called on
	// a not-found product.

	err := svc.DeleteProduct(context.Background(), 7, 999)
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusNotFound)
}

func TestService_DeleteProduct_RepositoryDeleteFails_PropagatesAsIs(t *testing.T) {
	repo := NewMockRepository(t)
	store := NewMockStorage(t)
	svc := NewService(repo, fakeTransactioner{}, store)

	existing := &Product{ID: 1, OrgID: 7}
	repo.EXPECT().GetProduct(mock.Anything, uint(7), uint(1)).Return(existing, nil).Once()
	repo.EXPECT().ComboItemsExistForProduct(mock.Anything, uint(1)).Return(false, nil).Once()
	repo.EXPECT().ProductHasHistory(mock.Anything, uint(1)).Return(false, nil).Once()
	repo.EXPECT().ListProductImagesByProductIDs(mock.Anything, []uint{1}).Return(nil, nil).Once()
	repo.EXPECT().DeleteProductImagesByProductID(mock.Anything, uint(1)).Return(nil).Once()
	dbErr := errors.New("db write failed")
	repo.EXPECT().DeleteProduct(mock.Anything, uint(1)).Return(dbErr).Once()
	// storage.Delete must never be called once the DB delete itself fails.

	err := svc.DeleteProduct(context.Background(), 7, 1)
	require.ErrorIs(t, err, dbErr)
}

func validCreateComboRequest() CreateComboRequest {
	return CreateComboRequest{
		Name:      "Breakfast Combo",
		Price:     decimal.NewFromFloat(5.00),
		ExpiresAt: time.Now().Add(24 * time.Hour),
		Items: []CreateComboItemRequest{
			{ProductID: 1, Qty: 1},
			{ProductID: 2, Qty: 2},
		},
	}
}

func TestService_CreateCombo_HappyPath_NoImage_CreatesComboAndItems(t *testing.T) {
	repo := NewMockRepository(t)
	store := NewMockStorage(t)
	svc := NewService(repo, fakeTransactioner{}, store)

	repo.EXPECT().GetProduct(mock.Anything, uint(7), uint(1)).Return(&Product{ID: 1, OrgID: 7}, nil).Once()
	repo.EXPECT().GetProduct(mock.Anything, uint(7), uint(2)).Return(&Product{ID: 2, OrgID: 7}, nil).Once()
	repo.EXPECT().
		CreateCombo(mock.Anything, mock.MatchedBy(func(c *Combo) bool {
			return c.OrgID == 7 && c.Name == "Breakfast Combo"
		})).
		Run(func(_ *gorm.DB, c *Combo) { c.ID = 9 }).
		Return(nil).
		Once()
	repo.EXPECT().
		CreateComboItems(mock.Anything, mock.MatchedBy(func(items []ComboItem) bool {
			return len(items) == 2 &&
				items[0].ComboID == 9 && items[0].ProductID == 1 && items[0].Qty == 1 &&
				items[1].ComboID == 9 && items[1].ProductID == 2 && items[1].Qty == 2
		})).
		Return(nil).
		Once()
	// storage.Upload/CreateComboImage must never be called - no image in
	// this request.

	got, err := svc.CreateCombo(context.Background(), 7, validCreateComboRequest(), nil, 0, "", "")
	require.NoError(t, err)
	require.Equal(t, uint(9), got.ID)
	require.Equal(t, uint(7), got.OrgID)
}

func TestService_CreateCombo_HappyPath_WithImage_CreatesComboItemsAndImage(t *testing.T) {
	repo := NewMockRepository(t)
	store := NewMockStorage(t)
	svc := NewService(repo, fakeTransactioner{}, store)

	imgBytes := testProductImageBytes(t, 2, 3)
	repo.EXPECT().GetProduct(mock.Anything, uint(7), uint(1)).Return(&Product{ID: 1, OrgID: 7}, nil).Once()
	repo.EXPECT().GetProduct(mock.Anything, uint(7), uint(2)).Return(&Product{ID: 2, OrgID: 7}, nil).Once()
	repo.EXPECT().
		CreateCombo(mock.Anything, mock.Anything).
		Run(func(_ *gorm.DB, c *Combo) { c.ID = 9 }).
		Return(nil).
		Once()
	repo.EXPECT().CreateComboItems(mock.Anything, mock.Anything).Return(nil).Once()
	store.EXPECT().
		Upload(mock.Anything, "combos/9/photo.png", mock.Anything, int64(len(imgBytes)), "image/png").
		Return(nil).
		Once()
	repo.EXPECT().
		CreateComboImage(mock.Anything, mock.MatchedBy(func(img *ComboImage) bool {
			return img.ComboID == 9 && img.StorageKey == "combos/9/photo.png" && img.Width == 2 && img.Height == 3
		})).
		Return(nil).
		Once()

	got, err := svc.CreateCombo(context.Background(), 7, validCreateComboRequest(), bytes.NewReader(imgBytes), int64(len(imgBytes)), "image/png", "photo.png")
	require.NoError(t, err)
	require.Equal(t, uint(9), got.ID)
}

func TestService_CreateCombo_InvalidImage_ReturnsBadRequest(t *testing.T) {
	repo := NewMockRepository(t)
	store := NewMockStorage(t)
	svc := NewService(repo, fakeTransactioner{}, store)

	repo.EXPECT().GetProduct(mock.Anything, uint(7), uint(1)).Return(&Product{ID: 1, OrgID: 7}, nil).Once()
	repo.EXPECT().GetProduct(mock.Anything, uint(7), uint(2)).Return(&Product{ID: 2, OrgID: 7}, nil).Once()
	// CreateCombo must never be called - not a decodable image.

	notAnImage := bytes.NewReader([]byte("this is not an image"))
	_, err := svc.CreateCombo(context.Background(), 7, validCreateComboRequest(), notAnImage, int64(notAnImage.Len()), "image/png", "photo.png")
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusBadRequest)
}

func TestService_CreateCombo_ImageUploadFails_RollsBackCombo(t *testing.T) {
	repo := NewMockRepository(t)
	store := NewMockStorage(t)
	svc := NewService(repo, fakeTransactioner{}, store)

	imgBytes := testProductImageBytes(t, 2, 3)
	repo.EXPECT().GetProduct(mock.Anything, uint(7), uint(1)).Return(&Product{ID: 1, OrgID: 7}, nil).Once()
	repo.EXPECT().GetProduct(mock.Anything, uint(7), uint(2)).Return(&Product{ID: 2, OrgID: 7}, nil).Once()
	repo.EXPECT().
		CreateCombo(mock.Anything, mock.Anything).
		Run(func(_ *gorm.DB, c *Combo) { c.ID = 9 }).
		Return(nil).
		Once()
	repo.EXPECT().CreateComboItems(mock.Anything, mock.Anything).Return(nil).Once()
	store.EXPECT().
		Upload(mock.Anything, "combos/9/photo.png", mock.Anything, int64(len(imgBytes)), "image/png").
		Return(errors.New("storage unavailable")).
		Once()
	// CreateComboImage must never be called once the upload itself fails.

	_, err := svc.CreateCombo(context.Background(), 7, validCreateComboRequest(), bytes.NewReader(imgBytes), int64(len(imgBytes)), "image/png", "photo.png")
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusInternalServerError)
}

func TestService_CreateCombo_ImageRowFails_CleansUpUploadedObject(t *testing.T) {
	repo := NewMockRepository(t)
	store := NewMockStorage(t)
	svc := NewService(repo, fakeTransactioner{}, store)

	imgBytes := testProductImageBytes(t, 2, 3)
	repo.EXPECT().GetProduct(mock.Anything, uint(7), uint(1)).Return(&Product{ID: 1, OrgID: 7}, nil).Once()
	repo.EXPECT().GetProduct(mock.Anything, uint(7), uint(2)).Return(&Product{ID: 2, OrgID: 7}, nil).Once()
	repo.EXPECT().
		CreateCombo(mock.Anything, mock.Anything).
		Run(func(_ *gorm.DB, c *Combo) { c.ID = 9 }).
		Return(nil).
		Once()
	repo.EXPECT().CreateComboItems(mock.Anything, mock.Anything).Return(nil).Once()
	store.EXPECT().
		Upload(mock.Anything, "combos/9/photo.png", mock.Anything, int64(len(imgBytes)), "image/png").
		Return(nil).
		Once()
	dbErr := errors.New("db write failed")
	repo.EXPECT().CreateComboImage(mock.Anything, mock.Anything).Return(dbErr).Once()
	// the now-orphaned object (no ComboImage row references it) must be
	// cleaned up - see Service.CreateCombo's comment on why a SQL
	// transaction rollback alone can't undo the storage upload.
	store.EXPECT().Delete(mock.Anything, "combos/9/photo.png").Return(nil).Once()

	_, err := svc.CreateCombo(context.Background(), 7, validCreateComboRequest(), bytes.NewReader(imgBytes), int64(len(imgBytes)), "image/png", "photo.png")
	require.ErrorIs(t, err, dbErr)
}

func TestService_CreateCombo_PriceNotPositive_RejectsBeforeTouchingRepository(t *testing.T) {
	repo := NewMockRepository(t)
	store := NewMockStorage(t)
	svc := NewService(repo, fakeTransactioner{}, store)

	// Neither GetProduct nor CreateCombo must be called - price fails
	// validation first.

	in := validCreateComboRequest()
	in.Price = decimal.Zero

	_, err := svc.CreateCombo(context.Background(), 7, in, nil, 0, "", "")
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusBadRequest)
}

func TestService_CreateCombo_ExpiresAtNotInFuture_RejectsBeforeTouchingRepository(t *testing.T) {
	repo := NewMockRepository(t)
	store := NewMockStorage(t)
	svc := NewService(repo, fakeTransactioner{}, store)

	in := validCreateComboRequest()
	in.ExpiresAt = time.Now().Add(-1 * time.Hour)

	_, err := svc.CreateCombo(context.Background(), 7, in, nil, 0, "", "")
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusBadRequest)
}

func TestService_CreateCombo_DuplicateProductID_RejectsBeforeTouchingRepository(t *testing.T) {
	repo := NewMockRepository(t)
	store := NewMockStorage(t)
	svc := NewService(repo, fakeTransactioner{}, store)

	// GetProduct/CreateCombo must never be called - the duplicate is caught
	// before any ownership check.

	in := validCreateComboRequest()
	in.Items = []CreateComboItemRequest{{ProductID: 1, Qty: 1}, {ProductID: 1, Qty: 2}}

	_, err := svc.CreateCombo(context.Background(), 7, in, nil, 0, "", "")
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusBadRequest)
}

func TestService_CreateCombo_ItemProductNotInOrg_PropagatesNotFound(t *testing.T) {
	repo := NewMockRepository(t)
	store := NewMockStorage(t)
	svc := NewService(repo, fakeTransactioner{}, store)

	repo.EXPECT().GetProduct(mock.Anything, uint(7), uint(1)).Return(&Product{ID: 1, OrgID: 7}, nil).Once()
	wantErr := common.NotFoundError("product not found")
	repo.EXPECT().GetProduct(mock.Anything, uint(7), uint(2)).Return(nil, wantErr).Once()
	// CreateCombo must never be called for a product that isn't ours - no
	// .EXPECT() set up for it means the mock fails the test if it is.

	_, err := svc.CreateCombo(context.Background(), 7, validCreateComboRequest(), nil, 0, "", "")
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusNotFound)
}

func TestService_CreateCombo_UnexpectedRepositoryError_PropagatesAsIs(t *testing.T) {
	repo := NewMockRepository(t)
	store := NewMockStorage(t)
	svc := NewService(repo, fakeTransactioner{}, store)

	repo.EXPECT().GetProduct(mock.Anything, uint(7), uint(1)).Return(&Product{ID: 1, OrgID: 7}, nil).Once()
	repo.EXPECT().GetProduct(mock.Anything, uint(7), uint(2)).Return(&Product{ID: 2, OrgID: 7}, nil).Once()
	dbErr := errors.New("connection refused")
	repo.EXPECT().CreateCombo(mock.Anything, mock.Anything).Return(dbErr).Once()
	// CreateComboItems must never be called once the combo insert itself
	// fails.

	_, err := svc.CreateCombo(context.Background(), 7, validCreateComboRequest(), nil, 0, "", "")
	require.ErrorIs(t, err, dbErr)
}

func TestService_CreateCombo_ComboItemsInsertFails_PropagatesAsIs(t *testing.T) {
	repo := NewMockRepository(t)
	store := NewMockStorage(t)
	svc := NewService(repo, fakeTransactioner{}, store)

	repo.EXPECT().GetProduct(mock.Anything, uint(7), uint(1)).Return(&Product{ID: 1, OrgID: 7}, nil).Once()
	repo.EXPECT().GetProduct(mock.Anything, uint(7), uint(2)).Return(&Product{ID: 2, OrgID: 7}, nil).Once()
	repo.EXPECT().
		CreateCombo(mock.Anything, mock.Anything).
		Run(func(_ *gorm.DB, c *Combo) { c.ID = 9 }).
		Return(nil).
		Once()
	dbErr := errors.New("db write failed")
	repo.EXPECT().CreateComboItems(mock.Anything, mock.Anything).Return(dbErr).Once()
	// storage.Upload must never be called once the items insert itself
	// fails.

	_, err := svc.CreateCombo(context.Background(), 7, validCreateComboRequest(), nil, 0, "", "")
	require.ErrorIs(t, err, dbErr)
}

func TestService_UpdateCombo_HappyPath_UpdatesAndReturns(t *testing.T) {
	repo := NewMockRepository(t)
	store := NewMockStorage(t)
	svc := NewService(repo, fakeTransactioner{}, store)

	existing := &Combo{ID: 9, OrgID: 7, Name: "Breakfast Combo", Price: decimal.NewFromFloat(5.00), ExpiresAt: time.Now().Add(24 * time.Hour)}
	updated := &Combo{ID: 9, OrgID: 7, Name: "Brunch Combo"}
	name := "Brunch Combo"
	in := UpdateComboRequest{Name: &name}

	repo.EXPECT().GetCombo(mock.Anything, uint(7), uint(9)).Return(existing, nil).Once()
	repo.EXPECT().UpdateCombo(mock.Anything, uint(9), map[string]any{"name": "Brunch Combo"}).Return(nil).Once()
	repo.EXPECT().GetCombo(mock.Anything, uint(7), uint(9)).Return(updated, nil).Once()

	got, err := svc.UpdateCombo(context.Background(), 7, 9, in, nil, 0, "", "")
	require.NoError(t, err)
	require.Same(t, updated, got)
}

func TestService_UpdateCombo_NoFieldsProvided_SkipsWrite(t *testing.T) {
	repo := NewMockRepository(t)
	store := NewMockStorage(t)
	svc := NewService(repo, fakeTransactioner{}, store)

	existing := &Combo{ID: 9, OrgID: 7, Name: "Breakfast Combo"}

	repo.EXPECT().GetCombo(mock.Anything, uint(7), uint(9)).Return(existing, nil).Twice()
	// UpdateCombo must never be called - no .EXPECT() set up for it means
	// the mock fails the test if it is.

	got, err := svc.UpdateCombo(context.Background(), 7, 9, UpdateComboRequest{}, nil, 0, "", "")
	require.NoError(t, err)
	require.Same(t, existing, got)
}

func TestService_UpdateCombo_PropagatesNotFound(t *testing.T) {
	repo := NewMockRepository(t)
	store := NewMockStorage(t)
	svc := NewService(repo, fakeTransactioner{}, store)

	wantErr := common.NotFoundError("combo not found")
	repo.EXPECT().GetCombo(mock.Anything, uint(7), uint(999)).Return(nil, wantErr).Once()
	// UpdateCombo must never be called on a not-found combo.

	_, err := svc.UpdateCombo(context.Background(), 7, 999, UpdateComboRequest{}, nil, 0, "", "")
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusNotFound)
}

func TestService_UpdateCombo_PriceOnly_ValidatesAgainstExistingExpiresAt(t *testing.T) {
	repo := NewMockRepository(t)
	store := NewMockStorage(t)
	svc := NewService(repo, fakeTransactioner{}, store)

	// existing ExpiresAt is already in the past - a Price-only update still
	// has to validate the combined state, so this must fail even though
	// ExpiresAt itself isn't part of the request.
	existing := &Combo{ID: 9, OrgID: 7, Price: decimal.NewFromFloat(5.00), ExpiresAt: time.Now().Add(-1 * time.Hour)}
	price := decimal.NewFromFloat(6.00)
	in := UpdateComboRequest{Price: &price}

	repo.EXPECT().GetCombo(mock.Anything, uint(7), uint(9)).Return(existing, nil).Once()
	// UpdateCombo must never be called - the combined state fails
	// validation first.

	_, err := svc.UpdateCombo(context.Background(), 7, 9, in, nil, 0, "", "")
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusBadRequest)
}

func TestService_UpdateCombo_WithImage_ReplacesOldImageAfterCommit(t *testing.T) {
	repo := NewMockRepository(t)
	store := NewMockStorage(t)
	svc := NewService(repo, fakeTransactioner{}, store)

	existing := &Combo{ID: 9, OrgID: 7, Name: "Breakfast Combo", Price: decimal.NewFromFloat(5.00), ExpiresAt: time.Now().Add(24 * time.Hour)}
	oldImages := []ComboImage{{ID: 20, ComboID: 9, StorageKey: "combos/9/old-uuid-photo.png"}}
	imgBytes := testProductImageBytes(t, 2, 3)

	repo.EXPECT().GetCombo(mock.Anything, uint(7), uint(9)).Return(existing, nil).Once()
	repo.EXPECT().ListComboImagesByComboIDs(mock.Anything, []uint{9}).Return(oldImages, nil).Once()
	repo.EXPECT().DeleteComboImagesByComboID(mock.Anything, uint(9)).Return(nil).Once()
	store.EXPECT().
		Upload(mock.Anything, mock.MatchedBy(func(key string) bool {
			return strings.HasPrefix(key, "combos/9/") && strings.HasSuffix(key, "-new-photo.png")
		}), mock.Anything, int64(len(imgBytes)), "image/png").
		Return(nil).
		Once()
	repo.EXPECT().
		CreateComboImage(mock.Anything, mock.MatchedBy(func(img *ComboImage) bool {
			return img.ComboID == 9 && img.Width == 2 && img.Height == 3 && strings.HasSuffix(img.StorageKey, "-new-photo.png")
		})).
		Return(nil).
		Once()
	// the old object is only deleted after the new row is durably
	// committed - see Service.UpdateCombo's comment.
	store.EXPECT().Delete(mock.Anything, "combos/9/old-uuid-photo.png").Return(nil).Once()
	repo.EXPECT().GetCombo(mock.Anything, uint(7), uint(9)).Return(&Combo{ID: 9, OrgID: 7}, nil).Once()

	_, err := svc.UpdateCombo(context.Background(), 7, 9, UpdateComboRequest{}, bytes.NewReader(imgBytes), int64(len(imgBytes)), "image/png", "new-photo.png")
	require.NoError(t, err)
}

func TestService_UpdateCombo_InvalidImage_ReturnsBadRequest(t *testing.T) {
	repo := NewMockRepository(t)
	store := NewMockStorage(t)
	svc := NewService(repo, fakeTransactioner{}, store)

	existing := &Combo{ID: 9, OrgID: 7, Name: "Breakfast Combo", Price: decimal.NewFromFloat(5.00), ExpiresAt: time.Now().Add(24 * time.Hour)}
	repo.EXPECT().GetCombo(mock.Anything, uint(7), uint(9)).Return(existing, nil).Once()
	// DeleteComboImagesByComboID/CreateComboImage must never be called -
	// not a decodable image.

	notAnImage := bytes.NewReader([]byte("this is not an image"))
	_, err := svc.UpdateCombo(context.Background(), 7, 9, UpdateComboRequest{}, notAnImage, int64(notAnImage.Len()), "image/png", "photo.png")
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusBadRequest)
}

func TestService_UpdateCombo_ImageRowFails_CleansUpUploadedObject(t *testing.T) {
	repo := NewMockRepository(t)
	store := NewMockStorage(t)
	svc := NewService(repo, fakeTransactioner{}, store)

	existing := &Combo{ID: 9, OrgID: 7, Name: "Breakfast Combo", Price: decimal.NewFromFloat(5.00), ExpiresAt: time.Now().Add(24 * time.Hour)}
	imgBytes := testProductImageBytes(t, 2, 3)

	repo.EXPECT().GetCombo(mock.Anything, uint(7), uint(9)).Return(existing, nil).Once()
	repo.EXPECT().ListComboImagesByComboIDs(mock.Anything, []uint{9}).Return(nil, nil).Once()
	repo.EXPECT().DeleteComboImagesByComboID(mock.Anything, uint(9)).Return(nil).Once()
	store.EXPECT().Upload(mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Once()
	dbErr := errors.New("db write failed")
	repo.EXPECT().CreateComboImage(mock.Anything, mock.Anything).Return(dbErr).Once()
	store.EXPECT().Delete(mock.Anything, mock.Anything).Return(nil).Once()

	_, err := svc.UpdateCombo(context.Background(), 7, 9, UpdateComboRequest{}, bytes.NewReader(imgBytes), int64(len(imgBytes)), "image/png", "photo.png")
	require.ErrorIs(t, err, dbErr)
}

func TestService_DeleteCombo_HappyPath_DeletesComboItemsAndImages(t *testing.T) {
	repo := NewMockRepository(t)
	store := NewMockStorage(t)
	svc := NewService(repo, fakeTransactioner{}, store)

	existing := &Combo{ID: 9, OrgID: 7, Name: "Breakfast Combo"}
	images := []ComboImage{{ID: 20, ComboID: 9, StorageKey: "combos/9/photo.png"}}

	repo.EXPECT().GetCombo(mock.Anything, uint(7), uint(9)).Return(existing, nil).Once()
	repo.EXPECT().ListComboImagesByComboIDs(mock.Anything, []uint{9}).Return(images, nil).Once()
	repo.EXPECT().DeleteComboImagesByComboID(mock.Anything, uint(9)).Return(nil).Once()
	repo.EXPECT().DeleteComboItemsByComboID(mock.Anything, uint(9)).Return(nil).Once()
	repo.EXPECT().DeleteCombo(mock.Anything, uint(9)).Return(nil).Once()
	// the storage object is only removed after the DB delete commits - see
	// Service.DeleteCombo's comment.
	store.EXPECT().Delete(mock.Anything, "combos/9/photo.png").Return(nil).Once()

	err := svc.DeleteCombo(context.Background(), 7, 9)
	require.NoError(t, err)
}

func TestService_DeleteCombo_PropagatesNotFound(t *testing.T) {
	repo := NewMockRepository(t)
	store := NewMockStorage(t)
	svc := NewService(repo, fakeTransactioner{}, store)

	wantErr := common.NotFoundError("combo not found")
	repo.EXPECT().GetCombo(mock.Anything, uint(7), uint(999)).Return(nil, wantErr).Once()
	// Neither the image lookup nor DeleteCombo must be called on a
	// not-found combo.

	err := svc.DeleteCombo(context.Background(), 7, 999)
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusNotFound)
}

func TestService_DeleteCombo_RepositoryDeleteFails_PropagatesAsIs(t *testing.T) {
	repo := NewMockRepository(t)
	store := NewMockStorage(t)
	svc := NewService(repo, fakeTransactioner{}, store)

	existing := &Combo{ID: 9, OrgID: 7, Name: "Breakfast Combo"}
	repo.EXPECT().GetCombo(mock.Anything, uint(7), uint(9)).Return(existing, nil).Once()
	repo.EXPECT().ListComboImagesByComboIDs(mock.Anything, []uint{9}).Return(nil, nil).Once()
	repo.EXPECT().DeleteComboImagesByComboID(mock.Anything, uint(9)).Return(nil).Once()
	repo.EXPECT().DeleteComboItemsByComboID(mock.Anything, uint(9)).Return(nil).Once()
	dbErr := errors.New("db write failed")
	repo.EXPECT().DeleteCombo(mock.Anything, uint(9)).Return(dbErr).Once()
	// storage.Delete must never be called once the DB delete itself fails.

	err := svc.DeleteCombo(context.Background(), 7, 9)
	require.ErrorIs(t, err, dbErr)
}
