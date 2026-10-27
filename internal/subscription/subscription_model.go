package subscription

import "time"

// OrgFeatureOverride switches one feature on or off for one organization,
// whatever its plan says - a deal or a beta. Unique per (org, feature); removing
// the row puts the feature back to the plan's default.
type OrgFeatureOverride struct {
	ID        uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	OrgID     uint      `gorm:"not null;uniqueIndex:ux_org_feature" json:"org_id"`
	Feature   string    `gorm:"size:50;not null;uniqueIndex:ux_org_feature" json:"feature"`
	Enabled   bool      `gorm:"not null" json:"enabled"`
	UpdatedAt time.Time `gorm:"autoUpdateTime;not null" json:"updated_at"`
}

// OrgPlanChange is the append-only history of an organization's subscription,
// shown in shagan_team. Every subscription write adds one - a plan or extras
// change, a feature override set or cleared (the plan fields then repeat the
// unchanged values and Note says what happened) - and none is ever edited or
// deleted. ChangedBy is the shagan_team admin's email: the internal API has no
// user of its own, so the portal sends it.
type OrgPlanChange struct {
	ID            uint       `gorm:"primaryKey;autoIncrement" json:"id"`
	OrgID         uint       `gorm:"not null;index" json:"org_id"`
	FromPlan      string     `gorm:"size:30;not null" json:"from_plan"`
	ToPlan        string     `gorm:"size:30;not null" json:"to_plan"`
	ExpiresAt     *time.Time `json:"expires_at"`
	Trial         bool       `gorm:"not null" json:"trial"`
	ExtraBranches int        `gorm:"not null" json:"extra_branches"`
	ExtraDevices  int        `gorm:"not null" json:"extra_devices"`
	Note          string     `gorm:"size:500" json:"note"`
	ChangedBy     string     `gorm:"size:255;not null" json:"changed_by"`
	CreatedAt     time.Time  `gorm:"autoCreateTime;not null" json:"created_at"`
}

// OrgPlan is the plan block stored on the organizations row (owned by the
// identity domain's Organization model): the raw fields the resolver starts
// from.
type OrgPlan struct {
	Plan          string     `json:"plan"`
	ExpiresAt     *time.Time `json:"expires_at"`
	Trial         bool       `json:"trial"`
	ExtraBranches int        `json:"extra_branches"`
	ExtraDevices  int        `json:"extra_devices"`
}

// Usage is how many ACTIVE branches and devices an organization has.
type Usage struct {
	Branches int `json:"branches"`
	Devices  int `json:"devices"`
}
