package identity

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"shagan_pos/internal/authtoken"
	"shagan_pos/internal/common"
)

// testJWTSecret is a fixed secret for tests only - never read from the environment.
var testJWTSecret = []byte("test-secret-do-not-use-in-production")

// fakeTransactioner runs fc directly against a nil *gorm.DB, with no real
// database transaction - sufficient for unit tests that only exercise
// service-level orchestration against a mocked Repository, which never
// dereferences the *gorm.DB it's handed (it just records the call). The
// real common.Transactioner is satisfied natively by *gorm.DB in production.
type fakeTransactioner struct{}

func (fakeTransactioner) Transaction(fc func(tx *gorm.DB) error, _ ...*sql.TxOptions) error {
	return fc(nil)
}

func newTestService(repo Repository) *Service {
	return NewService(repo, fakeTransactioner{}, testJWTSecret, DefaultAccessTokenTTL, DefaultRefreshTokenTTL, DefaultStaffPINTokenTTL, DefaultManagerPINTokenTTL)
}

func TestService_CreateAccount_HashesPasswordBeforePersisting(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	const ownerPlaintext = "correct horse battery staple"
	const serviceCenterPlaintext = "another strong password"
	in := CreateAccountInput{
		OrganizationName:      "Acme Retail",
		OwnerEmail:            "owner@acme.test",
		OwnerPassword:         ownerPlaintext,
		ServiceCenterEmail:    "service-center@acme.test",
		ServiceCenterPassword: serviceCenterPlaintext,
	}

	repo.EXPECT().
		CreateOrganization(mock.Anything, mock.MatchedBy(func(org *Organization) bool {
			return org.Name == in.OrganizationName
		})).
		Run(func(_ *gorm.DB, org *Organization) { org.ID = 1 }).
		Return(nil).
		Once()

	repo.EXPECT().
		CreateUser(mock.Anything, mock.MatchedBy(func(u *User) bool {
			return u.AccountType == AccountTypeOwner &&
				u.OrgID == 1 &&
				u.Email != nil && *u.Email == in.OwnerEmail &&
				u.CredentialHash != ownerPlaintext && // must not be the plaintext
				bcrypt.CompareHashAndPassword([]byte(u.CredentialHash), []byte(ownerPlaintext)) == nil
		})).
		Run(func(_ *gorm.DB, u *User) { u.ID = 10 }).
		Return(nil).
		Once()

	repo.EXPECT().
		CreateUser(mock.Anything, mock.MatchedBy(func(u *User) bool {
			return u.AccountType == AccountTypeServiceCenter &&
				u.OrgID == 1 &&
				u.Email != nil && *u.Email == in.ServiceCenterEmail &&
				u.CredentialHash != serviceCenterPlaintext && // must not be the plaintext
				bcrypt.CompareHashAndPassword([]byte(u.CredentialHash), []byte(serviceCenterPlaintext)) == nil
		})).
		Run(func(_ *gorm.DB, u *User) { u.ID = 11 }).
		Return(nil).
		Once()

	result, err := svc.CreateAccount(context.Background(), in)
	require.NoError(t, err)
	require.Equal(t, uint(1), result.Organization.ID)
	require.Equal(t, uint(10), result.Owner.ID)
	require.Equal(t, uint(11), result.ServiceCenter.ID)
}

func TestService_CreateAccount_OwnerCreationFails_RollsBackAndNeverCreatesServiceCenter(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	repo.EXPECT().
		CreateOrganization(mock.Anything, mock.Anything).
		Run(func(_ *gorm.DB, org *Organization) { org.ID = 1 }).
		Return(nil).
		Once()

	wantErr := common.ConflictError("an account with this email already exists")
	repo.EXPECT().CreateUser(mock.Anything, mock.Anything).Return(wantErr).Once()
	// the service_center User must never be created if the owner insert fails
	// mid-transaction - no second .EXPECT() set up for CreateUser means the
	// mock fails the test if it's called again.

	_, err := svc.CreateAccount(context.Background(), CreateAccountInput{
		OrganizationName:      "Acme Retail",
		OwnerEmail:            "owner@acme.test",
		OwnerPassword:         "whatever-password",
		ServiceCenterEmail:    "service-center@acme.test",
		ServiceCenterPassword: "whatever-password-2",
	})

	require.Error(t, err)

	var gotErr common.RestError
	require.True(t, errors.As(err, &gotErr), "expected a common.RestError, got %T", err)
	require.Equal(t, wantErr.Status, gotErr.Status)
	require.Equal(t, wantErr.Message, gotErr.Message)
}

func TestService_CreatePosAccount_HashesPasswordBeforePersisting(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	const plaintext = "correct horse battery staple"
	in := CreatePosAccountInput{
		OrgID:    1,
		DeviceID: 2,
		Email:    "pos@acme.test",
		Password: plaintext,
	}

	expected := &User{ID: 1}

	repo.EXPECT().
		CreatePosAccount(mock.Anything, mock.MatchedBy(func(got CreatePosAccountInput) bool {
			if got.Password == plaintext {
				return false // must not be the plaintext
			}
			if got.OrgID != in.OrgID || got.DeviceID != in.DeviceID || got.Email != in.Email {
				return false // everything else must pass through unchanged
			}
			return bcrypt.CompareHashAndPassword([]byte(got.Password), []byte(plaintext)) == nil
		})).
		Return(expected, nil).
		Once()

	result, err := svc.CreatePosAccount(context.Background(), in)
	require.NoError(t, err)
	require.Same(t, expected, result)
}

func TestService_CreatePosAccount_PropagatesRepositoryError(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	wantErr := common.NotFoundError("device not found")
	repo.EXPECT().CreatePosAccount(mock.Anything, mock.Anything).Return(nil, wantErr).Once()

	_, err := svc.CreatePosAccount(context.Background(), CreatePosAccountInput{
		OrgID:    1,
		DeviceID: 999,
		Email:    "pos@acme.test",
		Password: "whatever-password",
	})

	require.Error(t, err)

	var gotErr common.RestError
	require.True(t, errors.As(err, &gotErr), "expected a common.RestError, got %T", err)
	require.Equal(t, wantErr.Status, gotErr.Status)
	require.Equal(t, wantErr.Message, gotErr.Message)
}

func TestService_ListOrganizations_DelegatesToRepository(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	want := []Organization{{ID: 1, Name: "Acme Retail"}, {ID: 2, Name: "Golden Star Retail"}}
	repo.EXPECT().ListOrganizations(mock.Anything).Return(want, nil).Once()

	got, err := svc.ListOrganizations(context.Background())
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func TestService_ListOrganizations_PropagatesRepositoryError(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	wantErr := common.SystemError("db read failed")
	repo.EXPECT().ListOrganizations(mock.Anything).Return(nil, wantErr).Once()

	_, err := svc.ListOrganizations(context.Background())
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusInternalServerError)
}

func TestService_ListPosAccounts_DelegatesToRepository(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	want := []User{{ID: 1, OrgID: 7, AccountType: AccountTypePos}}
	repo.EXPECT().ListPosAccounts(mock.Anything, uint(7)).Return(want, nil).Once()

	got, err := svc.ListPosAccounts(context.Background(), 7)
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func TestService_UpdateOrganizationStatus_DelegatesToRepository(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	repo.EXPECT().UpdateOrganizationStatus(mock.Anything, uint(1), OrganizationStatusSuspended).Return(nil).Once()

	err := svc.UpdateOrganizationStatus(context.Background(), 1, OrganizationStatusSuspended)
	require.NoError(t, err)
}

func TestService_UpdateOrganizationStatus_PropagatesRepositoryError(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	wantErr := common.SystemError("db write failed")
	repo.EXPECT().UpdateOrganizationStatus(mock.Anything, uint(1), OrganizationStatusSuspended).Return(wantErr).Once()

	err := svc.UpdateOrganizationStatus(context.Background(), 1, OrganizationStatusSuspended)
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusInternalServerError)
}

func TestService_UpdatePosAccountStatus_DelegatesToRepository(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	repo.EXPECT().UpdatePosAccountStatus(mock.Anything, uint(5), UserStatusSuspended).Return(nil).Once()

	err := svc.UpdatePosAccountStatus(context.Background(), 5, UserStatusSuspended)
	require.NoError(t, err)
}

func TestService_UpdatePosAccountStatus_PropagatesNotFound(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	wantErr := common.NotFoundError("pos account not found")
	repo.EXPECT().UpdatePosAccountStatus(mock.Anything, uint(999), UserStatusSuspended).Return(wantErr).Once()

	err := svc.UpdatePosAccountStatus(context.Background(), 999, UserStatusSuspended)
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusNotFound)
}

func TestService_ResetPosAccountPassword_HashesPasswordBeforePersisting(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	const plaintext = "a-brand-new-password"
	repo.EXPECT().
		ResetPosAccountPassword(mock.Anything, uint(5), mock.MatchedBy(func(hash string) bool {
			if hash == plaintext {
				return false // must not be the plaintext
			}
			return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plaintext)) == nil
		})).
		Return(nil).Once()

	err := svc.ResetPosAccountPassword(context.Background(), 5, plaintext)
	require.NoError(t, err)
}

func TestService_ResetPosAccountPassword_PropagatesNotFound(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	wantErr := common.NotFoundError("pos account not found")
	repo.EXPECT().ResetPosAccountPassword(mock.Anything, uint(999), mock.Anything).Return(wantErr).Once()

	err := svc.ResetPosAccountPassword(context.Background(), 999, "whatever-password")
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusNotFound)
}

func hashPassword(t *testing.T, plaintext string) string {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(plaintext), bcrypt.DefaultCost)
	require.NoError(t, err)
	return string(hash)
}

func requireRestErrorStatus(t *testing.T, err error, status int) {
	t.Helper()
	var restErr common.RestError
	require.True(t, errors.As(err, &restErr), "expected a common.RestError, got %T: %v", err, err)
	require.Equal(t, status, restErr.Status)
}

func TestService_Login_HappyPath(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	const plaintext = "correct horse battery staple"
	user := &User{ID: 42, OrgID: 7, CredentialHash: hashPassword(t, plaintext)}

	repo.EXPECT().GetUserByEmail(mock.Anything, "owner@acme.test").Return(user, nil).Once()
	repo.EXPECT().GetOrganization(mock.Anything, uint(7)).Return(&Organization{ID: 7, Status: OrganizationStatusActive}, nil).Once()

	var capturedHash string
	repo.EXPECT().
		CreateSession(mock.Anything, user.ID, mock.MatchedBy(func(hash string) bool {
			capturedHash = hash
			return len(hash) == 64 // sha256 hex-encoded
		}), mock.AnythingOfType("time.Time")).
		Return(&Session{ID: 1}, nil).
		Once()

	result, err := svc.Login(context.Background(), LoginRequest{Email: "owner@acme.test", Password: plaintext})
	require.NoError(t, err)
	require.NotEmpty(t, result.AccessToken)
	require.NotEmpty(t, result.RefreshToken)
	require.WithinDuration(t, time.Now().Add(DefaultRefreshTokenTTL), result.ExpiresAt, 5*time.Second)

	// the plaintext refresh token returned to the client must hash to exactly
	// what was persisted - otherwise a later refresh could never match it.
	sum := sha256.Sum256([]byte(result.RefreshToken))
	require.Equal(t, capturedHash, hex.EncodeToString(sum[:]))

	// access token must be a valid JWT signed with our secret, carrying the right claims -
	// parsed via the exact same code middleware.Auth uses to verify it in production
	claims, err := authtoken.ParseAccessToken(testJWTSecret, result.AccessToken)
	require.NoError(t, err)
	require.Equal(t, user.ID, claims.UserID)
	require.Equal(t, user.OrgID, claims.OrgID)
	require.Nil(t, claims.BranchID, "an owner/service_center login has no branch - the claim must be absent, not zero")
}

func TestService_Login_PosAccount_IncludesBranchIDInClaims(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	const plaintext = "correct horse battery staple"
	branchID := uint(9)
	user := &User{ID: 42, OrgID: 7, BranchID: &branchID, CredentialHash: hashPassword(t, plaintext)}

	repo.EXPECT().GetUserByEmail(mock.Anything, "pos1@acme.test").Return(user, nil).Once()
	repo.EXPECT().GetOrganization(mock.Anything, uint(7)).Return(&Organization{ID: 7, Status: OrganizationStatusActive}, nil).Once()
	repo.EXPECT().
		CreateSession(mock.Anything, user.ID, mock.Anything, mock.AnythingOfType("time.Time")).
		Return(&Session{ID: 1}, nil).
		Once()

	result, err := svc.Login(context.Background(), LoginRequest{Email: "pos1@acme.test", Password: plaintext})
	require.NoError(t, err)

	claims, err := authtoken.ParseAccessToken(testJWTSecret, result.AccessToken)
	require.NoError(t, err)
	require.NotNil(t, claims.BranchID)
	require.Equal(t, branchID, *claims.BranchID)
}

func TestService_Login_UnknownEmail_ReturnsGenericUnauthorized(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	repo.EXPECT().GetUserByEmail(mock.Anything, "nobody@acme.test").
		Return(nil, common.NotFoundError("user not found")).Once()
	// CreateSession must never be called - no .EXPECT() set up for it means
	// the mock fails the test if it is.

	_, err := svc.Login(context.Background(), LoginRequest{Email: "nobody@acme.test", Password: "whatever"})
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusUnauthorized)

	var restErr common.RestError
	errors.As(err, &restErr)
	require.Equal(t, invalidCredentialsMessage, restErr.Message)
}

func TestService_Login_WrongPassword_ReturnsSameGenericUnauthorized(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	user := &User{ID: 42, OrgID: 7, CredentialHash: hashPassword(t, "the-real-password")}
	repo.EXPECT().GetUserByEmail(mock.Anything, "owner@acme.test").Return(user, nil).Once()

	_, err := svc.Login(context.Background(), LoginRequest{Email: "owner@acme.test", Password: "wrong-password"})
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusUnauthorized)

	var restErr common.RestError
	errors.As(err, &restErr)
	require.Equal(t, invalidCredentialsMessage, restErr.Message, "must match the unknown-email message exactly - no user enumeration")
}

func TestService_Login_SuspendedAccount_ReturnsSameGenericUnauthorized(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	const plaintext = "correct horse battery staple"
	user := &User{ID: 42, OrgID: 7, Status: UserStatusSuspended, CredentialHash: hashPassword(t, plaintext)}
	repo.EXPECT().GetUserByEmail(mock.Anything, "pos1@acme.test").Return(user, nil).Once()
	// CreateSession must never be called - a suspended account never gets a session.

	_, err := svc.Login(context.Background(), LoginRequest{Email: "pos1@acme.test", Password: plaintext})
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusUnauthorized)

	var restErr common.RestError
	errors.As(err, &restErr)
	require.Equal(t, invalidCredentialsMessage, restErr.Message, "must match the wrong-password message exactly - a suspended account must not be distinguishable from a wrong password")
}

func TestService_Login_SuspendedOrganization_ReturnsSameGenericUnauthorized(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	const plaintext = "correct horse battery staple"
	user := &User{ID: 42, OrgID: 7, CredentialHash: hashPassword(t, plaintext)}
	repo.EXPECT().GetUserByEmail(mock.Anything, "owner@acme.test").Return(user, nil).Once()
	repo.EXPECT().GetOrganization(mock.Anything, uint(7)).Return(&Organization{ID: 7, Status: OrganizationStatusSuspended}, nil).Once()
	// CreateSession must never be called - a whole-tenant suspension blocks
	// every one of that org's users, not just individually-suspended ones.

	_, err := svc.Login(context.Background(), LoginRequest{Email: "owner@acme.test", Password: plaintext})
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusUnauthorized)

	var restErr common.RestError
	errors.As(err, &restErr)
	require.Equal(t, invalidCredentialsMessage, restErr.Message)
}

func TestService_Login_UnexpectedRepositoryError_PropagatesAsIs(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	dbErr := errors.New("connection refused")
	repo.EXPECT().GetUserByEmail(mock.Anything, "owner@acme.test").Return(nil, dbErr).Once()

	_, err := svc.Login(context.Background(), LoginRequest{Email: "owner@acme.test", Password: "whatever"})
	require.ErrorIs(t, err, dbErr)
}

func TestService_Login_CreateSessionFails_PropagatesError(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	const plaintext = "correct horse battery staple"
	user := &User{ID: 42, OrgID: 7, CredentialHash: hashPassword(t, plaintext)}

	repo.EXPECT().GetUserByEmail(mock.Anything, "owner@acme.test").Return(user, nil).Once()
	repo.EXPECT().GetOrganization(mock.Anything, uint(7)).Return(&Organization{ID: 7, Status: OrganizationStatusActive}, nil).Once()
	sessionErr := common.SystemError("db write failed")
	repo.EXPECT().
		CreateSession(mock.Anything, user.ID, mock.Anything, mock.AnythingOfType("time.Time")).
		Return(nil, sessionErr).
		Once()

	_, err := svc.Login(context.Background(), LoginRequest{Email: "owner@acme.test", Password: plaintext})
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusInternalServerError)
}

const testRefreshPlaintext = "the-refresh-token-plaintext"

func TestService_RefreshSession_HappyPath_RotatesToken(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	oldHash := hashRefreshToken(testRefreshPlaintext)
	session := &Session{ID: 1, UserID: 42, RefreshHash: oldHash, ExpiresAt: time.Now().Add(time.Hour)}
	branchID := uint(9)
	user := &User{ID: 42, OrgID: 7, BranchID: &branchID}

	repo.EXPECT().GetSessionByRefreshHash(mock.Anything, oldHash).Return(session, nil).Once()
	repo.EXPECT().GetUserByID(mock.Anything, user.ID).Return(user, nil).Once()
	repo.EXPECT().GetOrganization(mock.Anything, uint(7)).Return(&Organization{ID: 7, Status: OrganizationStatusActive}, nil).Once()

	var newHash string
	repo.EXPECT().
		CreateSession(mock.Anything, user.ID, mock.MatchedBy(func(h string) bool {
			newHash = h
			return h != oldHash // must be a genuinely new token, not the same one reused
		}), mock.AnythingOfType("time.Time")).
		Return(&Session{ID: 2}, nil).
		Once()
	repo.EXPECT().RevokeSession(mock.Anything, session.ID).Return(nil).Once()

	result, err := svc.RefreshSession(context.Background(), RefreshRequest{RefreshToken: testRefreshPlaintext})
	require.NoError(t, err)
	require.NotEmpty(t, result.AccessToken)
	require.NotEmpty(t, result.RefreshToken)
	require.Equal(t, newHash, hashRefreshToken(result.RefreshToken))

	claims, err := authtoken.ParseAccessToken(testJWTSecret, result.AccessToken)
	require.NoError(t, err)
	require.Equal(t, user.ID, claims.UserID)
	require.Equal(t, user.OrgID, claims.OrgID)
	require.NotNil(t, claims.BranchID)
	require.Equal(t, branchID, *claims.BranchID)
}

func TestService_RefreshSession_UnknownToken_ReturnsGenericUnauthorized(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	repo.EXPECT().
		GetSessionByRefreshHash(mock.Anything, mock.Anything).
		Return(nil, common.NotFoundError("session not found")).
		Once()

	_, err := svc.RefreshSession(context.Background(), RefreshRequest{RefreshToken: "nonsense"})
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusUnauthorized)

	var restErr common.RestError
	errors.As(err, &restErr)
	require.Equal(t, invalidRefreshTokenMessage, restErr.Message)
}

func TestService_RefreshSession_RevokedToken_ReturnsSameGenericUnauthorized(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	revokedAt := time.Now().Add(-time.Minute)
	session := &Session{ID: 1, UserID: 42, ExpiresAt: time.Now().Add(time.Hour), RevokedAt: &revokedAt}
	repo.EXPECT().GetSessionByRefreshHash(mock.Anything, mock.Anything).Return(session, nil).Once()

	_, err := svc.RefreshSession(context.Background(), RefreshRequest{RefreshToken: testRefreshPlaintext})
	require.Error(t, err)

	var restErr common.RestError
	errors.As(err, &restErr)
	require.Equal(t, invalidRefreshTokenMessage, restErr.Message, "must match the unknown-token message exactly")
}

func TestService_RefreshSession_ExpiredToken_ReturnsSameGenericUnauthorized(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	session := &Session{ID: 1, UserID: 42, ExpiresAt: time.Now().Add(-time.Minute)}
	repo.EXPECT().GetSessionByRefreshHash(mock.Anything, mock.Anything).Return(session, nil).Once()

	_, err := svc.RefreshSession(context.Background(), RefreshRequest{RefreshToken: testRefreshPlaintext})
	require.Error(t, err)

	var restErr common.RestError
	errors.As(err, &restErr)
	require.Equal(t, invalidRefreshTokenMessage, restErr.Message, "must match the unknown-token message exactly")
}

func TestService_RefreshSession_UserGone_ReturnsGenericUnauthorized(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	session := &Session{ID: 1, UserID: 42, ExpiresAt: time.Now().Add(time.Hour)}
	repo.EXPECT().GetSessionByRefreshHash(mock.Anything, mock.Anything).Return(session, nil).Once()
	repo.EXPECT().GetUserByID(mock.Anything, session.UserID).Return(nil, common.NotFoundError("user not found")).Once()

	_, err := svc.RefreshSession(context.Background(), RefreshRequest{RefreshToken: testRefreshPlaintext})
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusUnauthorized)
}

func TestService_RefreshSession_SuspendedAccount_ReturnsSameGenericUnauthorized(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	session := &Session{ID: 1, UserID: 42, ExpiresAt: time.Now().Add(time.Hour)}
	user := &User{ID: 42, OrgID: 7, Status: UserStatusSuspended}
	repo.EXPECT().GetSessionByRefreshHash(mock.Anything, mock.Anything).Return(session, nil).Once()
	repo.EXPECT().GetUserByID(mock.Anything, session.UserID).Return(user, nil).Once()
	// CreateSession/RevokeSession must never be called - a refresh token
	// belonging to a now-suspended account must stop working immediately.

	_, err := svc.RefreshSession(context.Background(), RefreshRequest{RefreshToken: testRefreshPlaintext})
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusUnauthorized)

	var restErr common.RestError
	errors.As(err, &restErr)
	require.Equal(t, invalidRefreshTokenMessage, restErr.Message)
}

func TestService_RefreshSession_SuspendedOrganization_ReturnsSameGenericUnauthorized(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	session := &Session{ID: 1, UserID: 42, ExpiresAt: time.Now().Add(time.Hour)}
	user := &User{ID: 42, OrgID: 7}
	repo.EXPECT().GetSessionByRefreshHash(mock.Anything, mock.Anything).Return(session, nil).Once()
	repo.EXPECT().GetUserByID(mock.Anything, session.UserID).Return(user, nil).Once()
	repo.EXPECT().GetOrganization(mock.Anything, uint(7)).Return(&Organization{ID: 7, Status: OrganizationStatusSuspended}, nil).Once()
	// CreateSession/RevokeSession must never be called.

	_, err := svc.RefreshSession(context.Background(), RefreshRequest{RefreshToken: testRefreshPlaintext})
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusUnauthorized)

	var restErr common.RestError
	errors.As(err, &restErr)
	require.Equal(t, invalidRefreshTokenMessage, restErr.Message)
}

func TestService_RefreshSession_RotationFails_PropagatesError(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	session := &Session{ID: 1, UserID: 42, ExpiresAt: time.Now().Add(time.Hour)}
	user := &User{ID: 42, OrgID: 7}
	repo.EXPECT().GetSessionByRefreshHash(mock.Anything, mock.Anything).Return(session, nil).Once()
	repo.EXPECT().GetUserByID(mock.Anything, session.UserID).Return(user, nil).Once()
	repo.EXPECT().GetOrganization(mock.Anything, uint(7)).Return(&Organization{ID: 7, Status: OrganizationStatusActive}, nil).Once()

	dbErr := common.SystemError("db write failed")
	repo.EXPECT().
		CreateSession(mock.Anything, user.ID, mock.Anything, mock.AnythingOfType("time.Time")).
		Return(nil, dbErr).
		Once()

	_, err := svc.RefreshSession(context.Background(), RefreshRequest{RefreshToken: testRefreshPlaintext})
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusInternalServerError)
}

func TestService_Logout_HappyPath_RevokesSession(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	session := &Session{ID: 1, UserID: 42}
	repo.EXPECT().GetSessionByRefreshHash(mock.Anything, hashRefreshToken(testRefreshPlaintext)).Return(session, nil).Once()
	repo.EXPECT().RevokeSession(mock.Anything, session.ID).Return(nil).Once()

	err := svc.Logout(context.Background(), LogoutRequest{RefreshToken: testRefreshPlaintext})
	require.NoError(t, err)
}

func TestService_Logout_UnknownToken_SucceedsIdempotently(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	repo.EXPECT().
		GetSessionByRefreshHash(mock.Anything, mock.Anything).
		Return(nil, common.NotFoundError("session not found")).
		Once()
	// RevokeSession must never be called - no .EXPECT() set up for it means
	// the mock fails the test if it is.

	err := svc.Logout(context.Background(), LogoutRequest{RefreshToken: "nonsense"})
	require.NoError(t, err)
}

func TestService_Logout_AlreadyRevoked_SucceedsIdempotently(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	revokedAt := time.Now().Add(-time.Minute)
	session := &Session{ID: 1, UserID: 42, RevokedAt: &revokedAt}
	repo.EXPECT().GetSessionByRefreshHash(mock.Anything, mock.Anything).Return(session, nil).Once()

	err := svc.Logout(context.Background(), LogoutRequest{RefreshToken: testRefreshPlaintext})
	require.NoError(t, err)
}

func TestService_Logout_RepositoryFailure_Propagates(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	session := &Session{ID: 1, UserID: 42}
	repo.EXPECT().GetSessionByRefreshHash(mock.Anything, mock.Anything).Return(session, nil).Once()
	dbErr := common.SystemError("db write failed")
	repo.EXPECT().RevokeSession(mock.Anything, session.ID).Return(dbErr).Once()

	err := svc.Logout(context.Background(), LogoutRequest{RefreshToken: testRefreshPlaintext})
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusInternalServerError)
}

// GetMe and UpdateMe are pure passthroughs in the service - all the actual
// logic (which fields are safe to self-update) lives in the repository,
// which we don't unit test. These just guard against the delegation itself
// silently breaking (wrong userID forwarded, result/error swallowed, etc.)
// if someone edits this method later.

func TestService_GetMe_DelegatesToRepository(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	want := &User{ID: 42, OrgID: 7}
	repo.EXPECT().GetUserByID(mock.Anything, uint(42)).Return(want, nil).Once()

	got, err := svc.GetMe(context.Background(), 42)
	require.NoError(t, err)
	require.Same(t, want, got)
}

func TestService_GetMe_PropagatesRepositoryError(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	wantErr := common.NotFoundError("user not found")
	repo.EXPECT().GetUserByID(mock.Anything, uint(42)).Return(nil, wantErr).Once()

	_, err := svc.GetMe(context.Background(), 42)
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusNotFound)
}

func TestService_UpdateMe_DelegatesToRepository(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	name := "Chan Nyein"
	in := UpdateMeRequest{Name: &name}
	want := &User{ID: 42, Name: &name}

	repo.EXPECT().UpdateMe(mock.Anything, uint(42), in).Return(want, nil).Once()

	got, err := svc.UpdateMe(context.Background(), 42, in)
	require.NoError(t, err)
	require.Same(t, want, got)
}

func TestService_UpdateMe_PropagatesRepositoryError(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	in := UpdateMeRequest{}
	wantErr := common.ConflictError("an account with this email already exists")
	repo.EXPECT().UpdateMe(mock.Anything, uint(42), in).Return(nil, wantErr).Once()

	_, err := svc.UpdateMe(context.Background(), 42, in)
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusConflict)
}

// Branch endpoints are pure passthroughs in the service too - the org-scoping
// (a caller can only see/touch branches in their own org) lives entirely in
// the repository, which we don't unit test. These just guard the delegation.

func TestService_ListBranches_DelegatesToRepository(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	want := []Branch{{ID: 1, OrgID: 7}, {ID: 2, OrgID: 7}}
	repo.EXPECT().ListBranches(mock.Anything, uint(7)).Return(want, nil).Once()

	got, err := svc.ListBranches(context.Background(), 7)
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func TestService_ListBranches_PropagatesRepositoryError(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	wantErr := common.SystemError("db read failed")
	repo.EXPECT().ListBranches(mock.Anything, uint(7)).Return(nil, wantErr).Once()

	_, err := svc.ListBranches(context.Background(), 7)
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusInternalServerError)
}

func TestService_CreateBranch_DelegatesToRepository(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	in := CreateBranchRequest{Name: "Main Branch", Status: BranchStatusActive}
	want := &Branch{ID: 1, OrgID: 7, Name: "Main Branch"}
	repo.EXPECT().CreateBranch(mock.Anything, uint(7), in).Return(want, nil).Once()

	got, err := svc.CreateBranch(context.Background(), 7, in)
	require.NoError(t, err)
	require.Same(t, want, got)
}

func TestService_CreateBranch_PropagatesRepositoryError(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	in := CreateBranchRequest{Name: "Main Branch", Status: BranchStatusActive}
	wantErr := common.SystemError("db write failed")
	repo.EXPECT().CreateBranch(mock.Anything, uint(7), in).Return(nil, wantErr).Once()

	_, err := svc.CreateBranch(context.Background(), 7, in)
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusInternalServerError)
}

func TestService_GetBranch_DelegatesToRepository(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	want := &Branch{ID: 1, OrgID: 7}
	repo.EXPECT().GetBranch(mock.Anything, uint(7), uint(1)).Return(want, nil).Once()

	got, err := svc.GetBranch(context.Background(), 7, 1)
	require.NoError(t, err)
	require.Same(t, want, got)
}

func TestService_GetBranch_PropagatesNotFound(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	wantErr := common.NotFoundError("branch not found")
	repo.EXPECT().GetBranch(mock.Anything, uint(7), uint(999)).Return(nil, wantErr).Once()

	_, err := svc.GetBranch(context.Background(), 7, 999)
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusNotFound)
}

func TestService_UpdateBranch_DelegatesToRepository(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	name := "Renamed Branch"
	in := UpdateBranchRequest{Name: &name}
	want := &Branch{ID: 1, OrgID: 7, Name: name}
	repo.EXPECT().UpdateBranch(mock.Anything, uint(7), uint(1), in).Return(want, nil).Once()

	got, err := svc.UpdateBranch(context.Background(), 7, 1, in)
	require.NoError(t, err)
	require.Same(t, want, got)
}

func TestService_UpdateBranch_PropagatesNotFound(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	in := UpdateBranchRequest{}
	wantErr := common.NotFoundError("branch not found")
	repo.EXPECT().UpdateBranch(mock.Anything, uint(7), uint(999), in).Return(nil, wantErr).Once()

	_, err := svc.UpdateBranch(context.Background(), 7, 999, in)
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusNotFound)
}

func TestService_ListBranchStaff_DelegatesToRepository(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	want := []Staff{{ID: 1, BranchID: 1}, {ID: 2, BranchID: 1}}
	repo.EXPECT().ListBranchStaff(mock.Anything, uint(7), uint(1)).Return(want, nil).Once()

	got, err := svc.ListBranchStaff(context.Background(), 7, 1)
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func TestService_ListBranchStaff_PropagatesNotFound(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	wantErr := common.NotFoundError("branch not found")
	repo.EXPECT().ListBranchStaff(mock.Anything, uint(7), uint(999)).Return(nil, wantErr).Once()

	_, err := svc.ListBranchStaff(context.Background(), 7, 999)
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusNotFound)
}

func TestService_ListBranchManagers_DelegatesToRepository(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	want := []Staff{{ID: 1, BranchID: 1, RoleID: 3}}
	repo.EXPECT().ListBranchManagers(mock.Anything, uint(7), uint(1), "approve_void").Return(want, nil).Once()

	got, err := svc.ListBranchManagers(context.Background(), 7, 1, "approve_void")
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func TestService_ListBranchManagers_PropagatesNotFound(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	wantErr := common.NotFoundError("branch not found")
	repo.EXPECT().ListBranchManagers(mock.Anything, uint(7), uint(999), "approve_void").Return(nil, wantErr).Once()

	_, err := svc.ListBranchManagers(context.Background(), 7, 999, "approve_void")
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusNotFound)
}

func TestService_ListStaff_DelegatesToRepository(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	want := []Staff{{ID: 1, BranchID: 1}, {ID: 2, BranchID: 2}}
	repo.EXPECT().ListStaff(mock.Anything, uint(7), (*uint)(nil)).Return(want, nil).Once()

	got, err := svc.ListStaff(context.Background(), 7, nil)
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func TestService_ListStaff_PassesThroughCallerBranchID(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	branchID := uint(3)
	want := []Staff{{ID: 1, BranchID: 3}}
	repo.EXPECT().ListStaff(mock.Anything, uint(7), &branchID).Return(want, nil).Once()

	got, err := svc.ListStaff(context.Background(), 7, &branchID)
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func TestService_ListStaff_PropagatesRepositoryError(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	wantErr := common.SystemError("db read failed")
	repo.EXPECT().ListStaff(mock.Anything, uint(7), (*uint)(nil)).Return(nil, wantErr).Once()

	_, err := svc.ListStaff(context.Background(), 7, nil)
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusInternalServerError)
}

func TestService_CreateStaff_HashesPinBeforePersisting(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	const plaintextPin = "123456"
	in := CreateStaffRequest{BranchID: 1, Name: "Cashier Joe", Role: 3, Pin: plaintextPin, Phone: "555-0000", Status: StaffStatusActive}

	expected := &Staff{ID: 1, BranchID: 1}

	repo.EXPECT().
		CreateStaff(mock.Anything, uint(7), mock.MatchedBy(func(got CreateStaffRequest) bool {
			if got.Pin == plaintextPin {
				return false // must not be the plaintext
			}
			if got.BranchID != in.BranchID || got.Name != in.Name || got.Role != in.Role || got.Phone != in.Phone || got.Status != in.Status {
				return false // everything else must pass through unchanged
			}
			return bcrypt.CompareHashAndPassword([]byte(got.Pin), []byte(plaintextPin)) == nil
		})).
		Return(expected, nil).
		Once()

	result, err := svc.CreateStaff(context.Background(), 7, in)
	require.NoError(t, err)
	require.Same(t, expected, result)
}

func TestService_CreateStaff_PropagatesRepositoryError(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	in := CreateStaffRequest{BranchID: 999, Name: "Cashier Joe", Role: 3, Pin: "123456", Phone: "555-0000", Status: StaffStatusActive}
	wantErr := common.NotFoundError("branch not found")
	repo.EXPECT().CreateStaff(mock.Anything, uint(7), mock.Anything).Return(nil, wantErr).Once()

	_, err := svc.CreateStaff(context.Background(), 7, in)
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusNotFound)
}

func TestService_GetStaff_DelegatesToRepository(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	want := &Staff{ID: 1, BranchID: 1}
	repo.EXPECT().GetStaff(mock.Anything, uint(7), uint(1)).Return(want, nil).Once()

	got, err := svc.GetStaff(context.Background(), 7, 1)
	require.NoError(t, err)
	require.Same(t, want, got)
}

func TestService_GetStaff_PropagatesNotFound(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	wantErr := common.NotFoundError("staff not found")
	repo.EXPECT().GetStaff(mock.Anything, uint(7), uint(999)).Return(nil, wantErr).Once()

	_, err := svc.GetStaff(context.Background(), 7, 999)
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusNotFound)
}

func TestService_UpdateStaff_HashesPinWhenProvided(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	const plaintextPin = "567890"
	in := UpdateStaffRequest{Pin: &[]string{plaintextPin}[0]}
	want := &Staff{ID: 1, BranchID: 1}

	repo.EXPECT().
		UpdateStaff(mock.Anything, uint(7), uint(1), mock.MatchedBy(func(got UpdateStaffRequest) bool {
			return got.Pin != nil && *got.Pin != plaintextPin &&
				bcrypt.CompareHashAndPassword([]byte(*got.Pin), []byte(plaintextPin)) == nil
		})).
		Return(want, nil).
		Once()

	result, err := svc.UpdateStaff(context.Background(), 7, 1, in)
	require.NoError(t, err)
	require.Same(t, want, result)
}

func TestService_UpdateStaff_NoPinProvided_PassesThroughUnchanged(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	name := "Renamed Cashier"
	in := UpdateStaffRequest{Name: &name}
	want := &Staff{ID: 1, Name: name}

	repo.EXPECT().UpdateStaff(mock.Anything, uint(7), uint(1), in).Return(want, nil).Once()

	result, err := svc.UpdateStaff(context.Background(), 7, 1, in)
	require.NoError(t, err)
	require.Same(t, want, result)
}

func TestService_UpdateStaff_PropagatesNotFound(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	in := UpdateStaffRequest{}
	wantErr := common.NotFoundError("staff not found")
	repo.EXPECT().UpdateStaff(mock.Anything, uint(7), uint(999), in).Return(nil, wantErr).Once()

	_, err := svc.UpdateStaff(context.Background(), 7, 999, in)
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusNotFound)
}

func TestService_VerifyStaffPIN_HappyPath_NoBranchRestriction(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	const plaintext = "123456"
	staff := &Staff{ID: 5, BranchID: 3, RoleID: 2, PinHash: hashPassword(t, plaintext)}
	repo.EXPECT().GetStaff(mock.Anything, uint(7), uint(5)).Return(staff, nil).Once()
	repo.EXPECT().ListRolePermissions(mock.Anything, uint(2)).
		Return([]Permission{{Code: "access_pos_portal"}, {Code: "apply_manual_discount"}}, nil).Once()

	result, err := svc.VerifyStaffPIN(context.Background(), 7, nil, 5, VerifyStaffPINRequest{Pin: plaintext})
	require.NoError(t, err)
	require.Equal(t, *staff, result.Staff)
	require.NotEmpty(t, result.Token)

	claims, err := authtoken.ParseStaffToken(testJWTSecret, result.Token)
	require.NoError(t, err)
	require.Equal(t, staff.ID, claims.StaffID)
	require.Equal(t, staff.BranchID, claims.BranchID)
	require.Equal(t, staff.RoleID, claims.RoleID)
	require.ElementsMatch(t, []string{"access_pos_portal", "apply_manual_discount"}, claims.Permissions)
}

func TestService_VerifyStaffPIN_MatchingBranch_Succeeds(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	const plaintext = "123456"
	staff := &Staff{ID: 5, BranchID: 3, RoleID: 2, PinHash: hashPassword(t, plaintext)}
	repo.EXPECT().GetStaff(mock.Anything, uint(7), uint(5)).Return(staff, nil).Once()
	repo.EXPECT().ListRolePermissions(mock.Anything, uint(2)).Return([]Permission{}, nil).Once()

	callerBranch := uint(3)
	result, err := svc.VerifyStaffPIN(context.Background(), 7, &callerBranch, 5, VerifyStaffPINRequest{Pin: plaintext})
	require.NoError(t, err)
	require.NotEmpty(t, result.Token)
}

func TestService_VerifyStaffPIN_WrongBranch_ReturnsGenericUnauthorized(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	const plaintext = "123456"
	staff := &Staff{ID: 5, BranchID: 3, RoleID: 2, PinHash: hashPassword(t, plaintext)}
	repo.EXPECT().GetStaff(mock.Anything, uint(7), uint(5)).Return(staff, nil).Once()

	callerBranch := uint(99) // a different branch than the staff's own
	_, err := svc.VerifyStaffPIN(context.Background(), 7, &callerBranch, 5, VerifyStaffPINRequest{Pin: plaintext})
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusUnauthorized)

	var restErr common.RestError
	errors.As(err, &restErr)
	require.Equal(t, invalidStaffPINMessage, restErr.Message)
}

func TestService_VerifyStaffPIN_WrongPin_ReturnsSameGenericUnauthorized(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	staff := &Staff{ID: 5, BranchID: 3, RoleID: 2, PinHash: hashPassword(t, "123456")}
	repo.EXPECT().GetStaff(mock.Anything, uint(7), uint(5)).Return(staff, nil).Once()
	repo.EXPECT().UpdateStaffPinAttempts(mock.Anything, uint(5), 1, (*time.Time)(nil)).Return(nil).Once()

	_, err := svc.VerifyStaffPIN(context.Background(), 7, nil, 5, VerifyStaffPINRequest{Pin: "999999"})
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusUnauthorized)

	var restErr common.RestError
	errors.As(err, &restErr)
	require.Equal(t, invalidStaffPINMessage, restErr.Message)
}

func TestService_VerifyStaffPIN_StaffNotFound_ReturnsSameGenericUnauthorized(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	repo.EXPECT().GetStaff(mock.Anything, uint(7), uint(999)).
		Return(nil, common.NotFoundError("staff not found")).Once()

	_, err := svc.VerifyStaffPIN(context.Background(), 7, nil, 999, VerifyStaffPINRequest{Pin: "123456"})
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusUnauthorized)

	var restErr common.RestError
	errors.As(err, &restErr)
	require.Equal(t, invalidStaffPINMessage, restErr.Message,
		"must match the wrong-PIN/wrong-branch message exactly - no staff-ID enumeration")
}

func TestService_VerifyStaffPIN_UnexpectedRepositoryError_PropagatesAsIs(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	dbErr := errors.New("connection refused")
	repo.EXPECT().GetStaff(mock.Anything, uint(7), uint(5)).Return(nil, dbErr).Once()

	_, err := svc.VerifyStaffPIN(context.Background(), 7, nil, 5, VerifyStaffPINRequest{Pin: "123456"})
	require.ErrorIs(t, err, dbErr)
}

func TestService_VerifyStaffPIN_WrongPin_ReachesThreshold_LocksOut(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	// already at 4 prior failures - this 5th wrong guess must trigger the lock.
	staff := &Staff{ID: 5, BranchID: 3, RoleID: 2, PinHash: hashPassword(t, "123456"), FailedPinAttempts: DefaultPinLockoutThreshold - 1}
	repo.EXPECT().GetStaff(mock.Anything, uint(7), uint(5)).Return(staff, nil).Once()
	repo.EXPECT().
		UpdateStaffPinAttempts(mock.Anything, uint(5), DefaultPinLockoutThreshold, mock.MatchedBy(func(lockedUntil *time.Time) bool {
			return lockedUntil != nil && lockedUntil.After(time.Now())
		})).
		Return(nil).Once()

	_, err := svc.VerifyStaffPIN(context.Background(), 7, nil, 5, VerifyStaffPINRequest{Pin: "999999"})
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusUnauthorized)
}

func TestService_VerifyStaffPIN_CurrentlyLockedOut_RejectsWithoutCheckingPin(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	lockedUntil := time.Now().Add(10 * time.Minute)
	// correct PIN, but locked out - must still be rejected, and must not
	// even reach the point of touching PIN-attempt bookkeeping again.
	staff := &Staff{ID: 5, BranchID: 3, RoleID: 2, PinHash: hashPassword(t, "123456"), PinLockedUntil: &lockedUntil}
	repo.EXPECT().GetStaff(mock.Anything, uint(7), uint(5)).Return(staff, nil).Once()

	_, err := svc.VerifyStaffPIN(context.Background(), 7, nil, 5, VerifyStaffPINRequest{Pin: "123456"})
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusTooManyRequests)
}

func TestService_VerifyStaffPIN_LockoutExpired_AllowsRetryAgain(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	expiredLock := time.Now().Add(-1 * time.Minute)
	const plaintext = "123456"
	staff := &Staff{ID: 5, BranchID: 3, RoleID: 2, PinHash: hashPassword(t, plaintext), FailedPinAttempts: DefaultPinLockoutThreshold, PinLockedUntil: &expiredLock}
	repo.EXPECT().GetStaff(mock.Anything, uint(7), uint(5)).Return(staff, nil).Once()
	repo.EXPECT().UpdateStaffPinAttempts(mock.Anything, uint(5), 0, (*time.Time)(nil)).Return(nil).Once()
	repo.EXPECT().ListRolePermissions(mock.Anything, uint(2)).Return([]Permission{}, nil).Once()

	result, err := svc.VerifyStaffPIN(context.Background(), 7, nil, 5, VerifyStaffPINRequest{Pin: plaintext})
	require.NoError(t, err)
	require.NotEmpty(t, result.Token)
}

func TestService_VerifyManagerPIN_HappyPath_GrantsShortLivedToken(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	const plaintext = "654321"
	staff := &Staff{ID: 9, BranchID: 3, RoleID: 3, PinHash: hashPassword(t, plaintext)}
	repo.EXPECT().GetStaff(mock.Anything, uint(7), uint(9)).Return(staff, nil).Once()
	repo.EXPECT().ListRolePermissions(mock.Anything, uint(3)).
		Return([]Permission{{Code: "apply_manual_discount"}, {Code: "approve_void"}, {Code: "approve_return"}}, nil).Once()

	result, err := svc.VerifyManagerPIN(context.Background(), 7, nil, 9, VerifyManagerPINRequest{Pin: plaintext, Permission: "approve_void"})
	require.NoError(t, err)
	require.Equal(t, *staff, result.Staff)
	require.WithinDuration(t, time.Now().Add(DefaultManagerPINTokenTTL), result.ExpiresAt, 5*time.Second)

	claims, err := authtoken.ParseStaffToken(testJWTSecret, result.Token)
	require.NoError(t, err)
	require.Equal(t, staff.ID, claims.StaffID)
	require.Equal(t, staff.BranchID, claims.BranchID)
	require.Equal(t, staff.RoleID, claims.RoleID)
}

func TestService_VerifyManagerPIN_RoleLacksPermission_ReturnsGenericUnauthorized(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	const plaintext = "654321"
	// a super_staff-shaped role: has apply_manual_discount but not approve_void
	staff := &Staff{ID: 9, BranchID: 3, RoleID: 2, PinHash: hashPassword(t, plaintext)}
	repo.EXPECT().GetStaff(mock.Anything, uint(7), uint(9)).Return(staff, nil).Once()
	repo.EXPECT().ListRolePermissions(mock.Anything, uint(2)).
		Return([]Permission{{Code: "apply_manual_discount"}}, nil).Once()

	_, err := svc.VerifyManagerPIN(context.Background(), 7, nil, 9, VerifyManagerPINRequest{Pin: plaintext, Permission: "approve_void"})
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusUnauthorized)

	var restErr common.RestError
	errors.As(err, &restErr)
	require.Equal(t, invalidManagerPINMessage, restErr.Message)
}

func TestService_VerifyManagerPIN_WrongBranch_ReturnsGenericUnauthorized(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	staff := &Staff{ID: 9, BranchID: 3, RoleID: 3, PinHash: hashPassword(t, "654321")}
	repo.EXPECT().GetStaff(mock.Anything, uint(7), uint(9)).Return(staff, nil).Once()

	callerBranch := uint(99)
	_, err := svc.VerifyManagerPIN(context.Background(), 7, &callerBranch, 9, VerifyManagerPINRequest{Pin: "654321", Permission: "approve_void"})
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusUnauthorized)

	var restErr common.RestError
	errors.As(err, &restErr)
	require.Equal(t, invalidManagerPINMessage, restErr.Message)
}

func TestService_VerifyManagerPIN_WrongPin_ReturnsSameGenericUnauthorized(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	staff := &Staff{ID: 9, BranchID: 3, RoleID: 3, PinHash: hashPassword(t, "654321")}
	repo.EXPECT().GetStaff(mock.Anything, uint(7), uint(9)).Return(staff, nil).Once()
	repo.EXPECT().UpdateStaffPinAttempts(mock.Anything, uint(9), 1, (*time.Time)(nil)).Return(nil).Once()

	_, err := svc.VerifyManagerPIN(context.Background(), 7, nil, 9, VerifyManagerPINRequest{Pin: "000000", Permission: "approve_void"})
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusUnauthorized)

	var restErr common.RestError
	errors.As(err, &restErr)
	require.Equal(t, invalidManagerPINMessage, restErr.Message)
}

func TestService_VerifyManagerPIN_StaffNotFound_ReturnsSameGenericUnauthorized(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	repo.EXPECT().GetStaff(mock.Anything, uint(7), uint(999)).
		Return(nil, common.NotFoundError("staff not found")).Once()

	_, err := svc.VerifyManagerPIN(context.Background(), 7, nil, 999, VerifyManagerPINRequest{Pin: "654321", Permission: "approve_void"})
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusUnauthorized)

	var restErr common.RestError
	errors.As(err, &restErr)
	require.Equal(t, invalidManagerPINMessage, restErr.Message,
		"must match the wrong-PIN/wrong-branch/wrong-permission message exactly - no staff-ID enumeration")
}

func TestService_VerifyManagerPIN_UnexpectedRepositoryError_PropagatesAsIs(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	dbErr := errors.New("connection refused")
	repo.EXPECT().GetStaff(mock.Anything, uint(7), uint(9)).Return(nil, dbErr).Once()

	_, err := svc.VerifyManagerPIN(context.Background(), 7, nil, 9, VerifyManagerPINRequest{Pin: "654321", Permission: "approve_void"})
	require.ErrorIs(t, err, dbErr)
}

// Device endpoints are pure passthroughs in the service - org-scoping (via
// branch) lives entirely in the repository, which we don't unit test. These
// just guard the delegation.

func TestService_CreateDevice_DelegatesToRepository(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	in := CreateDeviceRequest{BranchID: 1, Name: "Front Counter iPad", Status: DeviceStatusActive}
	want := &Device{ID: 1, BranchID: 1}
	repo.EXPECT().CreateDevice(mock.Anything, uint(7), in).Return(want, nil).Once()

	got, err := svc.CreateDevice(context.Background(), 7, in)
	require.NoError(t, err)
	require.Same(t, want, got)
}

func TestService_CreateDevice_PropagatesRepositoryError(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	in := CreateDeviceRequest{BranchID: 999, Name: "Sneaky Device", Status: DeviceStatusActive}
	wantErr := common.NotFoundError("branch not found")
	repo.EXPECT().CreateDevice(mock.Anything, uint(7), in).Return(nil, wantErr).Once()

	_, err := svc.CreateDevice(context.Background(), 7, in)
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusNotFound)
}

func TestService_ListDevices_DelegatesToRepository(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	want := []Device{{ID: 1, BranchID: 1}, {ID: 2, BranchID: 2}}
	repo.EXPECT().ListDevices(mock.Anything, uint(7)).Return(want, nil).Once()

	got, err := svc.ListDevices(context.Background(), 7)
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func TestService_ListDevices_PropagatesRepositoryError(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	wantErr := common.SystemError("db read failed")
	repo.EXPECT().ListDevices(mock.Anything, uint(7)).Return(nil, wantErr).Once()

	_, err := svc.ListDevices(context.Background(), 7)
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusInternalServerError)
}

func TestService_UpdateDevice_DelegatesToRepository(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	name := "Renamed Device"
	in := UpdateDeviceRequest{Name: &name}
	want := &Device{ID: 1, BranchID: 1, Name: name}
	repo.EXPECT().UpdateDevice(mock.Anything, uint(7), uint(1), in).Return(want, nil).Once()

	got, err := svc.UpdateDevice(context.Background(), 7, 1, in)
	require.NoError(t, err)
	require.Same(t, want, got)
}

func TestService_UpdateDevice_PropagatesNotFound(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	in := UpdateDeviceRequest{}
	wantErr := common.NotFoundError("device not found")
	repo.EXPECT().UpdateDevice(mock.Anything, uint(7), uint(999), in).Return(nil, wantErr).Once()

	_, err := svc.UpdateDevice(context.Background(), 7, 999, in)
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusNotFound)
}

// Roles/Permissions endpoints are pure passthroughs in the service - they're
// global, fixed-for-v1 data (see the 09-12 permissions-model decision), so
// there's no org-scoping logic anywhere to test, just the delegation.

func TestService_ListRoles_DelegatesToRepository(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	want := []Role{{ID: 1, Code: "owner", Name: "Owner"}, {ID: 2, Code: "cashier", Name: "Cashier"}}
	repo.EXPECT().ListRoles(mock.Anything).Return(want, nil).Once()

	got, err := svc.ListRoles(context.Background())
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func TestService_ListRoles_PropagatesRepositoryError(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	wantErr := common.SystemError("db read failed")
	repo.EXPECT().ListRoles(mock.Anything).Return(nil, wantErr).Once()

	_, err := svc.ListRoles(context.Background())
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusInternalServerError)
}

func TestService_ListPermissions_DelegatesToRepository(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	want := []Permission{{ID: 1, Code: "products:create", Category: "catalog"}}
	repo.EXPECT().ListPermissions(mock.Anything).Return(want, nil).Once()

	got, err := svc.ListPermissions(context.Background())
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func TestService_ListPermissions_PropagatesRepositoryError(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	wantErr := common.SystemError("db read failed")
	repo.EXPECT().ListPermissions(mock.Anything).Return(nil, wantErr).Once()

	_, err := svc.ListPermissions(context.Background())
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusInternalServerError)
}

func TestService_ListRolePermissions_DelegatesToRepository(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	want := []Permission{{ID: 1, Code: "products:create"}, {ID: 2, Code: "products:delete"}}
	repo.EXPECT().ListRolePermissions(mock.Anything, uint(1)).Return(want, nil).Once()

	got, err := svc.ListRolePermissions(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func TestService_ListRolePermissions_PropagatesRepositoryError(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	wantErr := common.SystemError("db read failed")
	repo.EXPECT().ListRolePermissions(mock.Anything, uint(1)).Return(nil, wantErr).Once()

	_, err := svc.ListRolePermissions(context.Background(), 1)
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusInternalServerError)
}
