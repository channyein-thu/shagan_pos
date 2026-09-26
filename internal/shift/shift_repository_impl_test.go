//go:build cgo

package shift

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"shagan_pos/internal/identity"
	"shagan_pos/internal/sales"
)

func TestRepository_OpenShift_CreatesShiftWhenBusinessRulesPass(t *testing.T) {
	db := newOpenShiftTestDB(t)
	branch, staff, device := seedActiveOpenShiftResources(t, db, 7)
	repo := NewRepository(db)
	openedAt := time.Date(2026, time.September, 15, 2, 30, 0, 0, time.UTC)

	got, err := repo.OpenShift(context.Background(), OpenShiftRequest{
		OrgID:       7,
		BranchID:    branch.ID,
		StaffID:     staff.ID,
		DeviceID:    device.ID,
		OpenedAt:    openedAt,
		OpeningCash: decimal.RequireFromString("250.50"),
		Status:      ShiftStatusOpen,
	})

	require.NoError(t, err)
	require.NotZero(t, got.ID)
	require.Equal(t, branch.ID, got.BranchID)
	require.Equal(t, staff.ID, got.StaffID)
	require.Equal(t, device.ID, got.DeviceID)
	require.Equal(t, ShiftStatusOpen, got.Status)
	require.Nil(t, got.ClosedAt)
	require.True(t, decimal.RequireFromString("250.50").Equal(got.OpeningCash))

	var persisted Shift
	require.NoError(t, db.First(&persisted, got.ID).Error)
	require.Equal(t, got.ID, persisted.ID)
}

func TestRepository_OpenShift_RejectsBranchOutsideAuthenticatedOrganization(t *testing.T) {
	db := newOpenShiftTestDB(t)
	branch, staff, device := seedActiveOpenShiftResources(t, db, 99)
	repo := NewRepository(db)

	got, err := repo.OpenShift(context.Background(), repositoryOpenShiftRequest(7, branch.ID, staff.ID, device.ID))

	require.Nil(t, got)
	requireRestErrorStatus(t, err, http.StatusNotFound)
	require.Equal(t, int64(0), countShifts(t, db))
}

func TestRepository_OpenShift_RejectsInactiveOrCrossBranchResources(t *testing.T) {
	tests := []struct {
		name       string
		wantStatus int
		arrange    func(t *testing.T, db *gorm.DB, branch *identity.Branch, staff *identity.Staff, device *identity.Device)
	}{
		{
			name:       "inactive branch",
			wantStatus: http.StatusConflict,
			arrange: func(t *testing.T, db *gorm.DB, branch *identity.Branch, _ *identity.Staff, _ *identity.Device) {
				require.NoError(t, db.Model(branch).Update("status", identity.BranchStatusInactive).Error)
			},
		},
		{
			name:       "inactive staff",
			wantStatus: http.StatusConflict,
			arrange: func(t *testing.T, db *gorm.DB, _ *identity.Branch, staff *identity.Staff, _ *identity.Device) {
				require.NoError(t, db.Model(staff).Update("status", identity.StaffStatusInactive).Error)
			},
		},
		{
			name:       "inactive device",
			wantStatus: http.StatusConflict,
			arrange: func(t *testing.T, db *gorm.DB, _ *identity.Branch, _ *identity.Staff, device *identity.Device) {
				require.NoError(t, db.Model(device).Update("status", identity.DeviceStatusInactive).Error)
			},
		},
		{
			name:       "staff belongs to another branch",
			wantStatus: http.StatusNotFound,
			arrange: func(t *testing.T, db *gorm.DB, _ *identity.Branch, staff *identity.Staff, _ *identity.Device) {
				other := identity.Branch{OrgID: 7, Name: "Other", Status: identity.BranchStatusActive, Address: "-", Phone: "-"}
				require.NoError(t, db.Create(&other).Error)
				require.NoError(t, db.Model(staff).Update("branch_id", other.ID).Error)
			},
		},
		{
			name:       "device belongs to another branch",
			wantStatus: http.StatusNotFound,
			arrange: func(t *testing.T, db *gorm.DB, _ *identity.Branch, _ *identity.Staff, device *identity.Device) {
				other := identity.Branch{OrgID: 7, Name: "Other", Status: identity.BranchStatusActive, Address: "-", Phone: "-"}
				require.NoError(t, db.Create(&other).Error)
				require.NoError(t, db.Model(device).Update("branch_id", other.ID).Error)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := newOpenShiftTestDB(t)
			branch, staff, device := seedActiveOpenShiftResources(t, db, 7)
			tt.arrange(t, db, branch, staff, device)

			got, err := NewRepository(db).OpenShift(
				context.Background(),
				repositoryOpenShiftRequest(7, branch.ID, staff.ID, device.ID),
			)

			require.Nil(t, got)
			requireRestErrorStatus(t, err, tt.wantStatus)
			require.Equal(t, int64(0), countShifts(t, db))
		})
	}
}

func TestRepository_OpenShift_RejectsExistingOpenShiftForStaffOrDevice(t *testing.T) {
	tests := []struct {
		name        string
		reuseStaff  bool
		reuseDevice bool
	}{
		{name: "same staff on another device", reuseStaff: true},
		{name: "another staff on same device", reuseDevice: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := newOpenShiftTestDB(t)
			branch, firstStaff, firstDevice := seedActiveOpenShiftResources(t, db, 7)
			secondStaff := identity.Staff{BranchID: branch.ID, Name: "Second", RoleID: 1, PinHash: "hash", Phone: "2", Status: identity.StaffStatusActive}
			secondDevice := identity.Device{BranchID: branch.ID, Name: "Second", Status: identity.DeviceStatusActive, LastSeenAt: time.Now()}
			require.NoError(t, db.Create(&secondStaff).Error)
			require.NoError(t, db.Create(&secondDevice).Error)

			existing := Shift{
				BranchID: branch.ID,
				StaffID:  firstStaff.ID,
				DeviceID: firstDevice.ID,
				OpenedAt: time.Now().UTC(),
				Status:   ShiftStatusOpen,
			}
			require.NoError(t, db.Create(&existing).Error)

			staffID, deviceID := secondStaff.ID, secondDevice.ID
			if tt.reuseStaff {
				staffID = firstStaff.ID
			}
			if tt.reuseDevice {
				deviceID = firstDevice.ID
			}

			got, err := NewRepository(db).OpenShift(
				context.Background(),
				repositoryOpenShiftRequest(7, branch.ID, staffID, deviceID),
			)

			require.Nil(t, got)
			requireRestErrorStatus(t, err, http.StatusConflict)
			require.Equal(t, int64(1), countShifts(t, db))
		})
	}
}

func TestRepository_OpenShift_AllowsStaffAndDeviceAfterPreviousShiftClosed(t *testing.T) {
	db := newOpenShiftTestDB(t)
	branch, staff, device := seedActiveOpenShiftResources(t, db, 7)
	closedAt := time.Now().UTC()
	previous := Shift{
		BranchID: branch.ID,
		StaffID:  staff.ID,
		DeviceID: device.ID,
		OpenedAt: closedAt.Add(-8 * time.Hour),
		ClosedAt: &closedAt,
		Status:   ShiftStatusClosed,
	}
	require.NoError(t, db.Create(&previous).Error)

	got, err := NewRepository(db).OpenShift(
		context.Background(),
		repositoryOpenShiftRequest(7, branch.ID, staff.ID, device.ID),
	)

	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, ShiftStatusOpen, got.Status)
	require.Equal(t, int64(2), countShifts(t, db))
}

func TestRepository_GetCurrentShift_ReturnsPairedDevicesOpenShift(t *testing.T) {
	db := newOpenShiftTestDB(t)
	branch, staff, device := seedActiveOpenShiftResources(t, db, 7)
	user := seedPOSUser(t, db, 7, branch.ID, device.ID)
	closedAt := time.Now().UTC().Add(-time.Hour)
	closed := Shift{
		BranchID: branch.ID,
		StaffID:  staff.ID,
		DeviceID: device.ID,
		OpenedAt: closedAt.Add(-8 * time.Hour),
		ClosedAt: &closedAt,
		Status:   ShiftStatusClosed,
	}
	current := Shift{
		BranchID:    branch.ID,
		StaffID:     staff.ID,
		DeviceID:    device.ID,
		OpenedAt:    time.Now().UTC(),
		OpeningCash: decimal.NewFromInt(100),
		Status:      ShiftStatusOpen,
	}
	require.NoError(t, db.Create(&closed).Error)
	require.NoError(t, db.Create(&current).Error)

	got, err := NewRepository(db).GetCurrentShift(context.Background(), 7, user.ID)

	require.NoError(t, err)
	require.Equal(t, current.ID, got.ID)
	require.Equal(t, ShiftStatusOpen, got.Status)
}

func TestRepository_GetCurrentShift_DoesNotReturnAnotherDeviceShift(t *testing.T) {
	db := newOpenShiftTestDB(t)
	branch, staff, device := seedActiveOpenShiftResources(t, db, 7)
	otherDevice := identity.Device{BranchID: branch.ID, Name: "POS-2", Status: identity.DeviceStatusActive, LastSeenAt: time.Now()}
	require.NoError(t, db.Create(&otherDevice).Error)
	user := seedPOSUser(t, db, 7, branch.ID, device.ID)
	otherShift := Shift{
		BranchID: branch.ID,
		StaffID:  staff.ID,
		DeviceID: otherDevice.ID,
		OpenedAt: time.Now().UTC(),
		Status:   ShiftStatusOpen,
	}
	require.NoError(t, db.Create(&otherShift).Error)

	got, err := NewRepository(db).GetCurrentShift(context.Background(), 7, user.ID)

	require.Nil(t, got)
	requireRestErrorStatus(t, err, http.StatusNotFound)
}

func TestRepository_GetCurrentShift_DoesNotTrustCrossOrganizationBranchOnUser(t *testing.T) {
	db := newOpenShiftTestDB(t)
	branch, staff, device := seedActiveOpenShiftResources(t, db, 99)
	user := seedPOSUser(t, db, 7, branch.ID, device.ID)
	shift := Shift{
		BranchID: branch.ID,
		StaffID:  staff.ID,
		DeviceID: device.ID,
		OpenedAt: time.Now().UTC(),
		Status:   ShiftStatusOpen,
	}
	require.NoError(t, db.Create(&shift).Error)

	got, err := NewRepository(db).GetCurrentShift(context.Background(), 7, user.ID)

	require.Nil(t, got)
	requireRestErrorStatus(t, err, http.StatusNotFound)
}

func TestRepository_GetCurrentShift_RejectsCrossOrganizationOrUnpairedAccount(t *testing.T) {
	tests := []struct {
		name       string
		orgID      uint
		account    identity.AccountType
		paired     bool
		wantStatus int
	}{
		{name: "user outside authenticated organization", orgID: 99, account: identity.AccountTypePos, paired: true, wantStatus: http.StatusNotFound},
		{name: "owner account", orgID: 7, account: identity.AccountTypeOwner, paired: false, wantStatus: http.StatusForbidden},
		{name: "unpaired pos account", orgID: 7, account: identity.AccountTypePos, paired: false, wantStatus: http.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := newOpenShiftTestDB(t)
			branch, _, device := seedActiveOpenShiftResources(t, db, 7)
			user := identity.User{OrgID: 7, AccountType: tt.account, CredentialHash: "hash"}
			if tt.paired {
				user.BranchID = &branch.ID
				user.DeviceID = &device.ID
			}
			require.NoError(t, db.Create(&user).Error)

			got, err := NewRepository(db).GetCurrentShift(context.Background(), tt.orgID, user.ID)

			require.Nil(t, got)
			requireRestErrorStatus(t, err, tt.wantStatus)
		})
	}
}

func TestRepository_GetShift_ReturnsOpenOrClosedShiftInOrganization(t *testing.T) {
	tests := []struct {
		name   string
		status ShiftStatus
	}{
		{name: "open shift", status: ShiftStatusOpen},
		{name: "closed shift", status: ShiftStatusClosed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := newOpenShiftTestDB(t)
			branch, staff, device := seedActiveOpenShiftResources(t, db, 7)
			shift := Shift{
				BranchID: branch.ID,
				StaffID:  staff.ID,
				DeviceID: device.ID,
				OpenedAt: time.Now().UTC(),
				Status:   tt.status,
			}
			if tt.status == ShiftStatusClosed {
				closedAt := time.Now().UTC()
				shift.ClosedAt = &closedAt
			}
			require.NoError(t, db.Create(&shift).Error)

			got, err := NewRepository(db).GetShift(context.Background(), AccessScope{OrgID: 7}, shift.ID)

			require.NoError(t, err)
			require.Equal(t, shift.ID, got.ID)
			require.Equal(t, tt.status, got.Status)
		})
	}
}

func TestRepository_GetShift_HidesMissingAndCrossOrganizationShift(t *testing.T) {
	tests := []struct {
		name       string
		orgID      uint
		useSavedID bool
	}{
		{name: "missing shift", orgID: 7},
		{name: "shift in another organization", orgID: 99, useSavedID: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := newOpenShiftTestDB(t)
			branch, staff, device := seedActiveOpenShiftResources(t, db, 7)
			shift := Shift{BranchID: branch.ID, StaffID: staff.ID, DeviceID: device.ID, OpenedAt: time.Now().UTC(), Status: ShiftStatusOpen}
			require.NoError(t, db.Create(&shift).Error)
			shiftID := uint(999)
			if tt.useSavedID {
				shiftID = shift.ID
			}

			got, err := NewRepository(db).GetShift(context.Background(), AccessScope{OrgID: tt.orgID}, shiftID)

			require.Nil(t, got)
			requireRestErrorStatus(t, err, http.StatusNotFound)
			require.EqualError(t, err, "shift not found")
		})
	}
}

func TestRepository_CloseShift_ClosesAtomicallyAndWritesPaymentSnapshot(t *testing.T) {
	db := newOpenShiftTestDB(t)
	branch, staff, device := seedActiveOpenShiftResources(t, db, 7)
	shift := Shift{
		BranchID:    branch.ID,
		StaffID:     staff.ID,
		DeviceID:    device.ID,
		OpenedAt:    time.Now().UTC().Add(-8 * time.Hour),
		OpeningCash: decimal.NewFromInt(100),
		Status:      ShiftStatusOpen,
	}
	require.NoError(t, db.Create(&shift).Error)
	completedAt := time.Now().UTC().Add(-time.Hour)
	sale := sales.Sale{
		ID:          uuid.New(),
		OrgID:       7,
		BranchID:    branch.ID,
		ShiftID:     shift.ID,
		StaffID:     staff.ID,
		DeviceID:    device.ID,
		Total:       decimal.RequireFromString("87.25"),
		Status:      sales.SaleStatusCompleted,
		CompletedAt: &completedAt,
	}
	require.NoError(t, db.Create(&sale).Error)
	payments := []sales.Payment{
		{SaleID: sale.ID, Method: sales.PaymentMethodCash, Amount: decimal.RequireFromString("50.25")},
		{SaleID: sale.ID, Method: sales.PaymentMethodCard, Amount: decimal.NewFromInt(20)},
		{SaleID: sale.ID, Method: sales.PaymentMethodQR, Amount: decimal.NewFromInt(10)},
		{SaleID: sale.ID, Method: sales.PaymentMethodMobileWallet, Amount: decimal.NewFromInt(5)},
		{SaleID: sale.ID, Method: sales.PaymentMethodStoreCredit, Amount: decimal.NewFromInt(1)},
		{SaleID: sale.ID, Method: sales.PaymentMethodOther, Amount: decimal.NewFromInt(1)},
	}
	require.NoError(t, db.Create(&payments).Error)
	closedAt := time.Date(2026, time.September, 15, 14, 45, 0, 0, time.UTC)

	got, err := NewRepository(db).CloseShift(context.Background(), AccessScope{OrgID: 7}, shift.ID, closedAt, staff.ID,
		CloseShiftRequest{ClosingCash: decimal.RequireFromString("150.25")})

	require.NoError(t, err)
	require.Equal(t, ShiftStatusClosed, got.Status)
	require.NotNil(t, got.ClosedAt)
	require.Equal(t, closedAt, *got.ClosedAt)

	var reconciliations []ShiftReconciliation
	require.NoError(t, db.Where("shift_id = ?", shift.ID).Find(&reconciliations).Error)
	require.Len(t, reconciliations, 5)
	wantExpected := map[ReconciliationMethod]decimal.Decimal{
		ReconciliationMethodCash:   decimal.RequireFromString("150.25"),
		ReconciliationMethodCard:   decimal.NewFromInt(20),
		ReconciliationMethodQR:     decimal.NewFromInt(10),
		ReconciliationMethodMobile: decimal.NewFromInt(5),
		ReconciliationMethodOther:  decimal.NewFromInt(2),
	}
	for _, reconciliation := range reconciliations {
		want, ok := wantExpected[reconciliation.Method]
		require.True(t, ok, "unexpected reconciliation method %q", reconciliation.Method)
		require.True(t, want.Equal(reconciliation.Expected))
		require.True(t, want.Equal(reconciliation.Counted))
		require.True(t, reconciliation.Difference.IsZero())
		require.Empty(t, reconciliation.Reason)
	}
}

func TestRepository_CloseShift_WithNoSalesStillReconcilesOpeningCash(t *testing.T) {
	db := newOpenShiftTestDB(t)
	branch, staff, device := seedActiveOpenShiftResources(t, db, 7)
	shift := Shift{
		BranchID: branch.ID, StaffID: staff.ID, DeviceID: device.ID,
		OpenedAt: time.Now().UTC(), OpeningCash: decimal.NewFromInt(25), Status: ShiftStatusOpen,
	}
	require.NoError(t, db.Create(&shift).Error)

	got, err := NewRepository(db).CloseShift(context.Background(), AccessScope{OrgID: 7}, shift.ID, time.Now().UTC(), staff.ID,
		CloseShiftRequest{ClosingCash: decimal.NewFromInt(25)})

	require.NoError(t, err)
	require.Equal(t, ShiftStatusClosed, got.Status)
	var reconciliations []ShiftReconciliation
	require.NoError(t, db.Where("shift_id = ?", shift.ID).Find(&reconciliations).Error)
	require.Len(t, reconciliations, 1)
	require.Equal(t, ReconciliationMethodCash, reconciliations[0].Method)
	require.True(t, decimal.NewFromInt(25).Equal(reconciliations[0].Expected))
	require.True(t, decimal.NewFromInt(25).Equal(reconciliations[0].Counted))
	require.True(t, reconciliations[0].Difference.IsZero())
}

func TestRepository_CloseShift_CashShortWithReason_RecordsDifference(t *testing.T) {
	db := newOpenShiftTestDB(t)
	branch, staff, device := seedActiveOpenShiftResources(t, db, 7)
	shift := Shift{
		BranchID: branch.ID, StaffID: staff.ID, DeviceID: device.ID,
		OpenedAt: time.Now().UTC(), OpeningCash: decimal.NewFromInt(100), Status: ShiftStatusOpen,
	}
	require.NoError(t, db.Create(&shift).Error)

	got, err := NewRepository(db).CloseShift(context.Background(), AccessScope{OrgID: 7}, shift.ID, time.Now().UTC(), staff.ID,
		CloseShiftRequest{ClosingCash: decimal.NewFromInt(90), Reason: "till was short at count"})

	require.NoError(t, err)
	require.Equal(t, ShiftStatusClosed, got.Status)
	var reconciliation ShiftReconciliation
	require.NoError(t, db.Where("shift_id = ? AND method = ?", shift.ID, ReconciliationMethodCash).First(&reconciliation).Error)
	require.True(t, decimal.NewFromInt(100).Equal(reconciliation.Expected))
	require.True(t, decimal.NewFromInt(90).Equal(reconciliation.Counted))
	require.True(t, decimal.NewFromInt(-10).Equal(reconciliation.Difference))
	require.Equal(t, "till was short at count", reconciliation.Reason)
}

func TestRepository_CloseShift_CashMismatchWithoutReason_RejectsAndRollsBack(t *testing.T) {
	db := newOpenShiftTestDB(t)
	branch, staff, device := seedActiveOpenShiftResources(t, db, 7)
	shift := Shift{
		BranchID: branch.ID, StaffID: staff.ID, DeviceID: device.ID,
		OpenedAt: time.Now().UTC(), OpeningCash: decimal.NewFromInt(100), Status: ShiftStatusOpen,
	}
	require.NoError(t, db.Create(&shift).Error)

	got, err := NewRepository(db).CloseShift(context.Background(), AccessScope{OrgID: 7}, shift.ID, time.Now().UTC(), staff.ID,
		CloseShiftRequest{ClosingCash: decimal.NewFromInt(90)})

	require.Nil(t, got)
	requireRestErrorStatus(t, err, http.StatusBadRequest)
	var persisted Shift
	require.NoError(t, db.First(&persisted, shift.ID).Error)
	require.Equal(t, ShiftStatusOpen, persisted.Status)
	require.Equal(t, int64(0), countReconciliations(t, db, shift.ID))
}

func TestRepository_CloseShift_RejectsWhenCloserIsNotTheStaffWhoOpenedIt(t *testing.T) {
	db := newOpenShiftTestDB(t)
	branch, staff, device := seedActiveOpenShiftResources(t, db, 7)
	otherStaff := identity.Staff{BranchID: branch.ID, Name: "Other Cashier", RoleID: 1, PinHash: "hash", Phone: "2", Status: identity.StaffStatusActive}
	require.NoError(t, db.Create(&otherStaff).Error)
	shift := Shift{
		BranchID: branch.ID, StaffID: staff.ID, DeviceID: device.ID,
		OpenedAt: time.Now().UTC(), OpeningCash: decimal.NewFromInt(100), Status: ShiftStatusOpen,
	}
	require.NoError(t, db.Create(&shift).Error)

	got, err := NewRepository(db).CloseShift(context.Background(), AccessScope{OrgID: 7}, shift.ID, time.Now().UTC(), otherStaff.ID,
		CloseShiftRequest{ClosingCash: decimal.NewFromInt(100)})

	require.Nil(t, got)
	requireRestErrorStatus(t, err, http.StatusForbidden)
	var persisted Shift
	require.NoError(t, db.First(&persisted, shift.ID).Error)
	require.Equal(t, ShiftStatusOpen, persisted.Status)
	require.Equal(t, int64(0), countReconciliations(t, db, shift.ID))
}

func TestRepository_CloseShift_RejectsOpenSaleAndRollsBack(t *testing.T) {
	db := newOpenShiftTestDB(t)
	branch, staff, device := seedActiveOpenShiftResources(t, db, 7)
	shift := Shift{BranchID: branch.ID, StaffID: staff.ID, DeviceID: device.ID, OpenedAt: time.Now().UTC(), Status: ShiftStatusOpen}
	require.NoError(t, db.Create(&shift).Error)
	openSale := sales.Sale{
		ID: uuid.New(), OrgID: 7, BranchID: branch.ID, ShiftID: shift.ID,
		StaffID: staff.ID, DeviceID: device.ID, Status: sales.SaleStatusOpen,
	}
	require.NoError(t, db.Create(&openSale).Error)

	got, err := NewRepository(db).CloseShift(context.Background(), AccessScope{OrgID: 7}, shift.ID, time.Now().UTC(), staff.ID,
		CloseShiftRequest{ClosingCash: decimal.Zero})

	require.Nil(t, got)
	requireRestErrorStatus(t, err, http.StatusConflict)
	require.EqualError(t, err, "shift has open sales")
	var persisted Shift
	require.NoError(t, db.First(&persisted, shift.ID).Error)
	require.Equal(t, ShiftStatusOpen, persisted.Status)
	require.Nil(t, persisted.ClosedAt)
	require.Equal(t, int64(0), countReconciliations(t, db, shift.ID))
}

func TestRepository_CloseShift_RejectsAlreadyClosedMissingAndCrossOrganizationShift(t *testing.T) {
	tests := []struct {
		name       string
		orgID      uint
		status     ShiftStatus
		useSavedID bool
		wantStatus int
		wantError  string
	}{
		{name: "already closed", orgID: 7, status: ShiftStatusClosed, useSavedID: true, wantStatus: http.StatusConflict, wantError: "shift is already closed"},
		{name: "missing", orgID: 7, status: ShiftStatusOpen, wantStatus: http.StatusNotFound, wantError: "shift not found"},
		{name: "another organization", orgID: 99, status: ShiftStatusOpen, useSavedID: true, wantStatus: http.StatusNotFound, wantError: "shift not found"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := newOpenShiftTestDB(t)
			branch, staff, device := seedActiveOpenShiftResources(t, db, 7)
			shift := Shift{BranchID: branch.ID, StaffID: staff.ID, DeviceID: device.ID, OpenedAt: time.Now().UTC(), Status: tt.status}
			if tt.status == ShiftStatusClosed {
				closedAt := time.Now().UTC()
				shift.ClosedAt = &closedAt
			}
			require.NoError(t, db.Create(&shift).Error)
			shiftID := uint(999)
			if tt.useSavedID {
				shiftID = shift.ID
			}

			got, err := NewRepository(db).CloseShift(context.Background(), AccessScope{OrgID: tt.orgID}, shiftID, time.Now().UTC(), staff.ID,
				CloseShiftRequest{ClosingCash: decimal.Zero})

			require.Nil(t, got)
			requireRestErrorStatus(t, err, tt.wantStatus)
			require.EqualError(t, err, tt.wantError)
			require.Equal(t, int64(0), countReconciliations(t, db, shift.ID))
		})
	}
}

func newOpenShiftTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dbName := strings.NewReplacer("/", "_", " ", "_").Replace(t.Name())
	db, err := gorm.Open(
		sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", dbName)),
		&gorm.Config{Logger: logger.Default.LogMode(logger.Silent)},
	)
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&identity.Branch{}, &identity.Staff{}, &identity.Device{}, &identity.User{},
		&sales.Sale{}, &sales.Payment{},
		&Shift{}, &ShiftReconciliation{}, &DrawerEvent{}, &Expense{},
	))
	return db
}

func seedPOSUser(t *testing.T, db *gorm.DB, orgID, branchID, deviceID uint) *identity.User {
	t.Helper()
	user := &identity.User{
		OrgID:          orgID,
		AccountType:    identity.AccountTypePos,
		BranchID:       &branchID,
		DeviceID:       &deviceID,
		CredentialHash: "hash",
	}
	require.NoError(t, db.Create(user).Error)
	return user
}

func seedActiveOpenShiftResources(t *testing.T, db *gorm.DB, orgID uint) (*identity.Branch, *identity.Staff, *identity.Device) {
	t.Helper()
	branch := &identity.Branch{OrgID: orgID, Name: "Main", Status: identity.BranchStatusActive, Address: "-", Phone: "-"}
	require.NoError(t, db.Create(branch).Error)
	staff := &identity.Staff{BranchID: branch.ID, Name: "Cashier", RoleID: 1, PinHash: "hash", Phone: "1", Status: identity.StaffStatusActive}
	require.NoError(t, db.Create(staff).Error)
	device := &identity.Device{BranchID: branch.ID, Name: "POS-1", Status: identity.DeviceStatusActive, LastSeenAt: time.Now()}
	require.NoError(t, db.Create(device).Error)
	return branch, staff, device
}

func repositoryOpenShiftRequest(orgID, branchID, staffID, deviceID uint) OpenShiftRequest {
	return OpenShiftRequest{
		OrgID:       orgID,
		BranchID:    branchID,
		StaffID:     staffID,
		DeviceID:    deviceID,
		OpenedAt:    time.Now().UTC(),
		OpeningCash: decimal.NewFromInt(100),
		Status:      ShiftStatusOpen,
	}
}

func countShifts(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	var count int64
	require.NoError(t, db.Model(&Shift{}).Count(&count).Error)
	return count
}

func countReconciliations(t *testing.T, db *gorm.DB, shiftID uint) int64 {
	t.Helper()
	var count int64
	require.NoError(t, db.Model(&ShiftReconciliation{}).Where("shift_id = ?", shiftID).Count(&count).Error)
	return count
}
func TestRepository_GetShiftSummary_ReturnsTenantScopedTotals(t *testing.T) {
	db := newOpenShiftTestDB(t)
	branch, staff, device := seedActiveOpenShiftResources(t, db, 7)
	shift := seedShift(t, db, branch.ID, staff.ID, device.ID, ShiftStatusClosed)
	completedAt := time.Now().UTC()
	completed := sales.Sale{
		ID: uuid.New(), OrgID: 7, BranchID: branch.ID, ShiftID: shift.ID,
		StaffID: staff.ID, DeviceID: device.ID, Total: decimal.NewFromInt(75),
		Status: sales.SaleStatusCompleted, CompletedAt: &completedAt,
	}
	voided := sales.Sale{
		ID: uuid.New(), OrgID: 7, BranchID: branch.ID, ShiftID: shift.ID,
		StaffID: staff.ID, DeviceID: device.ID, Total: decimal.NewFromInt(999),
		Status: sales.SaleStatusVoided,
	}
	require.NoError(t, db.Create(&[]sales.Sale{completed, voided}).Error)
	require.NoError(t, db.Create(&[]sales.Payment{
		{SaleID: completed.ID, Method: sales.PaymentMethodCash, Amount: decimal.NewFromInt(50)},
		{SaleID: completed.ID, Method: sales.PaymentMethodCard, Amount: decimal.NewFromInt(25)},
		{SaleID: voided.ID, Method: sales.PaymentMethodCash, Amount: decimal.NewFromInt(999)},
	}).Error)
	reconciliation := ShiftReconciliation{
		ShiftID: shift.ID, Method: ReconciliationMethodCash,
		Expected: decimal.NewFromInt(150), Counted: decimal.NewFromInt(150),
	}
	require.NoError(t, db.Create(&reconciliation).Error)

	got, err := NewRepository(db).GetShiftSummary(context.Background(), AccessScope{OrgID: 7}, shift.ID)

	require.NoError(t, err)
	require.Equal(t, int64(1), got["sales_count"])
	require.True(t, decimal.NewFromInt(75).Equal(got["sales_total"].(decimal.Decimal)))
	require.True(t, decimal.NewFromInt(150).Equal(got["expected_cash"].(decimal.Decimal)))
	paymentTotals := got["payment_totals"].(map[string]decimal.Decimal)
	require.True(t, decimal.NewFromInt(50).Equal(paymentTotals[string(sales.PaymentMethodCash)]))
	require.True(t, decimal.NewFromInt(25).Equal(paymentTotals[string(sales.PaymentMethodCard)]))
	require.Len(t, got["reconciliations"].([]ShiftReconciliation), 1)

	_, err = NewRepository(db).GetShiftSummary(context.Background(), AccessScope{OrgID: 99}, shift.ID)
	requireRestErrorStatus(t, err, http.StatusNotFound)
}

func TestRepository_ListShiftReconciliations_EnforcesBranchScope(t *testing.T) {
	db := newOpenShiftTestDB(t)
	branch, staff, device := seedActiveOpenShiftResources(t, db, 7)
	shift := seedShift(t, db, branch.ID, staff.ID, device.ID, ShiftStatusClosed)
	require.NoError(t, db.Create(&[]ShiftReconciliation{
		{ShiftID: shift.ID, Method: ReconciliationMethodCash},
		{ShiftID: shift.ID, Method: ReconciliationMethodCard},
	}).Error)

	branchID := branch.ID
	got, err := NewRepository(db).ListShiftReconciliations(context.Background(), AccessScope{OrgID: 7, BranchID: &branchID}, shift.ID)
	require.NoError(t, err)
	require.Len(t, got, 2)

	otherBranchID := branch.ID + 999
	_, err = NewRepository(db).ListShiftReconciliations(context.Background(), AccessScope{OrgID: 7, BranchID: &otherBranchID}, shift.ID)
	requireRestErrorStatus(t, err, http.StatusNotFound)
}

func TestRepository_CreateAndListDrawerEvents_EnforcesOpenShiftAndScope(t *testing.T) {
	db := newOpenShiftTestDB(t)
	branch, staff, device := seedActiveOpenShiftResources(t, db, 7)
	openShift := seedShift(t, db, branch.ID, staff.ID, device.ID, ShiftStatusOpen)
	branchID := branch.ID
	scope := AccessScope{OrgID: 7, BranchID: &branchID}

	created, err := NewRepository(db).CreateDrawerEvent(context.Background(), scope, CreateDrawerEventRequest{
		ShiftID: openShift.ID, StaffID: staff.ID, Reason: "cash count",
	})
	require.NoError(t, err)
	require.NotZero(t, created.ID)
	require.False(t, created.CreatedAt.IsZero())

	events, err := NewRepository(db).ListDrawerEvents(context.Background(), scope)
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, created.ID, events[0].ID)

	closedShift := seedShift(t, db, branch.ID, staff.ID, device.ID, ShiftStatusClosed)
	got, err := NewRepository(db).CreateDrawerEvent(context.Background(), scope, CreateDrawerEventRequest{
		ShiftID: closedShift.ID, StaffID: staff.ID, Reason: "late event",
	})
	require.Nil(t, got)
	requireRestErrorStatus(t, err, http.StatusConflict)

	otherBranch := identity.Branch{OrgID: 7, Name: "Other", Status: identity.BranchStatusActive, Address: "-", Phone: "-"}
	require.NoError(t, db.Create(&otherBranch).Error)
	otherStaff := identity.Staff{BranchID: otherBranch.ID, Name: "Other", RoleID: 1, PinHash: "hash", Phone: "2", Status: identity.StaffStatusActive}
	require.NoError(t, db.Create(&otherStaff).Error)
	_, err = NewRepository(db).CreateDrawerEvent(context.Background(), scope, CreateDrawerEventRequest{
		ShiftID: openShift.ID, StaffID: otherStaff.ID, Reason: "wrong staff",
	})
	requireRestErrorStatus(t, err, http.StatusNotFound)
}

func TestRepository_CreateDrawerEvent_RoundTripsRealSaleUUID(t *testing.T) {
	db := newOpenShiftTestDB(t)
	branch, staff, device := seedActiveOpenShiftResources(t, db, 7)
	openShift := seedShift(t, db, branch.ID, staff.ID, device.ID, ShiftStatusOpen)
	sale := sales.Sale{
		ID: uuid.New(), OrgID: 7, BranchID: branch.ID, ShiftID: openShift.ID,
		StaffID: staff.ID, DeviceID: device.ID, Status: sales.SaleStatusCompleted,
	}
	require.NoError(t, db.Create(&sale).Error)
	branchID := branch.ID
	scope := AccessScope{OrgID: 7, BranchID: &branchID}

	created, err := NewRepository(db).CreateDrawerEvent(context.Background(), scope, CreateDrawerEventRequest{
		ShiftID: openShift.ID, StaffID: staff.ID, Reason: "drawer opened after sale", SaleID: &sale.ID,
	})

	require.NoError(t, err)
	require.NotNil(t, created.SaleID)
	require.Equal(t, sale.ID, *created.SaleID)

	var persisted DrawerEvent
	require.NoError(t, db.First(&persisted, created.ID).Error)
	require.NotNil(t, persisted.SaleID)
	require.Equal(t, sale.ID, *persisted.SaleID)
}

func TestRepository_ExpenseCRUD_EnforcesTenantBranchAndStaff(t *testing.T) {
	db := newOpenShiftTestDB(t)
	branch, staff, _ := seedActiveOpenShiftResources(t, db, 7)
	branchID := branch.ID
	scope := AccessScope{OrgID: 7, BranchID: &branchID}
	repo := NewRepository(db)

	created, err := repo.CreateExpense(context.Background(), scope, CreateExpenseRequest{
		BranchID: branch.ID, Date: time.Now().UTC(), Category: "supplies",
		Amount: decimal.RequireFromString("12.50"), CreatedBy: staff.ID,
	})
	require.NoError(t, err)
	require.NotZero(t, created.ID)

	listed, err := repo.ListExpenses(context.Background(), scope)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	require.Equal(t, created.ID, listed[0].ID)

	category := "transport"
	amount := decimal.NewFromInt(20)
	creator := ExpenseActor{StaffID: staff.ID}
	updated, err := repo.UpdateExpense(context.Background(), scope, created.ID, creator, UpdateExpenseRequest{
		Category: &category, Amount: &amount,
	})
	require.NoError(t, err)
	require.Equal(t, category, updated.Category)
	require.True(t, amount.Equal(updated.Amount))

	otherBranch := identity.Branch{OrgID: 7, Name: "Other", Status: identity.BranchStatusActive, Address: "-", Phone: "-"}
	require.NoError(t, db.Create(&otherBranch).Error)
	otherStaff := identity.Staff{BranchID: otherBranch.ID, Name: "Other", RoleID: 1, PinHash: "hash", Phone: "2", Status: identity.StaffStatusActive}
	require.NoError(t, db.Create(&otherStaff).Error)
	_, err = repo.CreateExpense(context.Background(), scope, CreateExpenseRequest{
		BranchID: otherBranch.ID, Date: time.Now(), Category: "hidden", Amount: decimal.NewFromInt(1), CreatedBy: otherStaff.ID,
	})
	requireRestErrorStatus(t, err, http.StatusNotFound)

	otherScope := AccessScope{OrgID: 99}
	_, err = repo.UpdateExpense(context.Background(), otherScope, created.ID, creator, UpdateExpenseRequest{Category: &category})
	requireRestErrorStatus(t, err, http.StatusNotFound)
	requireRestErrorStatus(t, repo.DeleteExpense(context.Background(), otherScope, created.ID, creator), http.StatusNotFound)

	require.NoError(t, repo.DeleteExpense(context.Background(), scope, created.ID, creator))
	var count int64
	require.NoError(t, db.Model(&Expense{}).Where("id = ?", created.ID).Count(&count).Error)
	require.Zero(t, count)
}

func TestRepository_UpdateAndDeleteExpense_RejectNonCreatorWithoutManagePermission(t *testing.T) {
	db := newOpenShiftTestDB(t)
	branch, staff, _ := seedActiveOpenShiftResources(t, db, 7)
	otherStaff := identity.Staff{BranchID: branch.ID, Name: "Other Cashier", RoleID: 1, PinHash: "hash", Phone: "2", Status: identity.StaffStatusActive}
	require.NoError(t, db.Create(&otherStaff).Error)
	branchID := branch.ID
	scope := AccessScope{OrgID: 7, BranchID: &branchID}
	repo := NewRepository(db)

	created, err := repo.CreateExpense(context.Background(), scope, CreateExpenseRequest{
		BranchID: branch.ID, Date: time.Now().UTC(), Category: "supplies", Amount: decimal.NewFromInt(10), CreatedBy: staff.ID,
	})
	require.NoError(t, err)

	otherCashier := ExpenseActor{StaffID: otherStaff.ID}
	category := "transport"
	_, err = repo.UpdateExpense(context.Background(), scope, created.ID, otherCashier, UpdateExpenseRequest{Category: &category})
	requireRestErrorStatus(t, err, http.StatusForbidden)
	requireRestErrorStatus(t, repo.DeleteExpense(context.Background(), scope, created.ID, otherCashier), http.StatusForbidden)

	manager := ExpenseActor{StaffID: otherStaff.ID, CanManageAny: true}
	updated, err := repo.UpdateExpense(context.Background(), scope, created.ID, manager, UpdateExpenseRequest{Category: &category})
	require.NoError(t, err)
	require.Equal(t, "transport", updated.Category)
	require.NoError(t, repo.DeleteExpense(context.Background(), scope, created.ID, manager))

	var count int64
	require.NoError(t, db.Model(&Expense{}).Where("id = ?", created.ID).Count(&count).Error)
	require.Zero(t, count)
}

func TestRepository_UpdateExpense_AllowsAtomicBranchAndCreatorMoveForOrgWideScope(t *testing.T) {
	db := newOpenShiftTestDB(t)
	branch, staff, _ := seedActiveOpenShiftResources(t, db, 7)
	repo := NewRepository(db)
	expense, err := repo.CreateExpense(context.Background(), AccessScope{OrgID: 7}, CreateExpenseRequest{
		BranchID: branch.ID, Date: time.Now(), Category: "supplies", Amount: decimal.NewFromInt(5), CreatedBy: staff.ID,
	})
	require.NoError(t, err)
	otherBranch := identity.Branch{OrgID: 7, Name: "Other", Status: identity.BranchStatusActive, Address: "-", Phone: "-"}
	require.NoError(t, db.Create(&otherBranch).Error)
	otherStaff := identity.Staff{BranchID: otherBranch.ID, Name: "Other", RoleID: 1, PinHash: "hash", Phone: "2", Status: identity.StaffStatusActive}
	require.NoError(t, db.Create(&otherStaff).Error)

	updated, err := repo.UpdateExpense(context.Background(), AccessScope{OrgID: 7}, expense.ID, ExpenseActor{StaffID: staff.ID}, UpdateExpenseRequest{
		BranchID: &otherBranch.ID, CreatedBy: &otherStaff.ID,
	})

	require.NoError(t, err)
	require.Equal(t, otherBranch.ID, updated.BranchID)
	require.Equal(t, otherStaff.ID, updated.CreatedBy)
}

func seedShift(t *testing.T, db *gorm.DB, branchID, staffID, deviceID uint, status ShiftStatus) *Shift {
	t.Helper()
	shift := &Shift{
		BranchID: branchID, StaffID: staffID, DeviceID: deviceID,
		OpenedAt: time.Now().UTC().Add(-time.Hour), OpeningCash: decimal.NewFromInt(100), Status: status,
	}
	if status == ShiftStatusClosed {
		closedAt := time.Now().UTC()
		shift.ClosedAt = &closedAt
	}
	require.NoError(t, db.Create(shift).Error)
	return shift
}
