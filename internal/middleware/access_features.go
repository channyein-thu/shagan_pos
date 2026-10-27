package middleware

import (
	"context"
	"time"

	"github.com/gin-gonic/gin"

	"shagan_pos/internal/common"
	"shagan_pos/internal/subscription"
)

// FeatureChecker is what RequireFeature needs: an organization's current
// entitlements. subscription.Service satisfies it (cached ~60 s per org, so the
// check is cheap on every request). The plan is looked up per request, never
// read from the JWT: staff tokens live 12 h and device refresh tokens 30 days.
type FeatureChecker interface {
	Entitlements(ctx context.Context, orgID uint, now time.Time) (*subscription.Entitlements, error)
}

// RequireFeature allows the request only if the caller's organization's
// current plan (after expiry, grace and per-org overrides) includes the
// feature; otherwise 403
// {"success":false,"code":"feature_not_in_plan","feature":...,"plan":...,"message":...}.
// Stack it after Auth. It is added to each Pro / Enterprise route as that
// feature is built - nothing built today is gated, and it must never be put on
// login, selling (POST /sales, /sync/sales), shifts, receipts or returns: a
// downgrade must not stop a till.
func RequireFeature(checker FeatureChecker, feature string) gin.HandlerFunc {
	return func(c *gin.Context) {
		orgID, ok := OrgIDFromContext(c)
		if !ok {
			common.HandleError(c, common.UnauthorizedError("missing or malformed authorization header"))
			c.Abort()
			return
		}
		ent, err := checker.Entitlements(c.Request.Context(), orgID, time.Now())
		if err != nil {
			common.HandleError(c, err)
			c.Abort()
			return
		}
		if !ent.Has(feature) {
			common.HandleError(c, subscription.FeatureNotInPlanError(feature, ent.Plan))
			c.Abort()
			return
		}
		c.Next()
	}
}
