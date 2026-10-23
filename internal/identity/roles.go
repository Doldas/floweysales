package identity

import (
	"context"
	"errors"
	"regexp"
)

var roleNamePattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]{1,39}$`)
var allowedPermissions = map[string]bool{
	"research.read": true, "ai.chat": true, "models.use": true,
	"flows.read": true, "flows.write": true, "flows.run": true,
}

type RoleDefinition struct {
	Name        string   `json:"name"`
	Permissions []string `json:"permissions"`
}

func (s *Service) ListRoles(ctx context.Context) ([]RoleDefinition, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT role_name FROM auth_roles ORDER BY role_name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []RoleDefinition{}
	for rows.Next() {
		var role RoleDefinition
		if err := rows.Scan(&role.Name); err != nil {
			return nil, err
		}
		grants, err := s.PermissionsForRole(ctx, role.Name)
		if err != nil {
			return nil, err
		}
		role.Permissions = grants
		result = append(result, role)
	}
	return result, rows.Err()
}
func (s *Service) PermissionsForRole(ctx context.Context, role string) ([]string, error) {
	if role == RoleSysAdmin {
		// SYS_ADMIN has platform authority; the list below is informative only.
		return []string{"research.read", "ai.chat", "models.use", "flows.read", "flows.write", "flows.run"}, nil
	}
	rows, err := s.db.QueryContext(ctx, "SELECT permission FROM auth_role_permissions WHERE role_name=? ORDER BY permission", role)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
func (s *Service) HasPermission(ctx context.Context, role, permission string) bool {
	if role == RoleSysAdmin {
		return true
	}
	if !allowedPermissions[permission] {
		return false
	}
	var found int
	return s.db.QueryRowContext(ctx, "SELECT 1 FROM auth_role_permissions WHERE role_name=? AND permission=?", role, permission).Scan(&found) == nil
}
func (s *Service) AddRole(ctx context.Context, name string) error {
	if !roleNamePattern.MatchString(name) || name == RoleSysAdmin {
		return errors.New("invalid role")
	}
	_, err := s.db.ExecContext(ctx, "INSERT INTO auth_roles(role_name) VALUES(?)", name)
	return err
}
func (s *Service) SetRolePermission(ctx context.Context, role, permission string, enabled bool) error {
	if role == RoleSysAdmin || !roleNamePattern.MatchString(role) || !allowedPermissions[permission] {
		return errors.New("invalid role or permission")
	}
	var found int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM auth_roles WHERE role_name=?", role).Scan(&found); err != nil || found == 0 {
		return errors.New("role does not exist")
	}
	if enabled {
		_, err := s.db.ExecContext(ctx, "INSERT IGNORE INTO auth_role_permissions(role_name,permission) VALUES(?,?)", role, permission)
		return err
	}
	_, err := s.db.ExecContext(ctx, "DELETE FROM auth_role_permissions WHERE role_name=? AND permission=?", role, permission)
	return err
}
func (s *Service) RoleExists(ctx context.Context, role string) bool {
	var found int
	return s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM auth_roles WHERE role_name=?", role).Scan(&found) == nil && found == 1
}
