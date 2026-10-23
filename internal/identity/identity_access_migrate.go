package identity

import (
	"fmt"
	"slices"

	"gorm.io/gorm"

	"shagan_pos/internal/common"
)

// EnsurePermissionCatalogue makes the permissions table match
// PermissionCatalogue: any missing code is inserted and every existing code
// gets the catalogue's current name and category (the seven legacy rows were
// created with different names and categories). Rows for codes outside the
// catalogue are left alone. Idempotent - run on every start and by the seed.
func EnsurePermissionCatalogue(db *gorm.DB) error {
	for _, def := range PermissionCatalogue {
		p := Permission{Code: def.Code}
		if err := db.Where("code = ?", def.Code).
			Assign(Permission{Name: def.Name, Category: def.Category}).
			FirstOrCreate(&p).Error; err != nil {
			return fmt.Errorf("ensure permission %s: %w", def.Code, err)
		}
	}
	return nil
}

// permissionIDsByCode maps every permission code in the table to its id.
func permissionIDsByCode(db *gorm.DB) (map[string]uint, error) {
	var perms []Permission
	if err := db.Find(&perms).Error; err != nil {
		return nil, err
	}
	ids := make(map[string]uint, len(perms))
	for _, p := range perms {
		ids[p.Code] = p.ID
	}
	return ids, nil
}

// createOrgRole inserts a role for orgID and its grants. grantCodes that
// aren't in permIDs are an error (the catalogue must be ensured first).
func createOrgRole(db *gorm.DB, orgID uint, code, name string, system bool, grantCodes []string, permIDs map[string]uint) (*Role, error) {
	role := Role{OrgID: &orgID, Code: code, Name: name, IsSystem: system}
	if err := db.Create(&role).Error; err != nil {
		return nil, err
	}
	if err := insertGrants(db, orgID, role.ID, grantCodes, permIDs); err != nil {
		return nil, err
	}
	return &role, nil
}

func insertGrants(db *gorm.DB, orgID, roleID uint, grantCodes []string, permIDs map[string]uint) error {
	grants := make([]OrgRolePermission, 0, len(grantCodes))
	for _, code := range grantCodes {
		id, ok := permIDs[code]
		if !ok {
			return fmt.Errorf("permission %q is not in the permissions table", code)
		}
		grants = append(grants, OrgRolePermission{OrgID: orgID, RoleID: roleID, PermissionID: id})
	}
	if len(grants) == 0 {
		return nil
	}
	return db.Create(&grants).Error
}

// ProvisionDefaultRoles gives a brand-new org its Manager and Cashier roles
// with the catalogue defaults. db is normally the transaction CreateAccount
// is running in, so a failure leaves no half-provisioned org behind.
func ProvisionDefaultRoles(db *gorm.DB, orgID uint) error {
	permIDs, err := permissionIDsByCode(db)
	if err != nil {
		return err
	}
	for _, def := range DefaultRoles {
		if !hasAllPermissions(permIDs, def.Grants) {
			// the seed/migration hasn't run against this database yet
			if err := EnsurePermissionCatalogue(db); err != nil {
				return err
			}
			if permIDs, err = permissionIDsByCode(db); err != nil {
				return err
			}
			break
		}
	}
	for _, def := range DefaultRoles {
		if _, err := createOrgRole(db, orgID, def.Code, def.Name, def.System, def.Grants, permIDs); err != nil {
			return err
		}
	}
	return nil
}

func hasAllPermissions(permIDs map[string]uint, codes []string) bool {
	for _, c := range codes {
		if _, ok := permIDs[c]; !ok {
			return false
		}
	}
	return true
}

// MigrateAccessRights moves a database from global roles to per-org roles,
// and keeps a current one's permission catalogue complete. Idempotent: run on
// every start (migrate.Run) and by the seed.
//
// For each organization that has no roles of its own yet, in one transaction:
//  1. create its Manager and Cashier roles (system), and a custom "Super
//     Staff" role if any of its staff used the old global super_staff;
//  2. copy the grants - the legacy permissions come from the legacy role's own
//     grants verbatim (so nobody gains or loses anything they could do), and
//     the permissions that didn't exist before come from the catalogue
//     defaults;
//  3. repoint every staff member's role at the org's own role (old `staff`
//     becomes `cashier`).
//
// A legacy role that isn't one of the three seeded ones is cloned as a custom
// role with its own grants, never dropped. The legacy global rows are left in
// place as the migration source. An org that already has roles is untouched,
// so an Owner's later edits survive every restart.
func MigrateAccessRights(db *gorm.DB) error {
	if err := dropLegacyRoleCodeIndex(db); err != nil {
		return err
	}
	if err := EnsurePermissionCatalogue(db); err != nil {
		return err
	}
	var orgIDs []uint
	if err := db.Model(&Organization{}).Order("id").Pluck("id", &orgIDs).Error; err != nil {
		return err
	}
	for _, orgID := range orgIDs {
		orgID := orgID
		if err := db.Transaction(func(tx *gorm.DB) error { return migrateOrgRoles(tx, orgID) }); err != nil {
			return fmt.Errorf("migrate roles of org %d: %w", orgID, err)
		}
	}
	return common.RunOnce(db, migrationCashierMayAskForDiscounts, grantCashierApplyManualDiscount)
}

const migrationCashierMayAskForDiscounts = "2026-10-23-cashier-apply-manual-discount"

// grantCashierApplyManualDiscount gives every org's built-in Cashier role
// apply_manual_discount ("may ask for a custom discount", Discount V1), once.
// Custom roles - which the Owner shaped on purpose - are not touched, and a
// Cashier that already has it is skipped.
func grantCashierApplyManualDiscount(tx *gorm.DB) error {
	return tx.Exec(`INSERT INTO org_role_permission (org_id, role_id, permission_id)
		SELECT r.org_id, r.id, p.id FROM roles r, permissions p
		WHERE r.org_id IS NOT NULL AND r.code = ? AND r.is_system = ? AND p.code = ?
		AND NOT EXISTS (SELECT 1 FROM org_role_permission x WHERE x.role_id = r.id AND x.permission_id = p.id)`,
		RoleCodeCashier, true, PermApplyManualDiscount).Error
}

// dropLegacyRoleCodeIndex removes the old unique index on roles.code alone:
// two orgs must each be able to have a role coded `manager`. AutoMigrate
// creates the new (org_id, code) index but never drops the old one.
func dropLegacyRoleCodeIndex(db *gorm.DB) error {
	// Plain SQL, not Migrator().DropIndex: that one emits invalid SQL
	// ("DROP INDEX CURRENT_SCHEMA()...") on the Postgres driver. IF EXISTS
	// makes it a no-op on a fresh database and on every later start.
	return db.Exec("DROP INDEX IF EXISTS idx_roles_code").Error
}

func migrateOrgRoles(tx *gorm.DB, orgID uint) error {
	var existing int64
	if err := tx.Model(&Role{}).Where("org_id = ?", orgID).Count(&existing).Error; err != nil {
		return err
	}
	if existing > 0 {
		return nil
	}

	permIDs, err := permissionIDsByCode(tx)
	if err != nil {
		return err
	}

	// every staff member of the org, by the legacy role they point at
	var staff []Staff
	if err := tx.Where("branch_id IN (?)", tx.Model(&Branch{}).Where("org_id = ?", orgID).Select("id")).
		Find(&staff).Error; err != nil {
		return err
	}
	legacyRoleIDs := map[uint]bool{}
	for _, s := range staff {
		legacyRoleIDs[s.RoleID] = true
	}

	// the legacy (global, org_id NULL) roles in play, with their own grants
	legacyByID := map[uint]Role{}
	legacyGrants := map[uint][]string{}
	if len(legacyRoleIDs) > 0 {
		ids := make([]uint, 0, len(legacyRoleIDs))
		for id := range legacyRoleIDs {
			ids = append(ids, id)
		}
		var legacy []Role
		if err := tx.Where("org_id IS NULL AND id IN ?", ids).Find(&legacy).Error; err != nil {
			return err
		}
		for _, r := range legacy {
			legacyByID[r.ID] = r
		}
		grantCodes, err := legacyGrantCodes(tx, ids)
		if err != nil {
			return err
		}
		legacyGrants = grantCodes
	}
	// the global manager/staff templates, for orgs with no staff to read them from
	templateGrants, err := legacyTemplateGrants(tx)
	if err != nil {
		return err
	}

	grantsFor := func(def DefaultRole, legacyCode string) []string {
		fromLegacy, ok := templateGrants[legacyCode]
		if !ok {
			// no legacy role to copy from: a database that never had one
			return def.Grants
		}
		merged := mergeGrants(fromLegacy, def.Grants)
		// apply_manual_discount is a legacy code, so it is copied from the legacy
		// role - which never had it for staff. A Cashier migrated now must still
		// be able to ask for a discount (Discount V1); the one-time grant for orgs
		// that already existed won't run again for this one.
		if def.Code == RoleCodeCashier && !slices.Contains(merged, PermApplyManualDiscount) {
			merged = append(merged, PermApplyManualDiscount)
		}
		return merged
	}

	newRoleByLegacyID := map[uint]uint{}
	var createdCodes []string
	create := func(def DefaultRole, legacyCode string) (*Role, error) {
		role, err := createOrgRole(tx, orgID, def.Code, def.Name, def.System, grantsFor(def, legacyCode), permIDs)
		if err != nil {
			return nil, err
		}
		createdCodes = append(createdCodes, def.Code)
		return role, nil
	}

	manager, err := create(DefaultRoles[0], "manager")
	if err != nil {
		return err
	}
	cashier, err := create(DefaultRoles[1], "staff")
	if err != nil {
		return err
	}

	for legacyID, legacyRole := range legacyByID {
		switch legacyRole.Code {
		case "manager":
			newRoleByLegacyID[legacyID] = manager.ID
		case "staff":
			newRoleByLegacyID[legacyID] = cashier.ID
		case "super_staff":
			role, err := create(SuperStaffRole, "super_staff")
			if err != nil {
				return err
			}
			newRoleByLegacyID[legacyID] = role.ID
		default:
			if slices.Contains(createdCodes, legacyRole.Code) {
				// a hand-made legacy role that happens to share a code with
				// one we just created: fold it into that role's slot would
				// change its grants, so give the clone a distinct code
				legacyRole.Code = fmt.Sprintf("%s_%d", legacyRole.Code, legacyID)
			}
			role, err := createOrgRole(tx, orgID, legacyRole.Code, legacyRole.Name, false, legacyGrants[legacyID], permIDs)
			if err != nil {
				return err
			}
			createdCodes = append(createdCodes, legacyRole.Code)
			newRoleByLegacyID[legacyID] = role.ID
		}
	}

	for _, s := range staff {
		newID, ok := newRoleByLegacyID[s.RoleID]
		if !ok {
			continue // dangling role id: nothing to remap it to
		}
		if err := tx.Model(&Staff{}).Where("id = ?", s.ID).Update("role", newID).Error; err != nil {
			return err
		}
	}
	return nil
}

// legacyGrantCodes returns, per legacy role id, the codes granted in the old
// global role_permission table.
func legacyGrantCodes(tx *gorm.DB, roleIDs []uint) (map[uint][]string, error) {
	var rows []struct {
		RoleID uint
		Code   string
	}
	if err := tx.Table("role_permission").
		Select("role_permission.role_id AS role_id, permissions.code AS code").
		Joins("JOIN permissions ON permissions.id = role_permission.permission_id").
		Where("role_permission.role_id IN ?", roleIDs).
		Order("permissions.code").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := map[uint][]string{}
	for _, r := range rows {
		out[r.RoleID] = append(out[r.RoleID], r.Code)
	}
	return out, nil
}

// legacyTemplateGrants returns the legacy grants (legacy permissions only) of
// the three seeded global roles, keyed by their legacy code, for the ones that
// exist. An org's Manager/Cashier/Super Staff take their legacy permissions
// from here even if no staff member of that org currently uses the role.
func legacyTemplateGrants(tx *gorm.DB) (map[string][]string, error) {
	var roles []Role
	if err := tx.Where("org_id IS NULL AND code IN ?", []string{"manager", "staff", "super_staff"}).Find(&roles).Error; err != nil {
		return nil, err
	}
	ids := make([]uint, len(roles))
	for i, r := range roles {
		ids[i] = r.ID
	}
	byID := map[uint][]string{}
	if len(ids) > 0 {
		var err error
		if byID, err = legacyGrantCodes(tx, ids); err != nil {
			return nil, err
		}
	}
	out := map[string][]string{}
	for _, r := range roles {
		var legacyOnly []string
		for _, code := range byID[r.ID] {
			if isLegacyPermission(code) {
				legacyOnly = append(legacyOnly, code)
			}
		}
		out[r.Code] = legacyOnly
	}
	return out, nil
}

// mergeGrants is the legacy role's own legacy-permission grants plus the
// catalogue defaults for every permission that didn't exist before.
func mergeGrants(legacy []string, defaults []string) []string {
	merged := slices.Clone(legacy)
	for _, code := range defaults {
		if !isLegacyPermission(code) && !slices.Contains(merged, code) {
			merged = append(merged, code)
		}
	}
	return merged
}
