package catalog

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"shagan_pos/internal/common"
)

func requireRestErrorStatus(t *testing.T, err error, status int) {
	t.Helper()
	var restErr common.RestError
	require.True(t, errors.As(err, &restErr), "expected a common.RestError, got %T: %v", err, err)
	require.Equal(t, status, restErr.Status)
}

func TestService_ListCategories_DelegatesToRepository(t *testing.T) {
	repo := NewMockRepository(t)
	svc := NewService(repo)

	want := []Category{{ID: 1, OrgID: 7, NameI18n: "Food"}, {ID: 3, OrgID: 7, NameI18n: "Drinks"}}
	repo.EXPECT().ListCategories(mock.Anything, uint(7)).Return(want, nil).Once()

	got, err := svc.ListCategories(context.Background(), 7)
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func TestService_ListCategories_PropagatesRepositoryError(t *testing.T) {
	repo := NewMockRepository(t)
	svc := NewService(repo)

	wantErr := common.SystemError("db read failed")
	repo.EXPECT().ListCategories(mock.Anything, uint(7)).Return(nil, wantErr).Once()

	_, err := svc.ListCategories(context.Background(), 7)
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusInternalServerError)
}

func TestService_CreateCategory_BuildsModelAndPersists(t *testing.T) {
	repo := NewMockRepository(t)
	svc := NewService(repo)

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
	svc := NewService(repo)

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
	svc := NewService(repo)

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
