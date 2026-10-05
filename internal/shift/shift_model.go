package shift

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// TODO: relationships (belongs-to/has-many) are intentionally omitted here;
// wire them up as needed in repository.go queries.
// ShiftStatus is a best-guess enum (ERD only specified "enum"; confirm real values).
type ShiftStatus string

const (
	ShiftStatusOpen   ShiftStatus = "open"
	ShiftStatusClosed ShiftStatus = "closed"
)

// ReconciliationMethod mirrors sales.PaymentMethod - cash and QR only, for
// this version.
type ReconciliationMethod string

const (
	ReconciliationMethodCash ReconciliationMethod = "cash"
	ReconciliationMethodQR   ReconciliationMethod = "qr"
)

// Shift maps to the "Shifts" table in the ERD.
type Shift struct {
	ID          uint            `gorm:"primaryKey;autoIncrement" json:"id"`
	BranchID    uint            `gorm:"index;not null" json:"branch_id"`
	StaffID     uint            `gorm:"index;not null" json:"staff_id"`
	DeviceID    uint            `gorm:"index;not null" json:"device_id"`
	OpenedAt    time.Time       `gorm:"not null" json:"opened_at"`
	OpeningCash decimal.Decimal `gorm:"type:decimal(10,2);not null" json:"opening_cash"`
	ClosedAt    *time.Time      `json:"closed_at"`
	// ClosedByStaffID records who actually performed the close - equal to
	// StaffID for a normal close (only the shift's own staff may call
	// CloseShift), or a different staff's ID for a manager's
	// ForceCloseShift override (e.g. the original staff is unexpectedly
	// unavailable). Comparing the two after the fact is how a force-close
	// shows up distinctly, without a separate boolean flag.
	ClosedByStaffID *uint       `gorm:"index" json:"closed_by_staff_id"`
	Status          ShiftStatus `gorm:"type:varchar(30);not null" json:"status"` // one of ShiftStatus* constants below (TODO: confirm real values)
}

// ShiftReconciliation maps to the "Shifts_reconciliations" table in the ERD.
type ShiftReconciliation struct {
	ID         uint                 `gorm:"primaryKey;autoIncrement" json:"id"`
	ShiftID    uint                 `gorm:"index;not null" json:"shift_id"`
	Method     ReconciliationMethod `gorm:"type:varchar(30);not null" json:"method"` // one of ReconciliationMethod* constants below (TODO: confirm real values)
	Expected   decimal.Decimal      `gorm:"type:decimal(10,2);not null" json:"expected"`
	Counted    decimal.Decimal      `gorm:"type:decimal(10,2);not null" json:"counted"`
	Difference decimal.Decimal      `gorm:"type:decimal(10,2);not null" json:"difference"`
	Reason     string               `gorm:"not null" json:"reason"`
}

// DrawerEvent maps to the "drawer_events" table in the ERD.
type DrawerEvent struct {
	ID        uint       `gorm:"primaryKey;autoIncrement" json:"id"`
	ShiftID   uint       `gorm:"index;not null" json:"shift_id"`
	StaffID   uint       `gorm:"index;not null" json:"staff_id"`
	Reason    string     `gorm:"not null" json:"reason"`
	SaleID    *uuid.UUID `gorm:"type:uuid;index" json:"sale_id"`
	CreatedAt time.Time  `gorm:"autoCreateTime;not null" json:"created_at"`
}

// Expense maps to the "expenses" table in the ERD.
type Expense struct {
	ID        uint            `gorm:"primaryKey;autoIncrement" json:"id"`
	BranchID  uint            `gorm:"index;not null" json:"branch_id"`
	Date      time.Time       `gorm:"type:date;not null" json:"date"`
	Category  string          `gorm:"size:255;not null" json:"category"`
	Amount    decimal.Decimal `gorm:"type:decimal(10,2);not null" json:"amount"`
	CreatedBy uint            `gorm:"index;not null" json:"created_by"`
}

func (ShiftReconciliation) TableName() string { return "shifts_reconciliations" }
