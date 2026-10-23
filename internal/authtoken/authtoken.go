// Package authtoken is the single source of truth for the access-token JWT
// format: identity.Service.Login issues these, middleware.Auth verifies them.
// Keeping both sides in one place means they can never drift apart silently.
package authtoken

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Claims is the payload of an access token. BranchID is only set for a
// pos-account token (nil for owner/service_center, which are org-wide) - see
// identity.User.BranchID.
type Claims struct {
	UserID   uint  `json:"user_id"`
	OrgID    uint  `json:"org_id"`
	BranchID *uint `json:"branch_id,omitempty"`
	// AccountType is identity.User.AccountType ("owner", "pos",
	// "service_center"), carried so route-level authorization
	// (middleware.RequireAccountType) needs no per-request DB read. Empty on
	// a token issued before this claim existed - callers that gate on it
	// must treat empty as "re-authenticate", never as "allowed".
	AccountType string `json:"account_type,omitempty"`
	jwt.RegisteredClaims
}

// GenerateAccessToken signs a short-lived JWT identifying userID/orgID
// (and branchID, when the caller has one) and its accountType, valid for ttl.
func GenerateAccessToken(secret []byte, userID, orgID uint, accountType string, branchID *uint, ttl time.Duration) (string, error) {
	now := time.Now()
	claims := Claims{
		UserID:      userID,
		OrgID:       orgID,
		BranchID:    branchID,
		AccountType: accountType,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(secret)
}

// ParseAccessToken verifies tokenString's signature and expiry against secret
// and returns its claims. It rejects anything not signed with HMAC to close
// off the classic "alg confusion" attack (a token claiming alg=none or a
// different algorithm than the server expects).
func ParseAccessToken(secret []byte, tokenString string) (*Claims, error) {
	var claims Claims
	token, err := jwt.ParseWithClaims(tokenString, &claims, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return secret, nil
	})
	if err != nil {
		return nil, err
	}
	if !token.Valid {
		return nil, errors.New("invalid token")
	}
	return &claims, nil
}

// StaffClaims is the payload of a staff-PIN session token - a separate,
// shorter-lived token from the device/owner access token above. It's issued
// by identity.Service.VerifyStaffPIN/VerifyManagerPIN once a cashier or
// manager PINs in, and identifies *who is currently operating* (RoleID) and
// *what they're allowed to do* (Permissions - the target's role's full
// permission-code list at the moment of issuance, so middleware.RequirePermission
// can check it with no DB access, the same way ParseAccessToken needs none).
// A grant/revoke on the Roles/Permissions matrix only takes effect on this
// staff's next PIN verify, not immediately - same staleness tradeoff every
// other TTL'd token in this codebase already accepts.
type StaffClaims struct {
	StaffID     uint     `json:"staff_id"`
	BranchID    uint     `json:"branch_id"`
	RoleID      uint     `json:"role_id"`
	Permissions []string `json:"permissions"`
	// AccessVersion says which permission catalogue Permissions was minted
	// from: absent (0) for a token issued before per-org roles, when only the
	// seven original permissions existed - see EffectivePermissions.
	AccessVersion int `json:"access_version,omitempty"`
	jwt.RegisteredClaims
}

// CurrentAccessVersion is stamped on every staff token issued now: its
// Permissions are the role's full grant list from the per-org catalogue and
// are enforced exactly as written.
const CurrentAccessVersion = 2

// Permissions that did not exist before per-org roles but that the till and
// back office routes now check. A token minted before then (AccessVersion 0)
// has none of them, so EffectivePermissions derives them from the legacy
// permission that used to imply the behaviour - keeping a signed-in till
// working through the deploy until its token expires (staff tokens last 12h).
var (
	// everything a legacy staff token could already do at the till
	legacyTillPermissions = []string{
		"accept_payments", "hold_resume_sales", "manage_own_shift", "log_till_expenses",
		"add_customers", "view_all_receipts", "reprint_receipt",
		// Discount V1: a Cashier may ask for a custom discount (it still needs
		// an approver's PIN) - so a till signed in before the deploy can too.
		"apply_manual_discount",
	}
	// what the old access_backoffice permission (the Manager's) unlocked,
	// including the force-close override it used to gate. manage_staff is
	// absent on purpose: staff management was Owner-only.
	legacyBackOfficePermissions = []string{
		"force_close_shift", "view_reports", "view_item_cost", "view_sales", "view_audit_log",
		"manage_catalog", "view_inventory", "manage_stock", "manage_suppliers", "manage_purchase_orders",
		"receive_stock", "view_customers", "manage_customers", "view_staff", "manage_expenses",
		"manage_receipt_settings", "resolve_sync_conflicts",
	}
)

// EffectivePermissions is the permission list to enforce for these claims. A
// current token is returned as issued. A legacy one (AccessVersion 0) is
// expanded: access_pos_portal implies the till basics every staff token could
// use before (the old `staff` role became `cashier`), and access_backoffice
// implies the Manager's back office reach. Nothing else is added - a legacy
// token never gains an approval, the drawer, or discount rights it lacked.
func (c *StaffClaims) EffectivePermissions() []string {
	if c.AccessVersion >= CurrentAccessVersion {
		return c.Permissions
	}
	out := append([]string(nil), c.Permissions...)
	add := func(codes []string) {
		for _, code := range codes {
			if !contains(out, code) {
				out = append(out, code)
			}
		}
	}
	if contains(c.Permissions, "access_pos_portal") {
		add(legacyTillPermissions)
	}
	if contains(c.Permissions, "access_backoffice") {
		add(legacyBackOfficePermissions)
	}
	return out
}

func contains(list []string, code string) bool {
	for _, c := range list {
		if c == code {
			return true
		}
	}
	return false
}

// GenerateStaffToken signs a staff-PIN session JWT identifying staffID's
// branch/role/permissions, valid for ttl.
func GenerateStaffToken(secret []byte, staffID, branchID, roleID uint, permissions []string, ttl time.Duration) (string, error) {
	now := time.Now()
	claims := StaffClaims{
		StaffID:       staffID,
		BranchID:      branchID,
		RoleID:        roleID,
		Permissions:   permissions,
		AccessVersion: CurrentAccessVersion,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(secret)
}

// ParseStaffToken verifies tokenString's signature and expiry against secret
// and returns its claims - same alg-confusion guard as ParseAccessToken.
func ParseStaffToken(secret []byte, tokenString string) (*StaffClaims, error) {
	var claims StaffClaims
	token, err := jwt.ParseWithClaims(tokenString, &claims, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return secret, nil
	})
	if err != nil {
		return nil, err
	}
	if !token.Valid {
		return nil, errors.New("invalid token")
	}
	return &claims, nil
}
