package identity

import (
	"context"
	"errors"
	"net/http"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"shagan_pos/internal/authtoken"
	"shagan_pos/internal/common"
)

const (
	// DefaultAccessTokenTTL is how long an issued access token stays valid.
	DefaultAccessTokenTTL = 15 * time.Minute
	// DefaultRefreshTokenTTL is how long a session's refresh token stays valid.
	DefaultRefreshTokenTTL = 30 * 24 * time.Hour
	// DefaultStaffPINTokenTTL is how long a staff-PIN session token stays
	// valid - shift-length, not request-length, since re-entering a PIN
	// before every single sale would be unusable at a terminal.
	DefaultStaffPINTokenTTL = 12 * time.Hour
)

// invalidCredentialsMessage is deliberately identical for "no such email" and
// "wrong password" - telling those apart lets an attacker enumerate which
// emails have accounts.
const invalidCredentialsMessage = "invalid email or password"

// invalidStaffPINMessage is deliberately identical for "no such staff",
// "wrong branch", and "wrong PIN" - same enumeration-prevention reasoning as
// invalidCredentialsMessage, since a terminal is a credential-check surface
// like login, not a plain CRUD lookup like GetStaff.
const invalidStaffPINMessage = "invalid staff or pin"

type Service struct {
	repo             Repository
	db               common.Transactioner
	jwtSecret        []byte
	accessTokenTTL   time.Duration
	refreshTokenTTL  time.Duration
	staffPINTokenTTL time.Duration
}

func NewService(repo Repository, db common.Transactioner, jwtSecret []byte, accessTokenTTL, refreshTokenTTL, staffPINTokenTTL time.Duration) *Service {
	return &Service{
		repo:             repo,
		db:               db,
		jwtSecret:        jwtSecret,
		accessTokenTTL:   accessTokenTTL,
		refreshTokenTTL:  refreshTokenTTL,
		staffPINTokenTTL: staffPINTokenTTL,
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

	accessToken, err := authtoken.GenerateAccessToken(s.jwtSecret, user.ID, user.OrgID, user.BranchID, s.accessTokenTTL)
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

	accessToken, err := authtoken.GenerateAccessToken(s.jwtSecret, user.ID, user.OrgID, user.BranchID, s.accessTokenTTL)
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

func (s *Service) GetMe(ctx context.Context, userID uint) (*User, error) {
	return s.repo.GetUserByID(ctx, userID)
}

func (s *Service) UpdateMe(ctx context.Context, userID uint, in UpdateMeRequest) (*User, error) {
	return s.repo.UpdateMe(ctx, userID, in)
}

func (s *Service) VerifyManagerPIN(ctx context.Context) (*Staff, error) {
	return s.repo.VerifyManagerPIN(ctx)
}

// VerifyStaffPIN looks up the staff via the same org-scoped query GetStaff
// uses (no dedicated repository method needed), then checks branch and PIN
// itself - same layering as Login, which fetches the user then does its own
// bcrypt compare rather than pushing that into the repository.
func (s *Service) VerifyStaffPIN(ctx context.Context, orgID uint, branchID *uint, id uint, in VerifyStaffPINRequest) (*VerifyStaffPINResult, error) {
	staff, err := s.repo.GetStaff(ctx, orgID, id)
	if err != nil {
		var restErr common.RestError
		if errors.As(err, &restErr) && restErr.Status == http.StatusNotFound {
			return nil, common.UnauthorizedError(invalidStaffPINMessage)
		}
		return nil, err
	}

	if branchID != nil && staff.BranchID != *branchID {
		return nil, common.UnauthorizedError(invalidStaffPINMessage)
	}

	if bcrypt.CompareHashAndPassword([]byte(staff.PinHash), []byte(in.Pin)) != nil {
		return nil, common.UnauthorizedError(invalidStaffPINMessage)
	}

	token, err := authtoken.GenerateStaffToken(s.jwtSecret, staff.ID, staff.BranchID, staff.RoleID, s.staffPINTokenTTL)
	if err != nil {
		return nil, common.SystemError("failed to issue staff token")
	}

	return &VerifyStaffPINResult{
		Staff:     *staff,
		Token:     token,
		ExpiresAt: time.Now().Add(s.staffPINTokenTTL),
	}, nil
}

func (s *Service) CreateDevice(ctx context.Context, orgID uint, in CreateDeviceRequest) (*Device, error) {
	return s.repo.CreateDevice(ctx, orgID, in)
}

func (s *Service) ListDevices(ctx context.Context, orgID uint) ([]Device, error) {
	return s.repo.ListDevices(ctx, orgID)
}

func (s *Service) UpdateDevice(ctx context.Context, orgID uint, id uint, in UpdateDeviceRequest) (*Device, error) {
	return s.repo.UpdateDevice(ctx, orgID, id, in)
}

func (s *Service) ListBranches(ctx context.Context, orgID uint) ([]Branch, error) {
	return s.repo.ListBranches(ctx, orgID)
}

func (s *Service) CreateBranch(ctx context.Context, orgID uint, in CreateBranchRequest) (*Branch, error) {
	return s.repo.CreateBranch(ctx, orgID, in)
}

func (s *Service) GetBranch(ctx context.Context, orgID uint, id uint) (*Branch, error) {
	return s.repo.GetBranch(ctx, orgID, id)
}

func (s *Service) UpdateBranch(ctx context.Context, orgID uint, id uint, in UpdateBranchRequest) (*Branch, error) {
	return s.repo.UpdateBranch(ctx, orgID, id, in)
}

func (s *Service) ListBranchStaff(ctx context.Context, orgID uint, id uint) ([]Staff, error) {
	return s.repo.ListBranchStaff(ctx, orgID, id)
}

func (s *Service) ListStaff(ctx context.Context, orgID uint) ([]Staff, error) {
	return s.repo.ListStaff(ctx, orgID)
}

// CreateStaff hashes the plaintext PIN before it ever reaches the repository
// - same reasoning as CreateAccount's password hashing.
func (s *Service) CreateStaff(ctx context.Context, orgID uint, in CreateStaffRequest) (*Staff, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(in.Pin), bcrypt.DefaultCost)
	if err != nil {
		return nil, common.SystemError("failed to hash PIN")
	}
	in.Pin = string(hash)

	return s.repo.CreateStaff(ctx, orgID, in)
}

func (s *Service) GetStaff(ctx context.Context, orgID uint, id uint) (*Staff, error) {
	return s.repo.GetStaff(ctx, orgID, id)
}

// UpdateStaff hashes the new PIN, if one was provided, before it reaches the repository.
func (s *Service) UpdateStaff(ctx context.Context, orgID uint, id uint, in UpdateStaffRequest) (*Staff, error) {
	if in.Pin != nil {
		hash, err := bcrypt.GenerateFromPassword([]byte(*in.Pin), bcrypt.DefaultCost)
		if err != nil {
			return nil, common.SystemError("failed to hash PIN")
		}
		hashed := string(hash)
		in.Pin = &hashed
	}

	return s.repo.UpdateStaff(ctx, orgID, id, in)
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

// CreateAccount hashes both the owner's and service_center's plaintext
// passwords before they ever reach the repository - the repository just
// persists whatever CredentialHash it's given, it doesn't know or care
// whether it's already hashed. It then creates the Organization and both
// Users as one atomic unit of work: if either User insert fails (e.g. a
// duplicate email), s.db.Transaction rolls back the Organization insert too,
// so a failure partway through never leaves an orphaned org with no owner.
func (s *Service) CreateAccount(ctx context.Context, in CreateAccountInput) (*CreateAccountResult, error) {
	ownerHash, err := bcrypt.GenerateFromPassword([]byte(in.OwnerPassword), bcrypt.DefaultCost)
	if err != nil {
		return nil, common.SystemError("failed to hash password")
	}
	in.OwnerPassword = string(ownerHash)

	serviceCenterHash, err := bcrypt.GenerateFromPassword([]byte(in.ServiceCenterPassword), bcrypt.DefaultCost)
	if err != nil {
		return nil, common.SystemError("failed to hash password")
	}
	in.ServiceCenterPassword = string(serviceCenterHash)

	var result CreateAccountResult
	err = s.db.Transaction(func(tx *gorm.DB) error {
		org := Organization{Name: in.OrganizationName}
		if err := s.repo.CreateOrganization(tx, &org); err != nil {
			return err
		}

		ownerEmail := in.OwnerEmail
		owner := User{
			OrgID:          org.ID,
			AccountType:    AccountTypeOwner,
			Email:          &ownerEmail,
			CredentialHash: in.OwnerPassword,
		}
		if err := s.repo.CreateUser(tx, &owner); err != nil {
			return err
		}

		serviceCenterEmail := in.ServiceCenterEmail
		serviceCenter := User{
			OrgID:          org.ID,
			AccountType:    AccountTypeServiceCenter,
			Email:          &serviceCenterEmail,
			CredentialHash: in.ServiceCenterPassword,
		}
		if err := s.repo.CreateUser(tx, &serviceCenter); err != nil {
			return err
		}

		result = CreateAccountResult{Organization: org, Owner: owner, ServiceCenter: serviceCenter}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return &result, nil
}

// CreatePosAccount hashes the plaintext password before it ever reaches the
// repository - same reasoning as CreateAccount.
func (s *Service) CreatePosAccount(ctx context.Context, in CreatePosAccountInput) (*User, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, common.SystemError("failed to hash password")
	}
	in.Password = string(hash)

	return s.repo.CreatePosAccount(ctx, in)
}
