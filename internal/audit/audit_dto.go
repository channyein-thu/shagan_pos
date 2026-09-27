package audit

import (
	"encoding/json"

	"gorm.io/datatypes"
)

// No request DTOs: audit_log is read-only from the API (written via
// mutation hooks in other domains' own service methods, through
// AuditWriter - see e.g. sales.AuditWriter/returns.AuditWriter/
// identity.AuditWriter), so it has no Create/Update endpoint to bind.

// ToJSON marshals v into the datatypes.JSON shape AuditLog.Before/After
// expect - a nil v marshals to the JSON literal null, which is what an
// audit entry for a Create-shaped action (nothing existed "before") or
// unused After should carry. Panics only if v itself is unmarshalable
// (a struct with a channel/func field, for example) - every caller passes
// a plain data struct, so this should never happen in practice.
func ToJSON(v any) datatypes.JSON {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return datatypes.JSON(b)
}
