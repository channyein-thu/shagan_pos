package subscription

import (
	"fmt"
	"net/http"

	"shagan_pos/internal/common"
)

// Machine-readable error codes the apps branch on.
const (
	CodePlanLimitReached = "plan_limit_reached"
	CodeFeatureNotInPlan = "feature_not_in_plan"
)

// LimitKind names a plan limit.
type LimitKind string

const (
	LimitBranches LimitKind = "branches"
	LimitDevices  LimitKind = "devices"
)

// LimitReachedError is the 403 for adding or reactivating a branch or device
// past the plan's limit:
// {"success":false,"code":"plan_limit_reached","limit":"branches","max":1,"current":1,"message":"..."}.
func LimitReachedError(kind LimitKind, max, current int) common.RestError {
	return common.CodedError(http.StatusForbidden, CodePlanLimitReached,
		fmt.Sprintf("your plan allows %d active %s and %d are active - ask Shagan to raise your plan or add more", max, kind, current),
		map[string]any{"limit": string(kind), "max": max, "current": current})
}

// FeatureNotInPlanError is the 403 RequireFeature sends:
// {"success":false,"code":"feature_not_in_plan","feature":"promotions","plan":"basic","message":"..."}.
// plan is the EFFECTIVE plan.
func FeatureNotInPlanError(feature, plan string) common.RestError {
	return common.CodedError(http.StatusForbidden, CodeFeatureNotInPlan,
		fmt.Sprintf("%s is not included in your %s plan", feature, plan),
		map[string]any{"feature": feature, "plan": plan})
}
