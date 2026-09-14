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
	jwt.RegisteredClaims
}

// GenerateAccessToken signs a short-lived JWT identifying userID/orgID
// (and branchID, when the caller has one), valid for ttl.
func GenerateAccessToken(secret []byte, userID, orgID uint, branchID *uint, ttl time.Duration) (string, error) {
	now := time.Now()
	claims := Claims{
		UserID:   userID,
		OrgID:    orgID,
		BranchID: branchID,
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
// by identity.Service.VerifyStaffPIN once a cashier signs in at a terminal,
// and identifies *who is currently operating the terminal* (for permission
// checks tied to RoleID), which the device's own access token has no notion
// of.
type StaffClaims struct {
	StaffID  uint `json:"staff_id"`
	BranchID uint `json:"branch_id"`
	RoleID   uint `json:"role_id"`
	jwt.RegisteredClaims
}

// GenerateStaffToken signs a staff-PIN session JWT identifying staffID's
// branch/role, valid for ttl.
func GenerateStaffToken(secret []byte, staffID, branchID, roleID uint, ttl time.Duration) (string, error) {
	now := time.Now()
	claims := StaffClaims{
		StaffID:  staffID,
		BranchID: branchID,
		RoleID:   roleID,
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
