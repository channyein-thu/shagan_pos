package shift

import (
	"shagan_pos/internal/common"
	"shagan_pos/internal/identity"
)

// Lifecycle policies contain no database operations. Call them against
// records read under the same transaction/locks used to persist the change.
func validateOpeningShift(branch identity.Branch, staff identity.Staff, device identity.Device, alreadyOpen bool) error {
	if branch.Status != identity.BranchStatusActive {
		return common.ConflictError("branch is not active")
	}
	if staff.Status != identity.StaffStatusActive {
		return common.ConflictError("staff is not active")
	}
	if device.Status != identity.DeviceStatusActive {
		return common.ConflictError("device is not active")
	}
	if alreadyOpen {
		return common.ConflictError("staff or device already has an open shift")
	}
	return nil
}

func validateShiftCloser(shift Shift, requireOpenerStaffID *uint) error {
	if shift.Status != ShiftStatusOpen {
		return common.ConflictError("shift is already closed")
	}
	if requireOpenerStaffID != nil && shift.StaffID != *requireOpenerStaffID {
		return common.ForbiddenError("only the staff member who opened this shift may close it")
	}
	return nil
}

func validateNoOpenSales(count int64) error {
	if count > 0 {
		return common.ConflictError("shift has open sales")
	}
	return nil
}

func validateCurrentShiftAccount(user identity.User) error {
	if user.AccountType != identity.AccountTypePos || user.BranchID == nil || user.DeviceID == nil {
		return common.ForbiddenError("current shift is only available to a paired POS account")
	}
	return nil
}
