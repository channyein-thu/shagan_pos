package identity

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"shagan_pos/internal/audit"
	"shagan_pos/internal/authtoken"
	"shagan_pos/internal/common"
	"shagan_pos/internal/subscription"
)

const (
	// DefaultAccessTokenTTL is how long an issued access token stays valid.
	DefaultAccessTokenTTL = 60 * time.Minute
	// DefaultRefreshTokenTTL is how long a session's refresh token stays valid.
	DefaultRefreshTokenTTL = 30 * 24 * time.Hour
	// DefaultStaffPINTokenTTL is how long a staff-PIN session token stays
	// valid - shift-length, not request-length, since re-entering a PIN
	// before every single sale would be unusable at a terminal.
	DefaultStaffPINTokenTTL = 12 * time.Hour
	// DefaultManagerPINTokenTTL is how long a manager-PIN elevation token
	// stays valid - a momentary stamp of approval for one specific action
	// (void/return/manual discount), not a session, so this is deliberately
	// much shorter than DefaultStaffPINTokenTTL.
	DefaultManagerPINTokenTTL = 2 * time.Minute
	// DefaultPinLockoutThreshold is how many consecutive wrong PIN guesses
	// (via VerifyStaffPIN or VerifyManagerPIN - they share one counter,
	// see Staff.FailedPinAttempts) lock a staff's PIN out.
	DefaultPinLockoutThreshold = 5
	// DefaultPinLockoutDuration is how long a lockout lasts before it clears
	// on its own - no explicit unlock endpoint exists yet.
	DefaultPinLockoutDuration = 15 * time.Minute
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

// invalidManagerPINMessage additionally folds in "role doesn't grant this
// permission" - same reasoning as invalidStaffPINMessage, one level up.
const invalidManagerPINMessage = "invalid staff, pin, or permission"

type Service struct {
	repo               Repository
	audit              AuditWriter
	subs               Subscriptions
	db                 common.Transactioner
	jwtSecret          []byte
	accessTokenTTL     time.Duration
	refreshTokenTTL    time.Duration
	staffPINTokenTTL   time.Duration
	managerPINTokenTTL time.Duration
}

func NewService(repo Repository, auditWriter AuditWriter, subs Subscriptions, db common.Transactioner, jwtSecret []byte, accessTokenTTL, refreshTokenTTL, staffPINTokenTTL, managerPINTokenTTL time.Duration) *Service {
	return &Service{
		repo:               repo,
		audit:              auditWriter,
		subs:               subs,
		db:                 db,
		jwtSecret:          jwtSecret,
		accessTokenTTL:     accessTokenTTL,
		refreshTokenTTL:    refreshTokenTTL,
		staffPINTokenTTL:   staffPINTokenTTL,
		managerPINTokenTTL: managerPINTokenTTL,
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

	// Folded into the same generic message as a wrong password - a
	// suspended account (see UpdatePosAccountStatus) must be
	// indistinguishable from a wrong password to anyone probing it.
	if user.Status == UserStatusSuspended {
		return nil, common.UnauthorizedError(invalidCredentialsMessage)
	}

	// A whole-tenant suspension (see UpdateOrganizationStatus) must block
	// every one of that org's logins, not just individually-suspended pos
	// accounts - same generic message, same reasoning.
	org, err := s.repo.GetOrganization(ctx, user.OrgID)
	if err != nil {
		var restErr common.RestError
		if errors.As(err, &restErr) && restErr.Status == http.StatusNotFound {
			return nil, common.UnauthorizedError(invalidCredentialsMessage)
		}
		return nil, err
	}
	if org.Status == OrganizationStatusSuspended {
		return nil, common.UnauthorizedError(invalidCredentialsMessage)
	}

	accessExpiresAt := time.Now().Add(s.accessTokenTTL)
	accessToken, err := authtoken.GenerateAccessToken(s.jwtSecret, user.ID, user.OrgID, string(user.AccountType), user.BranchID, s.accessTokenTTL)
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
		AccessToken:     accessToken,
		RefreshToken:    refreshPlaintext,
		ExpiresAt:       expiresAt,
		AccessExpiresAt: accessExpiresAt,
		AccountType:     user.AccountType,
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

	// A refresh token issued before the account was suspended must stop
	// working immediately, not just future logins - same generic message,
	// same reasoning as Login's own suspension check.
	if user.Status == UserStatusSuspended {
		return nil, common.UnauthorizedError(invalidRefreshTokenMessage)
	}

	// Same for a whole-tenant suspension - see Login's identical check.
	org, err := s.repo.GetOrganization(ctx, user.OrgID)
	if err != nil {
		var restErr common.RestError
		if errors.As(err, &restErr) && restErr.Status == http.StatusNotFound {
			return nil, common.UnauthorizedError(invalidRefreshTokenMessage)
		}
		return nil, err
	}
	if org.Status == OrganizationStatusSuspended {
		return nil, common.UnauthorizedError(invalidRefreshTokenMessage)
	}

	accessExpiresAt := time.Now().Add(s.accessTokenTTL)
	accessToken, err := authtoken.GenerateAccessToken(s.jwtSecret, user.ID, user.OrgID, string(user.AccountType), user.BranchID, s.accessTokenTTL)
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
		AccessToken:     accessToken,
		RefreshToken:    refreshPlaintext,
		ExpiresAt:       expiresAt,
		AccessExpiresAt: accessExpiresAt,
		AccountType:     user.AccountType,
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

func (s *Service) GetMe(ctx context.Context, userID uint) (*MeResult, error) {
	user, err := s.repo.GetUserByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	sub, err := s.subs.Summary(ctx, user.OrgID, time.Now())
	if err != nil {
		return nil, err
	}
	return &MeResult{User: *user, Subscription: sub}, nil
}

func (s *Service) UpdateMe(ctx context.Context, userID uint, in UpdateMeRequest) (*User, error) {
	return s.repo.UpdateMe(ctx, userID, in)
}

// VerifyManagerPIN checks that the target staff (1) belongs to the caller's
// org/branch, (2) isn't currently PIN-locked-out, (3) has the right PIN, and
// (4) actually holds in.Permission via their role - branch/PIN/permission
// failures all fold into the same generic unauthorized response as
// VerifyStaffPIN, one level up: this also hides "right staff, right PIN,
// wrong permission" so a terminal can't probe which staff have which
// permissions. Lockout gets its own distinct response - see checkPinLockout.
// On success, issues a short elevation token (DefaultManagerPINTokenTTL) -
// not a shift session.
func (s *Service) VerifyManagerPIN(ctx context.Context, orgID uint, branchID *uint, id uint, in VerifyManagerPINRequest) (*VerifyManagerPINResult, error) {
	staff, err := s.authenticateStaffPIN(ctx, orgID, branchID, id, in.Pin, invalidManagerPINMessage)
	if err != nil {
		return nil, err
	}

	permissions, err := s.repo.ListRolePermissions(ctx, orgID, staff.RoleID)
	if err != nil {
		return nil, err
	}
	codes := permissionCodes(permissions)
	if !slices.Contains(codes, in.Permission) {
		return nil, common.UnauthorizedError(invalidManagerPINMessage)
	}

	token, err := authtoken.GenerateStaffToken(s.jwtSecret, staff.ID, orgID, staff.BranchID, staff.RoleID, codes, s.managerPINTokenTTL)
	if err != nil {
		return nil, common.SystemError("failed to issue manager token")
	}

	return &VerifyManagerPINResult{
		Staff:     *staff,
		Token:     token,
		ExpiresAt: time.Now().Add(s.managerPINTokenTTL),
	}, nil
}

// VerifyStaffPIN looks up the staff via the same org-scoped query GetStaff
// uses (no dedicated repository method needed), then checks branch, lockout,
// and PIN itself - same layering as Login, which fetches the user then does
// its own bcrypt compare rather than pushing that into the repository.
func (s *Service) VerifyStaffPIN(ctx context.Context, orgID uint, branchID *uint, id uint, in VerifyStaffPINRequest) (*VerifyStaffPINResult, error) {
	staff, err := s.authenticateStaffPIN(ctx, orgID, branchID, id, in.Pin, invalidStaffPINMessage)
	if err != nil {
		return nil, err
	}

	permissions, err := s.repo.ListRolePermissions(ctx, orgID, staff.RoleID)
	if err != nil {
		return nil, err
	}

	token, err := authtoken.GenerateStaffToken(s.jwtSecret, staff.ID, orgID, staff.BranchID, staff.RoleID, permissionCodes(permissions), s.staffPINTokenTTL)
	if err != nil {
		return nil, common.SystemError("failed to issue staff token")
	}

	return &VerifyStaffPINResult{
		Staff:     *staff,
		Token:     token,
		ExpiresAt: time.Now().Add(s.staffPINTokenTTL),
	}, nil
}

// authenticateStaffPIN is the credential check every PIN endpoint shares: the
// staff must exist in orgID and (when branchID is set) belong to that branch,
// must not be locked out, and must have entered the right PIN. Unknown staff,
// wrong branch and wrong PIN are all the same generic 401 carrying
// genericMessage, so a terminal can't enumerate staff ids by probing;
// lockout is the one distinct answer (see checkPinLockout). A wrong PIN is
// recorded toward the shared lockout counter, a right one clears it.
func (s *Service) authenticateStaffPIN(ctx context.Context, orgID uint, branchID *uint, id uint, pin string, genericMessage string) (*Staff, error) {
	staff, err := s.repo.GetStaff(ctx, orgID, id)
	if err != nil {
		var restErr common.RestError
		if errors.As(err, &restErr) && restErr.Status == http.StatusNotFound {
			return nil, common.UnauthorizedError(genericMessage)
		}
		return nil, err
	}

	if branchID != nil && staff.BranchID != *branchID {
		return nil, common.UnauthorizedError(genericMessage)
	}

	if err := checkPinLockout(staff); err != nil {
		return nil, err
	}

	if bcrypt.CompareHashAndPassword([]byte(staff.PinHash), []byte(pin)) != nil {
		if err := s.recordFailedPinAttempt(ctx, staff); err != nil {
			return nil, err
		}
		return nil, common.UnauthorizedError(genericMessage)
	}
	if err := s.resetPinAttempts(ctx, staff); err != nil {
		return nil, err
	}
	return staff, nil
}

func (s *Service) VerifyApprover(ctx context.Context, orgID uint, branchID uint, staffID uint, pin string) (*Staff, []string, error) {
	staff, err := s.authenticateStaffPIN(ctx, orgID, &branchID, staffID, pin, invalidStaffPINMessage)
	if err != nil {
		return nil, nil, err
	}
	permissions, err := s.repo.ListRolePermissions(ctx, orgID, staff.RoleID)
	if err != nil {
		return nil, nil, err
	}
	return staff, permissionCodes(permissions), nil
}

// checkPinLockout returns common.TooManyRequestsError if staff is currently
// locked out from PIN attempts - deliberately distinct from
// invalidStaffPINMessage/invalidManagerPINMessage's generic response, since a
// legitimate staff member needs to know *why* their correct PIN is being
// rejected (same reasoning an ATM tells you "too many attempts" instead of
// silently failing).
func checkPinLockout(staff *Staff) error {
	if staff.PinLockedUntil != nil && time.Now().Before(*staff.PinLockedUntil) {
		return common.TooManyRequestsError(fmt.Sprintf(
			"too many failed PIN attempts - locked until %s",
			staff.PinLockedUntil.Format(time.RFC3339),
		))
	}
	return nil
}

// recordFailedPinAttempt increments staff's failed-PIN counter and locks it
// out for DefaultPinLockoutDuration once DefaultPinLockoutThreshold is
// reached. Shared by VerifyStaffPIN/VerifyManagerPIN since both check the
// same PinHash - a brute-force run against either endpoint counts toward the
// same lockout.
func (s *Service) recordFailedPinAttempt(ctx context.Context, staff *Staff) error {
	attempts := staff.FailedPinAttempts + 1
	var lockedUntil *time.Time
	if attempts >= DefaultPinLockoutThreshold {
		t := time.Now().Add(DefaultPinLockoutDuration)
		lockedUntil = &t
	}
	return s.repo.UpdateStaffPinAttempts(ctx, staff.ID, attempts, lockedUntil)
}

// resetPinAttempts clears staff's failed-PIN counter after a successful PIN
// check - skips the write entirely if there's nothing to clear.
func (s *Service) resetPinAttempts(ctx context.Context, staff *Staff) error {
	if staff.FailedPinAttempts == 0 && staff.PinLockedUntil == nil {
		return nil
	}
	return s.repo.UpdateStaffPinAttempts(ctx, staff.ID, 0, nil)
}

// permissionCodes extracts just the Code field from each Permission, for
// embedding into a StaffClaims token - see VerifyStaffPIN/VerifyManagerPIN.
func permissionCodes(permissions []Permission) []string {
	codes := make([]string, len(permissions))
	for i, p := range permissions {
		codes[i] = p.Code
	}
	return codes
}

func (s *Service) CreateDevice(ctx context.Context, orgID uint, in CreateDeviceRequest) (*Device, error) {
	var device *Device
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if in.Status == DeviceStatusActive {
			// the branch must be the org's before the limit is looked at, so a
			// foreign branch id is a 404 whatever the org's plan says
			if _, err := s.repo.GetBranch(ctx, orgID, in.BranchID); err != nil {
				return err
			}
			if err := s.subs.EnforceLimit(tx, orgID, subscription.LimitDevices, time.Now()); err != nil {
				return err
			}
		}
		var err error
		device, err = s.repo.CreateDevice(tx, orgID, in)
		return err
	})
	if err != nil {
		return nil, err
	}
	return device, nil
}

func (s *Service) ListDevices(ctx context.Context, orgID uint) ([]Device, error) {
	return s.repo.ListDevices(ctx, orgID)
}

func (s *Service) UpdateDevice(ctx context.Context, orgID uint, id uint, in UpdateDeviceRequest) (*Device, error) {
	var device *Device
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if in.Status != nil && *in.Status == DeviceStatusActive {
			// only a REactivation counts as adding one: a device that is already
			// active (a rename, a branch move) never trips the limit
			current, err := s.repo.GetDevice(ctx, orgID, id)
			if err != nil {
				return err
			}
			if current.Status != DeviceStatusActive {
				if err := s.subs.EnforceLimit(tx, orgID, subscription.LimitDevices, time.Now()); err != nil {
					return err
				}
			}
		}
		var err error
		device, err = s.repo.UpdateDevice(tx, orgID, id, in)
		return err
	})
	if err != nil {
		return nil, err
	}
	return device, nil
}

func (s *Service) ListBranches(ctx context.Context, orgID uint) ([]Branch, error) {
	return s.repo.ListBranches(ctx, orgID)
}

func (s *Service) CreateBranch(ctx context.Context, orgID uint, in CreateBranchRequest) (*Branch, error) {
	var branch *Branch
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if in.Status == BranchStatusActive {
			if err := s.subs.EnforceLimit(tx, orgID, subscription.LimitBranches, time.Now()); err != nil {
				return err
			}
		}
		var err error
		branch, err = s.repo.CreateBranch(tx, orgID, in)
		return err
	})
	if err != nil {
		return nil, err
	}
	return branch, nil
}

func (s *Service) GetBranch(ctx context.Context, orgID uint, id uint) (*Branch, error) {
	return s.repo.GetBranch(ctx, orgID, id)
}

func (s *Service) UpdateBranch(ctx context.Context, orgID uint, id uint, in UpdateBranchRequest) (*Branch, error) {
	var branch *Branch
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if in.Status != nil && *in.Status == BranchStatusActive {
			// only a REactivation counts as adding one
			current, err := s.repo.GetBranch(ctx, orgID, id)
			if err != nil {
				return err
			}
			if current.Status != BranchStatusActive {
				if err := s.subs.EnforceLimit(tx, orgID, subscription.LimitBranches, time.Now()); err != nil {
					return err
				}
			}
		}
		var err error
		branch, err = s.repo.UpdateBranch(tx, orgID, id, in)
		return err
	})
	if err != nil {
		return nil, err
	}
	return branch, nil
}

func (s *Service) ListBranchStaff(ctx context.Context, orgID uint, id uint) ([]Staff, error) {
	return s.repo.ListBranchStaff(ctx, orgID, id)
}

func (s *Service) ListBranchManagers(ctx context.Context, orgID uint, id uint, permissionCode string) ([]Staff, error) {
	return s.repo.ListBranchManagers(ctx, orgID, id, permissionCode)
}

func (s *Service) ListStaff(ctx context.Context, orgID uint, branchID *uint) ([]Staff, error) {
	return s.repo.ListStaff(ctx, orgID, branchID)
}

// CreateStaff hashes the plaintext PIN before it ever reaches the repository
// - same reasoning as CreateAccount's password hashing.
func (s *Service) CreateStaff(ctx context.Context, orgID uint, in CreateStaffRequest) (*Staff, error) {
	// roles are per org: the role must be one of this org's own, or a caller
	// could attach staff to another tenant's role by guessing its id
	if _, err := s.repo.GetRole(ctx, orgID, in.Role); err != nil {
		return nil, err
	}
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

// UpdateStaff hashes the new PIN, if one was provided, before it reaches the
// repository, then writes an audit entry capturing the staff record before
// and after the change - see the Interface doc.
func (s *Service) UpdateStaff(ctx context.Context, orgID uint, actorUserID uint, id uint, in UpdateStaffRequest) (*Staff, error) {
	before, err := s.repo.GetStaff(ctx, orgID, id)
	if err != nil {
		return nil, err
	}
	if in.Role != nil {
		if _, err := s.repo.GetRole(ctx, orgID, *in.Role); err != nil {
			return nil, err
		}
	}

	if in.Pin != nil {
		hash, err := bcrypt.GenerateFromPassword([]byte(*in.Pin), bcrypt.DefaultCost)
		if err != nil {
			return nil, common.SystemError("failed to hash PIN")
		}
		hashed := string(hash)
		in.Pin = &hashed
	}

	after, err := s.repo.UpdateStaff(ctx, orgID, id, in)
	if err != nil {
		return nil, err
	}

	auditErr := s.db.Transaction(func(tx *gorm.DB) error {
		return s.audit.CreateAuditLog(tx, &audit.AuditLog{
			OrgID: orgID, ActorID: &actorUserID, BranchID: &after.BranchID,
			Entity: "staff", EntityID: strconv.FormatUint(uint64(id), 10), Action: "updated",
			Before: audit.ToJSON(before), After: audit.ToJSON(after),
		})
	})
	if auditErr != nil {
		return nil, auditErr
	}
	return after, nil
}

// CreateAccount hashes both the owner's and service_center's plaintext
// passwords before they ever reach the repository - the repository just
// persists whatever CredentialHash it's given, it doesn't know or care
// whether it's already hashed. It then creates the Organization and both
// Users as one atomic unit of work: if either User insert fails (e.g. a
// duplicate email), s.db.Transaction rolls back the Organization insert too,
// so a failure partway through never leaves an orphaned org with no owner.
func (s *Service) CreateAccount(ctx context.Context, in CreateAccountInput) (*CreateAccountResult, error) {
	timezone, err := validateTimezone(in.Timezone)
	if err != nil {
		return nil, err
	}
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

	changedBy := strings.TrimSpace(in.ChangedBy)
	if changedBy == "" {
		changedBy = "system"
	}
	var result CreateAccountResult
	err = s.db.Transaction(func(tx *gorm.DB) error {
		// every new org starts on the 14-day Pro trial, then drops to Basic
		trial := subscription.NewTrial(time.Now())
		org := Organization{
			Name: in.OrganizationName, Timezone: timezone,
			Plan: trial.Plan, PlanExpiresAt: trial.ExpiresAt, PlanTrial: trial.Trial,
		}
		if err := s.repo.CreateOrganization(tx, &org); err != nil {
			return err
		}
		if err := s.subs.RecordChange(tx, &subscription.OrgPlanChange{
			OrgID: org.ID, FromPlan: "", ToPlan: trial.Plan, ExpiresAt: trial.ExpiresAt, Trial: true,
			Note: "new organization: 14-day Pro trial", ChangedBy: changedBy,
		}); err != nil {
			return err
		}
		if err := s.audit.CreateAuditLog(tx, &audit.AuditLog{
			OrgID: org.ID, Entity: "organization_subscription", EntityID: strconv.FormatUint(uint64(org.ID), 10), Action: "subscription_trial_started",
			Before: audit.ToJSON(nil),
			After:  audit.ToJSON(map[string]any{"plan": trial.Plan, "expires_at": trial.ExpiresAt, "trial": true, "changed_by": changedBy}),
		}); err != nil {
			return err
		}
		// every org starts with a Manager and a Cashier role, so staff can be
		// given a PIN-able role from day one - in this transaction, so a
		// failure leaves no org without roles behind
		if err := s.repo.ProvisionDefaultRoles(tx, org.ID); err != nil {
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

// summarizeOrganization adds what the stored plan means today: the effective plan and
// whether the org is over its limits.
func summarizeOrganization(org Organization, usage subscription.Usage, now time.Time) OrganizationSummary {
	ent := subscription.Resolve(subscription.OrgPlan{
		Plan: org.Plan, ExpiresAt: org.PlanExpiresAt, Trial: org.PlanTrial,
		ExtraBranches: org.ExtraBranches, ExtraDevices: org.ExtraDevices,
	}, nil, now)
	return OrganizationSummary{Organization: org, EffectivePlan: ent.Plan, OverLimit: ent.Limits.Exceeded(usage)}
}

func (s *Service) ListOrganizations(ctx context.Context) ([]OrganizationSummary, error) {
	orgs, err := s.repo.ListOrganizations(ctx)
	if err != nil {
		return nil, err
	}
	usage, err := s.subs.UsageByOrg(ctx)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	out := make([]OrganizationSummary, len(orgs))
	for i, org := range orgs {
		out[i] = summarizeOrganization(org, usage[org.ID], now)
	}
	return out, nil
}

func (s *Service) ListPosAccounts(ctx context.Context, orgID uint) ([]User, error) {
	return s.repo.ListPosAccounts(ctx, orgID)
}

func (s *Service) UpdateOrganizationStatus(ctx context.Context, id uint, status OrganizationStatus) error {
	return s.repo.UpdateOrganizationStatus(ctx, id, status)
}

// validateTimezone returns the zone name to store: DefaultTimezone for "",
// otherwise name itself if it's a real IANA zone, 400 if not.
func validateTimezone(name string) (string, error) {
	if name == "" {
		return common.DefaultTimezone, nil
	}
	if _, err := common.LoadTimezone(name); err != nil {
		return "", common.BadRequestError("timezone must be an IANA zone name such as Asia/Yangon")
	}
	return name, nil
}

func (s *Service) UpdateOrganizationTimezone(ctx context.Context, id uint, timezone string) error {
	name, err := validateTimezone(timezone)
	if err != nil {
		return err
	}
	if _, err := s.repo.GetOrganization(ctx, id); err != nil {
		return err
	}
	return s.repo.UpdateOrganizationTimezone(ctx, id, name)
}

func (s *Service) GetOrganization(ctx context.Context, id uint) (*OrganizationSummary, error) {
	org, err := s.repo.GetOrganization(ctx, id)
	if err != nil {
		return nil, err
	}
	usage, err := s.subs.UsageByOrg(ctx)
	if err != nil {
		return nil, err
	}
	summary := summarizeOrganization(*org, usage[org.ID], time.Now())
	return &summary, nil
}

func (s *Service) ListOrgWideAccounts(ctx context.Context, orgID uint) ([]User, error) {
	return s.repo.ListOrgWideAccounts(ctx, orgID)
}

// ResetOrgWideAccountPassword hashes the plaintext password before it ever
// reaches the repository - same pattern as ResetPosAccountPassword.
func (s *Service) ResetOrgWideAccountPassword(ctx context.Context, id uint, plaintext string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(plaintext), bcrypt.DefaultCost)
	if err != nil {
		return common.SystemError("failed to hash password")
	}
	return s.repo.ResetOrgWideAccountPassword(ctx, id, string(hash))
}

func (s *Service) UpdateBranchStatus(ctx context.Context, id uint, status BranchStatus) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		if status == BranchStatusActive {
			// reactivating counts as adding one, for the branch's own org
			branch, err := s.repo.GetBranchAnyOrg(tx, id)
			if err != nil {
				return err
			}
			if branch.Status != BranchStatusActive {
				if err := s.subs.EnforceLimit(tx, branch.OrgID, subscription.LimitBranches, time.Now()); err != nil {
					return err
				}
			}
		}
		return s.repo.UpdateBranchStatusInternal(tx, id, status)
	})
}

func (s *Service) UpdateDeviceStatus(ctx context.Context, id uint, status DeviceStatus) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		if status == DeviceStatusActive {
			device, orgID, err := s.repo.GetDeviceAnyOrg(tx, id)
			if err != nil {
				return err
			}
			if device.Status != DeviceStatusActive {
				if err := s.subs.EnforceLimit(tx, orgID, subscription.LimitDevices, time.Now()); err != nil {
					return err
				}
			}
		}
		return s.repo.UpdateDeviceStatusInternal(tx, id, status)
	})
}

func (s *Service) UpdatePosAccountStatus(ctx context.Context, id uint, status UserStatus) error {
	return s.repo.UpdatePosAccountStatus(ctx, id, status)
}

// ResetPosAccountPassword hashes the plaintext password before it ever
// reaches the repository - same pattern as CreateAccount/CreatePosAccount.
func (s *Service) ResetPosAccountPassword(ctx context.Context, id uint, plaintext string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(plaintext), bcrypt.DefaultCost)
	if err != nil {
		return common.SystemError("failed to hash password")
	}
	return s.repo.ResetPosAccountPassword(ctx, id, string(hash))
}
