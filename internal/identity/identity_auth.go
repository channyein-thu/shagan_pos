package identity

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
)

// generateRefreshToken returns a random plaintext token and its hash. Unlike
// passwords, refresh tokens are already high-entropy random data - a fast
// cryptographic hash (not bcrypt) is the standard choice for these.
func generateRefreshToken() (plaintext string, hash string, err error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", "", err
	}
	plaintext = hex.EncodeToString(buf)
	hash = hashRefreshToken(plaintext)
	return plaintext, hash, nil
}

// hashRefreshToken hashes a plaintext refresh token for lookup/storage -
// used both when issuing a new token and when verifying one presented by a
// client (RefreshSession, Logout).
func hashRefreshToken(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}
