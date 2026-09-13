package identity

import (
	"context"
	"errors"
	"net/http"
	"time"

	"golang.org/x/crypto/bcrypt"

	"shagan_pos/internal/authtoken"
	"shagan_pos/internal/common"
)

const (
	// DefaultAccessTokenTTL is how long an issued access token stays valid.
	DefaultAccessTokenTTL = 15 * time.Minute
	// DefaultRefreshTokenTTL is how long a session's refresh token stays valid.
	DefaultRefreshTokenTTL = 30 * 24 * time.Hour
)

// invalidCredentialsMessage is deliberately identical for "no such email" and
// "wrong password" - telling those apart lets an attacker enumerate which
// emails have accounts.
const invalidCredentialsMessage = "invalid email or password"

type Service struct {
	repo            Repository
	jwtSecret       []byte
	accessTokenTTL  time.Duration
	refreshTokenTTL time.Duration
}

func NewService(repo Repository, jwtSecret []byte, accessTokenTTL, refreshTokenTTL time.Duration) *Service {
	return &Service{
		repo:            repo,
		jwtSecret:       jwtSecret,
		accessTokenTTL:  accessTokenTTL,
		refreshTokenTTL: refreshTokenTTL,
	}
}

var _ Interface = (*Service)(nil)

// Login verifies email+password, then issues a new session: a short-lived
// JWT access token plus a long-lived refresh token whose hash (never the
// plaintext) is what gets persisted via Repository.CreateSession.
func (s *Service) Login(ctx context.Context, in LoginRequest) (*SessionResult, error) {
	user, err := s.repo.GetUserByEmail(ctx, in.Email)
	if err != nil {
		var restErr common.RestError
		if errors.As(err, &restErr) && restErr.Status == http.StatusNotFound {
			return nil, common.UnauthorizedError(invalidCredentialsMessage)
		}
		return nil, err
	}

	if bcrypt.CompareHashAndPassword([]byte(user.CredentialHash), []byte(in.Password)) != nil {
		return nil, common.UnauthorizedError(invalidCredentialsMessage)
	}

	accessToken, err := authtoken.GenerateAccessToken(s.jwtSecret, user.ID, user.OrgID, s.accessTokenTTL)
	if err != nil {
		return nil, common.SystemError("failed to issue access token")
	}

	refreshPlaintext, refreshHash, err := generateRefreshToken()
	if err != nil {
		return nil, common.SystemError("failed to issue refresh token")
	}
	expiresAt := time.Now().Add(s.refreshTokenTTL)

	if _, err := s.repo.CreateSession(ctx, user.ID, refreshHash, expiresAt); err != nil {
		return nil, err
	}

	return &SessionResult{
		AccessToken:  accessToken,
		RefreshToken: refreshPlaintext,
		ExpiresAt:    expiresAt,
	}, nil
}

// invalidRefreshTokenMessage is deliberately identical for "unknown token",
// "revoked token", and "expired token" - same enumeration-prevention
// reasoning as invalidCredentialsMessage.
const invalidRefreshTokenMessage = "invalid or expired refresh token"

// RefreshSession verifies a refresh token, then rotates it: a new session
// (new refresh token) is created and the old one is revoked, so a stolen and
// later-reused old token becomes detectable (it'll already be revoked)
// instead of silently still working.
func (s *Service) RefreshSession(ctx context.Context, in RefreshRequest) (*SessionResult, error) {
	session, err := s.repo.GetSessionByRefreshHash(ctx, hashRefreshToken(in.RefreshToken))
	if err != nil {
		var restErr common.RestError
		if errors.As(err, &restErr) && restErr.Status == http.StatusNotFound {
			return nil, common.UnauthorizedError(invalidRefreshTokenMessage)
		}
		return nil, err
	}

	if session.RevokedAt != nil || time.Now().After(session.ExpiresAt) {
		return nil, common.UnauthorizedError(invalidRefreshTokenMessage)
	}

	user, err := s.repo.GetUserByID(ctx, session.UserID)
	if err != nil {
		var restErr common.RestError
		if errors.As(err, &restErr) && restErr.Status == http.StatusNotFound {
			return nil, common.UnauthorizedError(invalidRefreshTokenMessage)
		}
		return nil, err
	}

	accessToken, err := authtoken.GenerateAccessToken(s.jwtSecret, user.ID, user.OrgID, s.accessTokenTTL)
	if err != nil {
		return nil, common.SystemError("failed to issue access token")
	}

	refreshPlaintext, refreshHash, err := generateRefreshToken()
	if err != nil {
		return nil, common.SystemError("failed to issue refresh token")
	}
	expiresAt := time.Now().Add(s.refreshTokenTTL)

	if _, err := s.repo.CreateSession(ctx, user.ID, refreshHash, expiresAt); err != nil {
		return nil, err
	}
	if err := s.repo.RevokeSession(ctx, session.ID); err != nil {
		return nil, err
	}

	return &SessionResult{
		AccessToken:  accessToken,
		RefreshToken: refreshPlaintext,
		ExpiresAt:    expiresAt,
	}, nil
}

// Logout revokes the session identified by the given refresh token.
// Idempotent: a token that's unknown or already revoked is treated as
// "already logged out" rather than an error, since a client retrying a
// logout call shouldn't get a scary failure for something already true.
func (s *Service) Logout(ctx context.Context, in LogoutRequest) error {
	session, err := s.repo.GetSessionByRefreshHash(ctx, hashRefreshToken(in.RefreshToken))
	if err != nil {
		var restErr common.RestError
		if errors.As(err, &restErr) && restErr.Status == http.StatusNotFound {
			return nil
		}
		return err
	}

	if session.RevokedAt != nil {
		return nil
	}

	return s.repo.RevokeSession(ctx, session.ID)
}

func (s *Service) GetMe(ctx context.Context) (*User, error) {
	return s.repo.GetMe(ctx)
}

func (s *Service) UpdateMe(ctx context.Context, in UpdateMeRequest) (*User, error) {
	return s.repo.UpdateMe(ctx, in)
}

func (s *Service) VerifyManagerPIN(ctx context.Context) (*Staff, error) {
	return s.repo.VerifyManagerPIN(ctx)
}

func (s *Service) VerifyStaffPIN(ctx context.Context, id uint) (*Staff, error) {
	return s.repo.VerifyStaffPIN(ctx, id)
}

func (s *Service) RegisterDevice(ctx context.Context, in RegisterDeviceRequest) (*Device, error) {
	return s.repo.RegisterDevice(ctx, in)
}

func (s *Service) ListDevices(ctx context.Context) ([]Device, error) {
	return s.repo.ListDevices(ctx)
}

func (s *Service) UpdateDevice(ctx context.Context, id uint, in UpdateDeviceRequest) (*Device, error) {
	return s.repo.UpdateDevice(ctx, id, in)
}

func (s *Service) ListBranches(ctx context.Context) ([]Branch, error) {
	return s.repo.ListBranches(ctx)
}

func (s *Service) CreateBranch(ctx context.Context, in CreateBranchRequest) (*Branch, error) {
	return s.repo.CreateBranch(ctx, in)
}

func (s *Service) GetBranch(ctx context.Context, id uint) (*Branch, error) {
	return s.repo.GetBranch(ctx, id)
}

func (s *Service) UpdateBranch(ctx context.Context, id uint, in UpdateBranchRequest) (*Branch, error) {
	return s.repo.UpdateBranch(ctx, id, in)
}

func (s *Service) ListBranchStaff(ctx context.Context, id uint) ([]Staff, error) {
	return s.repo.ListBranchStaff(ctx, id)
}

func (s *Service) ListStaff(ctx context.Context) ([]Staff, error) {
	return s.repo.ListStaff(ctx)
}

func (s *Service) CreateStaff(ctx context.Context, in CreateStaffRequest) (*Staff, error) {
	return s.repo.CreateStaff(ctx, in)
}

func (s *Service) GetStaff(ctx context.Context, id uint) (*Staff, error) {
	return s.repo.GetStaff(ctx, id)
}

func (s *Service) UpdateStaff(ctx context.Context, id uint, in UpdateStaffRequest) (*Staff, error) {
	return s.repo.UpdateStaff(ctx, id, in)
}

func (s *Service) ListRoles(ctx context.Context) ([]Role, error) {
	return s.repo.ListRoles(ctx)
}

func (s *Service) ListPermissions(ctx context.Context) ([]Permission, error) {
	return s.repo.ListPermissions(ctx)
}

func (s *Service) ListRolePermissions(ctx context.Context, id uint) ([]Permission, error) {
	return s.repo.ListRolePermissions(ctx, id)
}

// CreateAccount hashes the owner's plaintext password before it ever reaches
// the repository - the repository just persists whatever CredentialHash it's
// given, it doesn't know or care whether it's already hashed.
func (s *Service) CreateAccount(ctx context.Context, in CreateAccountInput) (*CreateAccountResult, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(in.OwnerPassword), bcrypt.DefaultCost)
	if err != nil {
		return nil, common.SystemError("failed to hash password")
	}
	in.OwnerPassword = string(hash)

	return s.repo.CreateAccount(ctx, in)
}
