package identity

import (
	"time"
)

// TODO: relationships (belongs-to/has-many) are intentionally omitted here;
// wire them up as needed in repository.go queries.
// BranchStatus is a best-guess enum (ERD only specified "enum"; confirm real values).
type BranchStatus string

const (
	BranchStatusActive   BranchStatus = "active"
	BranchStatusInactive BranchStatus = "inactive"
)

// DeviceStatus is a best-guess enum (ERD only specified "enum"; confirm real values).
type DeviceStatus string

const (
	DeviceStatusActive   DeviceStatus = "active"
	DeviceStatusInactive DeviceStatus = "inactive"
	DeviceStatusRevoked  DeviceStatus = "revoked"
)

// AccountType is a best-guess enum (ERD only specified "enum"; confirm real values).
type AccountType string

const (
	AccountTypeOwner         AccountType = "owner"
	AccountTypePos           AccountType = "pos"
	AccountTypeServiceCenter AccountType = "service_center"
)

// OrganizationStatus gates whether a whole tenant can operate - a
// Shagan-team-only lever (see CreateAccount/UpdateOrganizationStatus), not
// something a tenant's own owner can set.
type OrganizationStatus string

const (
	OrganizationStatusActive    OrganizationStatus = "active"
	OrganizationStatusSuspended OrganizationStatus = "suspended"
)

// UserStatus gates whether a single login (most commonly a pos-type
// terminal credential) can authenticate at all - checked in Login/RefreshSession,
// separate from Device.Status (a device can be fine while its login is
// suspended, e.g. after a password reset, or vice versa).
type UserStatus string

const (
	UserStatusActive    UserStatus = "active"
	UserStatusSuspended UserStatus = "suspended"
)

// StaffStatus is a best-guess enum (ERD only specified "enum"; confirm real values).
type StaffStatus string

const (
	StaffStatusActive    StaffStatus = "active"
	StaffStatusInactive  StaffStatus = "inactive"
	StaffStatusSuspended StaffStatus = "suspended"
)

// Organization maps to the "Organizations" table in the ERD.
type Organization struct {
	ID   uint   `gorm:"primaryKey;autoIncrement" json:"id"`
	Name string `gorm:"size:255;not null" json:"name"`
	// Status defaults to active for every existing and new row (the
	// `default` tag applies at the DB level via AutoMigrate, so this never
	// needs setting explicitly in CreateAccount - see OrganizationStatus).
	Status OrganizationStatus `gorm:"type:varchar(30);not null;default:'active'" json:"status"`
	// Timezone is the IANA zone ("Asia/Yangon") that defines this org's
	// calendar day - what "today" means on the dashboard and where a report's
	// from/to dates and daily buckets fall. Defaults to Asia/Yangon for every
	// existing and new row (applied at the DB level by AutoMigrate, same as
	// Status). Validated on write - see common.LoadTimezone.
	Timezone string `gorm:"size:64;not null;default:'Asia/Yangon'" json:"timezone"`
	// The subscription block - see internal/subscription. Plan is a plan code
	// (basic | pro | enterprise); PlanExpiresAt nil = no end date; PlanTrial marks
	// the 14-day Pro trial a new org starts on. ExtraBranches / ExtraDevices are
	// per-org add-ons on top of the plan's limits. These are the STORED fields:
	// what an org may do right now (after expiry and grace) is
	// subscription.Resolve. default 'basic' / false / 0 lets AutoMigrate add the
	// columns to a table with rows - every existing org is Basic, no trial.
	Plan          string     `gorm:"size:30;not null;default:'basic'" json:"plan"`
	PlanExpiresAt *time.Time `json:"plan_expires_at"`
	PlanTrial     bool       `gorm:"not null;default:false" json:"plan_trial"`
	ExtraBranches int        `gorm:"not null;default:0" json:"extra_branches"`
	ExtraDevices  int        `gorm:"not null;default:0" json:"extra_devices"`
	CreatedAt     time.Time  `gorm:"autoCreateTime;not null" json:"created_at"`
	UpdatedAt     time.Time  `gorm:"autoUpdateTime;not null" json:"updated_at"`
}

// Branch maps to the "Branches" table in the ERD.
type Branch struct {
	ID        uint         `gorm:"primaryKey;autoIncrement" json:"id"`
	OrgID     uint         `gorm:"index;not null" json:"org_id"`
	Name      string       `gorm:"size:255;not null" json:"name"`
	Status    BranchStatus `gorm:"type:varchar(30);not null" json:"status"` // one of BranchStatus* constants below (TODO: confirm real values)
	Address   string       `gorm:"size:500;not null" json:"address"`
	Phone     string       `gorm:"size:50;not null" json:"phone"`
	CreatedAt time.Time    `gorm:"autoCreateTime;not null" json:"created_at"`
	UpdatedAt time.Time    `gorm:"autoUpdateTime;not null" json:"updated_at"`
}

// Device maps to the "Devices" table in the ERD.
type Device struct {
	ID         uint         `gorm:"primaryKey;autoIncrement" json:"id"`
	BranchID   uint         `gorm:"index;not null" json:"branch_id"`
	Name       string       `gorm:"size:255;not null" json:"name"`
	Status     DeviceStatus `gorm:"type:varchar(30);not null" json:"status"` // one of DeviceStatus* constants below (TODO: confirm real values)
	LastSeenAt time.Time    `gorm:"not null" json:"last_seen_at"`
	CreatedAt  time.Time    `gorm:"autoCreateTime;not null" json:"created_at"`
	UpdatedAt  time.Time    `gorm:"autoUpdateTime;not null" json:"updated_at"`
}

// User maps to the "Users" table in the ERD.
type User struct {
	ID          uint        `gorm:"primaryKey;autoIncrement" json:"id"`
	OrgID       uint        `gorm:"index;not null" json:"org_id"`
	Name        *string     `gorm:"size:255" json:"name"`
	AccountType AccountType `gorm:"type:varchar(30);not null" json:"account_type"` // one of AccountType* constants below (TODO: confirm real values)
	DeviceID    *uint       `gorm:"index" json:"device_id"`
	// BranchID is only ever set for AccountTypePos - owner/service_center are
	// org-wide, not tied to one branch. It's derived server-side from the
	// device's own branch at CreatePosAccount time (see RepositoryImpl.CreatePosAccount),
	// never accepted as client input, and gets carried into the access token's
	// claims on Login/RefreshSession so a pos terminal's requests can be
	// branch-scoped without an extra Device lookup.
	BranchID       *uint   `gorm:"index" json:"branch_id"`
	Email          *string `gorm:"size:255;uniqueIndex" json:"email"`
	CredentialHash string  `gorm:"size:255;not null" json:"-"`
	// Status defaults to active for every existing and new row - see
	// UserStatus. Checked in Login/RefreshSession; never settable by the
	// account itself, only via the internal UpdatePosAccountStatus endpoint.
	Status    UserStatus `gorm:"type:varchar(30);not null;default:'active'" json:"status"`
	CreatedAt time.Time  `gorm:"autoCreateTime;not null" json:"created_at"`
	UpdatedAt time.Time  `gorm:"autoUpdateTime;not null" json:"updated_at"`
}

// Session maps to the "Sessions" table in the ERD.
type Session struct {
	ID          uint       `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID      uint       `gorm:"index;not null" json:"user_id"`
	RefreshHash string     `gorm:"size:255;not null" json:"refresh_hash"`
	ExpiresAt   time.Time  `gorm:"not null" json:"expires_at"`
	RevokedAt   *time.Time `json:"revoked_at"`
}

// Staff maps to the "Staffs" table in the ERD.
type Staff struct {
	ID       uint        `gorm:"primaryKey;autoIncrement" json:"id"`
	BranchID uint        `gorm:"index;not null" json:"branch_id"`
	Name     string      `gorm:"size:255;not null" json:"name"`
	RoleID   uint        `gorm:"column:role;index;not null" json:"role"`
	PinHash  string      `gorm:"size:255;not null" json:"-"`
	Phone    string      `gorm:"size:25;not null" json:"phone"`
	Status   StaffStatus `gorm:"type:varchar(30);not null" json:"status"` // one of StaffStatus* constants below (TODO: confirm real values)
	// FailedPinAttempts/PinLockedUntil back PIN brute-force lockout - shared
	// between VerifyStaffPIN and VerifyManagerPIN since both check the same
	// PinHash (see identity.Service.VerifyStaffPIN). Internal bookkeeping,
	// never serialized to the client.
	FailedPinAttempts int        `gorm:"not null;default:0" json:"-"`
	PinLockedUntil    *time.Time `json:"-"`
	CreatedAt         time.Time  `gorm:"autoCreateTime;not null" json:"created_at"`
	UpdatedAt         time.Time  `gorm:"autoUpdateTime;not null" json:"updated_at"`
}

// Permission maps to the "Permissions" table in the ERD.
type Permission struct {
	ID       uint   `gorm:"primaryKey;autoIncrement" json:"id"`
	Code     string `gorm:"size:50;not null;uniqueIndex" json:"code"`
	Name     string `gorm:"size:255;not null" json:"name"`
	Category string `gorm:"size:100;not null" json:"category"`
}

// RolePermission maps to the "Role_Permission" table in the ERD.
type RolePermission struct {
	ID           uint `gorm:"primaryKey;autoIncrement" json:"id"`
	PermissionID uint `gorm:"index;not null" json:"permission_id"`
	RoleID       uint `gorm:"index;not null" json:"role_id"`
}

// Role maps to the "Roles" table in the ERD. A role belongs to one
// organization (OrgID set); a row with a nil OrgID is a global template, and
// the three legacy rows (staff, super_staff, manager) are kept that way only
// as the source MigrateAccessRights copies from - nothing reads them at
// runtime. Code is unique within an org.
type Role struct {
	ID    uint   `gorm:"primaryKey;autoIncrement" json:"id"`
	OrgID *uint  `gorm:"uniqueIndex:idx_roles_org_code" json:"org_id"`
	Code  string `gorm:"size:50;not null;uniqueIndex:idx_roles_org_code" json:"code"`
	Name  string `gorm:"size:255;not null" json:"name"`
	// IsSystem marks Manager and Cashier: they can be renamed and re-granted
	// but never deleted, so every org always has a till role and a manager.
	IsSystem bool `gorm:"not null;default:false" json:"is_system"`
	// default:CURRENT_TIMESTAMP so a raw INSERT (db/demo_seed.sql) that
	// doesn't know this column still works.
	UpdatedAt time.Time `gorm:"autoUpdateTime;not null;default:CURRENT_TIMESTAMP" json:"updated_at"`
}

// OrgRolePermission is one grant: orgID's roleID holds permissionID. Grants
// are per org (the older global RolePermission table is only the migration
// source), so two orgs can edit their Manager role independently.
type OrgRolePermission struct {
	ID           uint `gorm:"primaryKey;autoIncrement" json:"id"`
	OrgID        uint `gorm:"not null;uniqueIndex:idx_org_role_permission" json:"org_id"`
	RoleID       uint `gorm:"not null;uniqueIndex:idx_org_role_permission;index" json:"role_id"`
	PermissionID uint `gorm:"not null;uniqueIndex:idx_org_role_permission" json:"permission_id"`
}

func (Staff) TableName() string { return "staffs" }

func (RolePermission) TableName() string { return "role_permission" }

func (OrgRolePermission) TableName() string { return "org_role_permission" }
