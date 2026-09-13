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

// Claims is the payload of an access token.
type Claims struct {
	UserID uint `json:"user_id"`
	OrgID  uint `json:"org_id"`
	jwt.RegisteredClaims
}

// GenerateAccessToken signs a short-lived JWT identifying userID/orgID, valid for ttl.
func GenerateAccessToken(secret []byte, userID, orgID uint, ttl time.Duration) (string, error) {
	now := time.Now()
	claims := Claims{
		UserID: userID,
		OrgID:  orgID,
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
