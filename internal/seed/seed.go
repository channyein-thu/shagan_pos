package seed

import (
	"gorm.io/gorm"

	"shagan_pos/internal/identity"
)

// Run seeds baseline reference data - the fixed v1 roles, permissions, and
// their grants (global and fixed for v1, not per-org customizable - see
// identity.ListRoles) - for local development and any fresh environment.
// Idempotent: each role/permission is matched by its own unique Code and
// each grant by its role/permission pair, so running this against a
// database that already has the data is a safe no-op rather than an error
// or a duplicate insert.
func Run(db *gorm.DB) error {
	roleRows := []identity.Role{
		{Code: "staff", Name: "Staff"},
		{Code: "super_staff", Name: "Super Staff"},
		{Code: "manager", Name: "Manager"},
	}
	roles := make(map[string]identity.Role, len(roleRows))
	for _, r := range roleRows {
		if err := db.Where(identity.Role{Code: r.Code}).FirstOrCreate(&r).Error; err != nil {
			return err
		}
		roles[r.Code] = r
	}

	permissionRows := []identity.Permission{
		{Code: "access_pos_portal", Name: "Access the POS selling portal", Category: "portal"},
		{Code: "access_backoffice", Name: "Access the backoffice (own branch only)", Category: "portal"},
		{Code: "open_drawer_no_sale", Name: "Open the cash drawer without an active sale", Category: "cash"},
		{Code: "apply_manual_discount", Name: "Apply a manual discount at time of sale", Category: "sales"},
		{Code: "approve_void", Name: "Approve a void", Category: "sales"},
		{Code: "approve_return", Name: "Approve a return", Category: "sales"},
	}
	permissions := make(map[string]identity.Permission, len(permissionRows))
	for _, p := range permissionRows {
		if err := db.Where(identity.Permission{Code: p.Code}).FirstOrCreate(&p).Error; err != nil {
			return err
		}
		permissions[p.Code] = p
	}

	// grants is the v1 role -> permission matrix. staff gets POS access only;
	// super_staff additionally covers till operations and discounts;
	// manager additionally covers backoffice access and void/return approval.
	grants := map[string][]string{
		"staff":       {"access_pos_portal"},
		"super_staff": {"access_pos_portal", "open_drawer_no_sale", "apply_manual_discount"},
		"manager":     {"access_pos_portal", "access_backoffice", "apply_manual_discount", "approve_void", "approve_return"},
	}
	for roleCode, permCodes := range grants {
		role := roles[roleCode]
		for _, permCode := range permCodes {
			perm := permissions[permCode]
			rp := identity.RolePermission{RoleID: role.ID, PermissionID: perm.ID}
			if err := db.Where(rp).FirstOrCreate(&rp).Error; err != nil {
				return err
			}
		}
	}

	return nil
}
