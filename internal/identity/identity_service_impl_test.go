package identity

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"shagan_pos/internal/authtoken"
	"shagan_pos/internal/common"
)

// testJWTSecret is a fixed secret for tests only - never read from the environment.
var testJWTSecret = []byte("test-secret-do-not-use-in-production")

func newTestService(repo Repository) *Service {
	return NewService(repo, testJWTSecret, DefaultAccessTokenTTL, DefaultRefreshTokenTTL)
}

func TestService_CreateAccount_HashesPasswordBeforePersisting(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	const plaintext = "correct horse battery staple"
	in := CreateAccountInput{
		OrganizationName: "Acme Retail",
		OwnerEmail:       "owner@acme.test",
		OwnerPassword:    plaintext,
		BranchName:       "Main Branch",
	}

	expected := &CreateAccountResult{Organization: Organization{ID: 1}}

	repo.EXPECT().
		CreateAccount(mock.Anything, mock.MatchedBy(func(got CreateAccountInput) bool {
			if got.OwnerPassword == plaintext {
				return false // must not be the plaintext
			}
			if got.OrganizationName != in.OrganizationName || got.OwnerEmail != in.OwnerEmail || got.BranchName != in.BranchName {
				return false // everything else must pass through unchanged
			}
			return bcrypt.CompareHashAndPassword([]byte(got.OwnerPassword), []byte(plaintext)) == nil
		})).
		Return(expected, nil).
		Once()

	result, err := svc.CreateAccount(context.Background(), in)
	require.NoError(t, err)
	require.Same(t, expected, result)
}

func TestService_CreateAccount_PropagatesRepositoryError(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	wantErr := common.ConflictError("an account with this email already exists")
	repo.EXPECT().CreateAccount(mock.Anything, mock.Anything).Return(nil, wantErr).Once()

	_, err := svc.CreateAccount(context.Background(), CreateAccountInput{
		OrganizationName: "Acme Retail",
		OwnerEmail:       "owner@acme.test",
		OwnerPassword:    "whatever-password",
		BranchName:       "Main Branch",
	})

	require.Error(t, err)

	var gotErr common.RestError
	require.True(t, errors.As(err, &gotErr), "expected a common.RestError, got %T", err)
	require.Equal(t, wantErr.Status, gotErr.Status)
	require.Equal(t, wantErr.Message, gotErr.Message)
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
	user := &User{ID: 42, OrgID: 7}

	repo.EXPECT().GetSessionByRefreshHash(mock.Anything, oldHash).Return(session, nil).Once()
	repo.EXPECT().GetUserByID(mock.Anything, user.ID).Return(user, nil).Once()

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

func TestService_RefreshSession_RotationFails_PropagatesError(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	session := &Session{ID: 1, UserID: 42, ExpiresAt: time.Now().Add(time.Hour)}
	user := &User{ID: 42, OrgID: 7}
	repo.EXPECT().GetSessionByRefreshHash(mock.Anything, mock.Anything).Return(session, nil).Once()
	repo.EXPECT().GetUserByID(mock.Anything, session.UserID).Return(user, nil).Once()

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
