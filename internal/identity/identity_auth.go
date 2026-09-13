package identity

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// LoginRequest is the request body for `POST /auth/login`.
type LoginRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

// LoginResult is returned on a successful login. RefreshToken is the
// plaintext token - it is shown to the client this one time only; the
// server stores just its hash (see Session.RefreshHash).
type LoginResult struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
}

// accessTokenClaims is the JWT payload for an access token.
type accessTokenClaims struct {
	UserID uint `json:"user_id"`
	OrgID  uint `json:"org_id"`
	jwt.RegisteredClaims
}

// generateAccessToken signs a short-lived JWT identifying user, valid for ttl.
func generateAccessToken(secret []byte, user User, ttl time.Duration) (string, error) {
	now := time.Now()
	claims := accessTokenClaims{
		UserID: user.ID,
		OrgID:  user.OrgID,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(secret)
}

// generateRefreshToken returns a random plaintext token and its hash. Unlike
// passwords, refresh tokens are already high-entropy random data - a fast
// cryptographic hash (not bcrypt) is the standard choice for these.
func generateRefreshToken() (plaintext string, hash string, err error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", "", err
	}
	plaintext = hex.EncodeToString(buf)
	sum := sha256.Sum256([]byte(plaintext))
	hash = hex.EncodeToString(sum[:])
	return plaintext, hash, nil
}
