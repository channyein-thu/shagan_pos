package identity

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"gorm.io/gorm"

	"shagan_pos/internal/audit"
	"shagan_pos/internal/common"
)

// maxRoleNameLen is the longest role name, in characters.
const maxRoleNameLen = 50

// ownerRole is the synthetic first row of `GET /roles`: the Owner is the
// org's own login account, not a staff role, so it has no row, no id and
// nothing to edit - it's shown only so the roles screen reads like Loyverse's.
var ownerRole = RoleSummary{
	ID: 0, Code: RoleCodeOwner, Name: "Owner",
	IsSystem: true, Editable: false, StaffCount: 0, POS: true, Backoffice: true,
}

func summarize(role Role, grants []string, staffCount int) RoleSummary {
	return RoleSummary{
		ID: role.ID, Code: role.Code, Name: role.Name,
		IsSystem: role.IsSystem, Editable: true, StaffCount: staffCount,
		POS:        slices.Contains(grants, PermAccessPOSPortal),
		Backoffice: slices.Contains(grants, PermAccessBackOffice),
	}
}

func (s *Service) ListRoles(ctx context.Context, orgID uint) ([]RoleSummary, error) {
	roles, err := s.repo.ListRoles(ctx, orgID)
	if err != nil {
		return nil, err
	}
	grants, err := s.repo.ListRoleGrantCodes(ctx, orgID)
	if err != nil {
		return nil, err
	}
	counts, err := s.repo.CountStaffByRole(ctx, orgID)
	if err != nil {
		return nil, err
	}
	out := make([]RoleSummary, 0, len(roles)+1)
	out = append(out, ownerRole)
	for _, role := range roles {
		out = append(out, summarize(role, grants[role.ID], counts[role.ID]))
	}
	return out, nil
}

// permissionGroupLabels is the heading each category's checklist sits under.
var permissionGroupLabels = map[string]string{
	CategoryPOS:        "POS",
	CategoryBackOffice: "Back office",
}

func (s *Service) ListPermissions(ctx context.Context) ([]PermissionGroup, error) {
	rows, err := s.repo.ListPermissions(ctx)
	if err != nil {
		return nil, err
	}
	byCode := make(map[string]Permission, len(rows))
	for _, p := range rows {
		byCode[p.Code] = p
	}
	groups := []PermissionGroup{
		{Category: CategoryPOS, Label: permissionGroupLabels[CategoryPOS], Permissions: []Permission{}},
		{Category: CategoryBackOffice, Label: permissionGroupLabels[CategoryBackOffice], Permissions: []Permission{}},
	}
	// catalogue order, not table order, and nothing outside the catalogue
	for _, def := range PermissionCatalogue {
		p, ok := byCode[def.Code]
		if !ok {
			continue
		}
		for i := range groups {
			if groups[i].Category == def.Category {
				groups[i].Permissions = append(groups[i].Permissions, p)
			}
		}
	}
	return groups, nil
}

func (s *Service) ListRolePermissions(ctx context.Context, orgID uint, id uint) ([]Permission, error) {
	// confirms the role belongs to orgID first, so another org's role id is
	// a 404 rather than an empty list that says "exists, nothing granted"
	if _, err := s.repo.GetRole(ctx, orgID, id); err != nil {
		return nil, err
	}
	return s.repo.ListRolePermissions(ctx, orgID, id)
}

// roleCodeFromName makes a role's code: the name lowercased with every run of
// non-alphanumeric characters collapsed to one underscore.
func roleCodeFromName(name string) string {
	var b strings.Builder
	pendingSep := false
	for _, r := range strings.ToLower(name) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if pendingSep && b.Len() > 0 {
				b.WriteByte('_')
			}
			pendingSep = false
			b.WriteRune(r)
		} else {
			pendingSep = true
		}
	}
	return b.String()
}

// validateRoleName trims name and checks it's 1..maxRoleNameLen characters.
func validateRoleName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", common.BadRequestError("name is required")
	}
	if utf8.RuneCountInString(name) > maxRoleNameLen {
		return "", common.BadRequestError("name must be at most 50 characters")
	}
	return name, nil
}

// resolvePermissionCodes checks every code is in the catalogue (400 listing
// the unknown ones), drops duplicates, and returns the codes in catalogue
// order with their permission ids.
func (s *Service) resolvePermissionCodes(ctx context.Context, codes []string) ([]string, []uint, error) {
	wanted := make(map[string]bool, len(codes))
	var unknown []string
	for _, code := range codes {
		if PermissionCategoryOf(code) == "" {
			unknown = append(unknown, code)
			continue
		}
		wanted[code] = true
	}
	if len(unknown) > 0 {
		return nil, nil, common.BadRequestError("unknown permission code(s): " + strings.Join(unknown, ", "))
	}
	if len(wanted) == 0 {
		return []string{}, nil, nil
	}
	rows, err := s.repo.ListPermissions(ctx)
	if err != nil {
		return nil, nil, err
	}
	idByCode := make(map[string]uint, len(rows))
	for _, p := range rows {
		idByCode[p.Code] = p.ID
	}
	ordered := make([]string, 0, len(wanted))
	ids := make([]uint, 0, len(wanted))
	for _, def := range PermissionCatalogue {
		if !wanted[def.Code] {
			continue
		}
		id, ok := idByCode[def.Code]
		if !ok {
			return nil, nil, common.SystemError("permission " + def.Code + " is missing - run the seed")
		}
		ordered = append(ordered, def.Code)
		ids = append(ids, id)
	}
	return ordered, ids, nil
}

// roleWarnings explains grants that are accepted but can't take effect:
// every permission sits under a switch, and is inert while that switch is off.
func roleWarnings(codes []string) []string {
	warnings := []string{}
	hasPOS := slices.Contains(codes, PermAccessPOSPortal)
	hasBackOffice := slices.Contains(codes, PermAccessBackOffice)
	var inertPOS, inertBackOffice []string
	for _, code := range codes {
		switch PermissionCategoryOf(code) {
		case CategoryPOS:
			if code != PermAccessPOSPortal && !hasPOS {
				inertPOS = append(inertPOS, code)
			}
		case CategoryBackOffice:
			if code != PermAccessBackOffice && !hasBackOffice {
				inertBackOffice = append(inertBackOffice, code)
			}
		}
	}
	if len(inertPOS) > 0 {
		warnings = append(warnings, "These POS permissions have no effect until the POS switch (access_pos_portal) is on: "+strings.Join(inertPOS, ", "))
	}
	if len(inertBackOffice) > 0 {
		warnings = append(warnings, "These back office permissions have no effect until the Back office switch (access_backoffice) is on: "+strings.Join(inertBackOffice, ", "))
	}
	return warnings
}

func (s *Service) roleDetail(role Role, codes []string, staffCount int) *RoleDetail {
	return &RoleDetail{
		RoleSummary:     summarize(role, codes, staffCount),
		PermissionCodes: codes,
		Warnings:        roleWarnings(codes),
	}
}

func roleAuditState(role Role, codes []string) any {
	return map[string]any{"id": role.ID, "code": role.Code, "name": role.Name, "is_system": role.IsSystem, "permission_codes": codes}
}

func (s *Service) auditRole(tx *gorm.DB, orgID, actorUserID, roleID uint, action string, before, after any) error {
	return s.audit.CreateAuditLog(tx, &audit.AuditLog{
		OrgID: orgID, ActorUserID: &actorUserID,
		Entity: "role", EntityID: strconv.FormatUint(uint64(roleID), 10), Action: action,
		Before: audit.ToJSON(before), After: audit.ToJSON(after),
	})
}

func (s *Service) CreateRole(ctx context.Context, orgID uint, actorUserID uint, in CreateRoleRequest) (*RoleDetail, error) {
	name, err := validateRoleName(in.Name)
	if err != nil {
		return nil, err
	}
	code := roleCodeFromName(name)
	if code == "" {
		return nil, common.BadRequestError("name must contain a letter or digit")
	}
	codes, permIDs, err := s.resolvePermissionCodes(ctx, in.PermissionCodes)
	if err != nil {
		return nil, err
	}

	existing, err := s.repo.ListRoles(ctx, orgID)
	if err != nil {
		return nil, err
	}
	if code == RoleCodeOwner || slices.ContainsFunc(existing, func(r Role) bool { return r.Code == code }) {
		return nil, common.ConflictError("a role with this name already exists")
	}

	role := Role{OrgID: &orgID, Code: code, Name: name}
	err = s.db.Transaction(func(tx *gorm.DB) error {
		if err := s.repo.CreateRole(tx, &role); err != nil {
			return err
		}
		if err := s.repo.ReplaceRoleGrants(tx, orgID, role.ID, permIDs); err != nil {
			return err
		}
		return s.auditRole(tx, orgID, actorUserID, role.ID, "created", nil, roleAuditState(role, codes))
	})
	if err != nil {
		return nil, err
	}
	return s.roleDetail(role, codes, 0), nil
}

func (s *Service) UpdateRole(ctx context.Context, orgID uint, actorUserID uint, id uint, in UpdateRoleRequest) (*RoleDetail, error) {
	if id == ownerRole.ID {
		return nil, common.ForbiddenError("the Owner role is fixed and cannot be edited")
	}
	role, err := s.repo.GetRole(ctx, orgID, id)
	if err != nil {
		return nil, err
	}
	grants, err := s.repo.ListRoleGrantCodes(ctx, orgID)
	if err != nil {
		return nil, err
	}
	counts, err := s.repo.CountStaffByRole(ctx, orgID)
	if err != nil {
		return nil, err
	}
	before := roleAuditState(*role, grants[role.ID])

	newName := role.Name
	renamed := false
	if in.Name != nil {
		name, err := validateRoleName(*in.Name)
		if err != nil {
			return nil, err
		}
		if name != role.Name {
			others, err := s.repo.ListRoles(ctx, orgID)
			if err != nil {
				return nil, err
			}
			if slices.ContainsFunc(others, func(r Role) bool { return r.ID != role.ID && strings.EqualFold(r.Name, name) }) {
				return nil, common.ConflictError("a role with this name already exists")
			}
			newName, renamed = name, true
		}
	}

	newCodes := grants[role.ID]
	var newPermIDs []uint
	regrant := in.PermissionCodes != nil
	if regrant {
		newCodes, newPermIDs, err = s.resolvePermissionCodes(ctx, *in.PermissionCodes)
		if err != nil {
			return nil, err
		}
		// Taking the POS switch off the last role that has it would leave
		// nobody able to sign in at a till.
		if slices.Contains(grants[role.ID], PermAccessPOSPortal) && !slices.Contains(newCodes, PermAccessPOSPortal) {
			otherHolder := false
			for otherID, codes := range grants {
				if otherID != role.ID && slices.Contains(codes, PermAccessPOSPortal) {
					otherHolder = true
					break
				}
			}
			if !otherHolder {
				return nil, common.ConflictError("at least one role must keep the POS switch on, or nobody could use the till")
			}
		}
	}

	if renamed || regrant {
		err = s.db.Transaction(func(tx *gorm.DB) error {
			if renamed {
				if err := s.repo.UpdateRoleName(tx, orgID, role.ID, newName); err != nil {
					return err
				}
			}
			if regrant {
				if err := s.repo.ReplaceRoleGrants(tx, orgID, role.ID, newPermIDs); err != nil {
					return err
				}
			}
			updated := *role
			updated.Name = newName
			return s.auditRole(tx, orgID, actorUserID, role.ID, "updated", before, roleAuditState(updated, newCodes))
		})
		if err != nil {
			return nil, err
		}
	}
	role.Name = newName
	return s.roleDetail(*role, newCodes, counts[role.ID]), nil
}

func (s *Service) DeleteRole(ctx context.Context, orgID uint, actorUserID uint, id uint) error {
	if id == ownerRole.ID {
		return common.ForbiddenError("the Owner role is fixed and cannot be deleted")
	}
	role, err := s.repo.GetRole(ctx, orgID, id)
	if err != nil {
		return err
	}
	if role.IsSystem {
		return common.ConflictError("Manager and Cashier are built-in roles and cannot be deleted")
	}
	counts, err := s.repo.CountStaffByRole(ctx, orgID)
	if err != nil {
		return err
	}
	if counts[role.ID] > 0 {
		return common.ConflictError("this role is still assigned to staff - move them to another role first")
	}
	grants, err := s.repo.ListRoleGrantCodes(ctx, orgID)
	if err != nil {
		return err
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := s.repo.DeleteRole(tx, orgID, role.ID); err != nil {
			return err
		}
		return s.auditRole(tx, orgID, actorUserID, role.ID, "deleted", roleAuditState(*role, grants[role.ID]), nil)
	})
}
