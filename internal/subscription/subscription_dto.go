package subscription

import (
	"bytes"
	"encoding/json"
	"time"
)

// NullableTime tells an omitted JSON field from an explicit null: Set is true
// whenever the key was present, and Time is nil for null - so {"expires_at":
// null} clears the end date while leaving the key out keeps it.
type NullableTime struct {
	Set  bool
	Time *time.Time
}

func (n *NullableTime) UnmarshalJSON(b []byte) error {
	n.Set = true
	if bytes.Equal(bytes.TrimSpace(b), []byte("null")) {
		n.Time = nil
		return nil
	}
	var t time.Time
	if err := json.Unmarshal(b, &t); err != nil {
		return err
	}
	n.Time = &t
	return nil
}

// UpdateSubscriptionRequest is the body of `PATCH /internal/organizations/:id/subscription`.
// Every plan field is optional - an omitted one is left as it is (expires_at:
// null clears the end date). ChangedBy (the shagan_team admin's email) is
// required: the internal API has no user, so the portal says who it was. Note
// is free text (max 500) kept with the history row.
type UpdateSubscriptionRequest struct {
	Plan          *string      `json:"plan"`
	ExpiresAt     NullableTime `json:"expires_at"`
	Trial         *bool        `json:"trial"`
	ExtraBranches *int         `json:"extra_branches"`
	ExtraDevices  *int         `json:"extra_devices"`
	Note          string       `json:"note"`
	ChangedBy     string       `json:"changed_by"`
}

// FeatureOverrideRequest is the body of `PUT /internal/organizations/:id/features/:code`
// (Enabled required) and `DELETE` (only ChangedBy / Note are read; ChangedBy may
// instead come as a ?changed_by= query param, since a DELETE often has no body).
type FeatureOverrideRequest struct {
	Enabled   *bool  `json:"enabled"`
	Note      string `json:"note"`
	ChangedBy string `json:"changed_by"`
}

// PlanCatalog is `GET /internal/plans`: the tiers and every feature code (what
// an override may name).
type PlanCatalog struct {
	Plans    []Plan   `json:"plans"`
	Features []string `json:"features"`
}

// Detail is `GET /internal/organizations/:id/subscription` (and what each
// write returns): the effective entitlements with usage, the stored plan
// fields, the feature overrides and the change history (newest first, at most
// HistoryLimit).
type Detail struct {
	OrganizationID uint                 `json:"organization_id"`
	Effective      Summary              `json:"effective"`
	Raw            OrgPlan              `json:"raw"`
	OverLimit      bool                 `json:"over_limit"`
	Overrides      []OrgFeatureOverride `json:"overrides"`
	History        []OrgPlanChange      `json:"history"`
}

// HistoryLimit caps how many history rows a Detail carries.
const HistoryLimit = 100
