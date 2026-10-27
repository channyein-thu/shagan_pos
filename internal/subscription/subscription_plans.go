// Package subscription is the organization's plan (Basic / Pro / Enterprise):
// what each tier includes, the one resolver that turns an org's stored plan
// fields into what it may do right now, and the limit and feature checks the
// rest of the backend enforces. The plan belongs to the ORGANIZATION - every
// login of the org (owner, service center, POS devices, staff) gets it - and is
// separate from Organization.Status: a plan never suspends an org or stops a
// till from selling, it only decides features and how many branches / devices
// can be added.
package subscription

import "time"

// Plan codes.
const (
	PlanBasic      = "basic"
	PlanPro        = "pro"
	PlanEnterprise = "enterprise"
)

// Feature codes - what a plan can include. None of the features below exists
// in the product yet (everything built today is in every tier); each one gets
// middleware.RequireFeature on its routes when it is built.
const (
	FeatureCustomerDisplay = "customer_display"
	FeatureAdvancedReports = "advanced_reports"
	FeatureReportExport    = "report_export"
	FeaturePromotions      = "promotions"
	FeatureLoyalty         = "loyalty"
	FeatureSMSReceipts     = "sms_receipts"
	FeatureAPIAccess       = "api_access"
)

// allFeatures is every feature code, in the order the apps show them.
var allFeatures = []string{
	FeatureCustomerDisplay, FeatureAdvancedReports, FeatureReportExport, FeaturePromotions,
	FeatureLoyalty, FeatureSMSReceipts, FeatureAPIAccess,
}

const (
	// GracePeriod is how long a PAID plan keeps working after plan_expires_at
	// before the org drops to Basic. A trial has no grace period.
	GracePeriod = 7 * 24 * time.Hour
	// TrialPlan and TrialDuration are what a new organization starts on.
	TrialPlan     = PlanPro
	TrialDuration = 14 * 24 * time.Hour
)

// Limits are a plan's caps on ACTIVE branches and devices. A nil limit is
// unlimited (JSON null).
type Limits struct {
	Branches *int `json:"branches"`
	Devices  *int `json:"devices"`
}

// Plan is one tier of the catalogue.
type Plan struct {
	Code     string   `json:"code"`
	Name     string   `json:"name"`
	Features []string `json:"features"`
	Limits   Limits   `json:"limits"`
}

func intp(n int) *int { return &n }

var proFeatures = []string{FeatureCustomerDisplay, FeatureAdvancedReports, FeatureReportExport, FeaturePromotions}

// plans is the catalogue, lowest tier first. Defined in code: the features are
// code anyway and there are only three tiers.
var plans = []Plan{
	{Code: PlanBasic, Name: "Basic", Features: []string{}, Limits: Limits{Branches: intp(1), Devices: intp(2)}},
	{Code: PlanPro, Name: "Pro", Features: proFeatures, Limits: Limits{Branches: intp(5), Devices: intp(10)}},
	{Code: PlanEnterprise, Name: "Enterprise",
		Features: append(append([]string{}, proFeatures...), FeatureLoyalty, FeatureSMSReceipts, FeatureAPIAccess),
		Limits:   Limits{}},
}

// Plans returns the catalogue (a copy), lowest tier first.
func Plans() []Plan {
	out := make([]Plan, len(plans))
	for i, p := range plans {
		p.Features = append([]string{}, p.Features...)
		out[i] = p
	}
	return out
}

// LookupPlan returns the plan with that code.
func LookupPlan(code string) (Plan, bool) {
	for _, p := range plans {
		if p.Code == code {
			p.Features = append([]string{}, p.Features...)
			return p, true
		}
	}
	return Plan{}, false
}

// AllFeatures lists every feature code, in display order.
func AllFeatures() []string { return append([]string{}, allFeatures...) }

// IsFeature reports whether code is a known feature.
func IsFeature(code string) bool {
	for _, f := range allFeatures {
		if f == code {
			return true
		}
	}
	return false
}
