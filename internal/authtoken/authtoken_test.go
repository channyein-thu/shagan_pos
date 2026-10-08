package authtoken

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAccessToken_CarriesAccountType(t *testing.T) {
	secret := []byte("s")
	branch := uint(9)

	tok, err := GenerateAccessToken(secret, 1, 2, "pos", &branch, time.Minute)
	require.NoError(t, err)

	claims, err := ParseAccessToken(secret, tok)
	require.NoError(t, err)
	assert.Equal(t, "pos", claims.AccountType)
	assert.Equal(t, uint(1), claims.UserID)
	assert.Equal(t, uint(2), claims.OrgID)
	require.NotNil(t, claims.BranchID)
	assert.Equal(t, branch, *claims.BranchID)
}
