//go:build cgo

package identity

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"shagan_pos/internal/common"
)

func newInternalTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	name := strings.NewReplacer("/", "_", " ", "_").Replace(t.Name())
	db, err := gorm.Open(
		sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", name)),
		&gorm.Config{Logger: logger.Default.LogMode(logger.Silent)},
	)
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Organization{}, &Branch{}, &Device{}, &User{}, &Session{}))
	return db
}

func seedUser(t *testing.T, db *gorm.DB, orgID uint, typ AccountType, email string) *User {
	t.Helper()
	e := email
	u := &User{OrgID: orgID, AccountType: typ, Email: &e, CredentialHash: "old-hash"}
	require.NoError(t, db.Create(u).Error)
	return u
}

func liveSessions(t *testing.T, db *gorm.DB, userID uint) int64 {
	t.Helper()
	var n int64
	require.NoError(t, db.Model(&Session{}).Where("user_id = ? AND revoked_at IS NULL", userID).Count(&n).Error)
	return n
}

func requireStatus(t *testing.T, err error, status int) {
	t.Helper()
	var rest common.RestError
	require.True(t, errors.As(err, &rest), "want a common.RestError, got %T: %v", err, err)
	require.Equal(t, status, rest.Status)
}

func TestRepository_ListOrgWideAccounts_OnlyOwnerAndServiceCenterOfThatOrg(t *testing.T) {
	db := newInternalTestDB(t)
	repo := NewRepository(db)
	owner := seedUser(t, db, 7, AccountTypeOwner, "o@a.test")
	sc := seedUser(t, db, 7, AccountTypeServiceCenter, "sc@a.test")
	seedUser(t, db, 7, AccountTypePos, "pos@a.test")
	seedUser(t, db, 8, AccountTypeOwner, "other-org-owner@b.test")

	got, err := repo.ListOrgWideAccounts(context.Background(), 7)

	require.NoError(t, err)
	require.Len(t, got, 2, "no pos account, nothing from another org")
	require.Equal(t, owner.ID, got[0].ID)
	require.Equal(t, sc.ID, got[1].ID)
}

func TestRepository_ResetOrgWideAccountPassword_ChangesHashAndRevokesLiveSessions(t *testing.T) {
	db := newInternalTestDB(t)
	repo := NewRepository(db)
	owner := seedUser(t, db, 7, AccountTypeOwner, "o@a.test")
	bystander := seedUser(t, db, 7, AccountTypeServiceCenter, "sc@a.test")
	exp := time.Now().Add(time.Hour)
	require.NoError(t, db.Create(&Session{UserID: owner.ID, RefreshHash: "a", ExpiresAt: exp}).Error)
	require.NoError(t, db.Create(&Session{UserID: owner.ID, RefreshHash: "b", ExpiresAt: exp}).Error)
	require.NoError(t, db.Create(&Session{UserID: bystander.ID, RefreshHash: "c", ExpiresAt: exp}).Error)

	require.NoError(t, repo.ResetOrgWideAccountPassword(context.Background(), owner.ID, "new-hash"))

	var got User
	require.NoError(t, db.First(&got, owner.ID).Error)
	require.Equal(t, "new-hash", got.CredentialHash)
	require.Zero(t, liveSessions(t, db, owner.ID), "every refresh token of the reset account is cut off")
	require.Equal(t, int64(1), liveSessions(t, db, bystander.ID), "another user's sessions are untouched")
}

func TestRepository_ResetOrgWideAccountPassword_RefusesPosAccountsAndUnknownIDs(t *testing.T) {
	db := newInternalTestDB(t)
	repo := NewRepository(db)
	pos := seedUser(t, db, 7, AccountTypePos, "pos@a.test")

	err := repo.ResetOrgWideAccountPassword(context.Background(), pos.ID, "new-hash")
	requireStatus(t, err, http.StatusNotFound)
	var got User
	require.NoError(t, db.First(&got, pos.ID).Error)
	require.Equal(t, "old-hash", got.CredentialHash, "a pos login must not be reachable through the org-wide endpoint")

	requireStatus(t, repo.ResetOrgWideAccountPassword(context.Background(), 99999, "x"), http.StatusNotFound)
}

func TestRepository_ResetPosAccountPassword_AlsoRevokesSessions(t *testing.T) {
	db := newInternalTestDB(t)
	repo := NewRepository(db)
	pos := seedUser(t, db, 7, AccountTypePos, "pos@a.test")
	require.NoError(t, db.Create(&Session{UserID: pos.ID, RefreshHash: "a", ExpiresAt: time.Now().Add(time.Hour)}).Error)

	require.NoError(t, repo.ResetPosAccountPassword(context.Background(), pos.ID, "new-hash"))

	require.Zero(t, liveSessions(t, db, pos.ID))
}

func TestRepository_ResetOrgWideAccountPassword_AlreadyRevokedSessionsKeepTheirOriginalTimestamp(t *testing.T) {
	db := newInternalTestDB(t)
	repo := NewRepository(db)
	owner := seedUser(t, db, 7, AccountTypeOwner, "o@a.test")
	earlier := time.Now().Add(-48 * time.Hour).Truncate(time.Second)
	require.NoError(t, db.Create(&Session{UserID: owner.ID, RefreshHash: "a", ExpiresAt: time.Now().Add(time.Hour), RevokedAt: &earlier}).Error)

	require.NoError(t, repo.ResetOrgWideAccountPassword(context.Background(), owner.ID, "new-hash"))

	var s Session
	require.NoError(t, db.Where("user_id = ?", owner.ID).First(&s).Error)
	require.WithinDuration(t, earlier, *s.RevokedAt, time.Second)
}

func TestRepository_UpdateBranchStatusInternal(t *testing.T) {
	db := newInternalTestDB(t)
	repo := NewRepository(db)
	b := &Branch{OrgID: 7, Name: "B", Status: BranchStatusActive, Address: "-", Phone: "-"}
	require.NoError(t, db.Create(b).Error)

	require.NoError(t, repo.UpdateBranchStatusInternal(context.Background(), b.ID, BranchStatusInactive))
	var got Branch
	require.NoError(t, db.First(&got, b.ID).Error)
	require.Equal(t, BranchStatusInactive, got.Status)
	require.Equal(t, "B", got.Name, "only the status changes")

	// Setting the same value is not "not found".
	require.NoError(t, repo.UpdateBranchStatusInternal(context.Background(), b.ID, BranchStatusInactive))
	requireStatus(t, repo.UpdateBranchStatusInternal(context.Background(), 99999, BranchStatusActive), http.StatusNotFound)
}

func TestRepository_UpdateDeviceStatusInternal(t *testing.T) {
	db := newInternalTestDB(t)
	repo := NewRepository(db)
	d := &Device{BranchID: 1, Name: "R1", Status: DeviceStatusActive, LastSeenAt: time.Now()}
	require.NoError(t, db.Create(d).Error)

	require.NoError(t, repo.UpdateDeviceStatusInternal(context.Background(), d.ID, DeviceStatusRevoked))
	var got Device
	require.NoError(t, db.First(&got, d.ID).Error)
	require.Equal(t, DeviceStatusRevoked, got.Status)
	require.Equal(t, "R1", got.Name)

	requireStatus(t, repo.UpdateDeviceStatusInternal(context.Background(), 99999, DeviceStatusActive), http.StatusNotFound)
}
