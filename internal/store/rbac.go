package store

import "strings"

// 角色分层对齐 New API（user / admin / root）与 Open WebUI（user / admin），
// 并加一层客服（staff）方便日常运营、不接触密钥。
const (
	RoleUser  = "user"
	RoleStaff = "staff"
	RoleAdmin = "admin"
	RoleOwner = "owner"

	PermUsersRead       = "users.read"
	PermUsersWrite      = "users.write"
	PermUsersRole       = "users.role"
	PermStatsRead       = "stats.read"
	PermSystemRead      = "system.read"
	PermModelsManage    = "models.manage"
	PermProvidersManage = "providers.manage"
	PermConfigManage    = "config.manage"
)

var allPerms = []string{
	PermUsersRead,
	PermUsersWrite,
	PermUsersRole,
	PermStatsRead,
	PermSystemRead,
	PermModelsManage,
	PermProvidersManage,
	PermConfigManage,
}

func NormalizeRole(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case RoleStaff, RoleAdmin, RoleOwner:
		return strings.ToLower(strings.TrimSpace(role))
	default:
		return RoleUser
	}
}

func RoleRank(role string) int {
	switch NormalizeRole(role) {
	case RoleStaff:
		return 1
	case RoleAdmin:
		return 2
	case RoleOwner:
		return 3
	default:
		return 0
	}
}

// RoleName 返回规范化角色。旧数据只有 is_admin、没有 role 时，管理员视为 admin。
func (u *User) RoleName() string {
	if u == nil {
		return RoleUser
	}
	r := NormalizeRole(u.Role)
	if r == RoleUser && u.IsAdmin {
		return RoleAdmin
	}
	return r
}

func (u *User) IsStaffPlus() bool {
	return RoleRank(u.RoleName()) >= 1
}

func (u *User) Can(perm string) bool {
	if u == nil {
		return false
	}
	rank := RoleRank(u.RoleName())
	switch perm {
	case PermUsersRead, PermUsersWrite, PermStatsRead:
		return rank >= 1
	case PermUsersRole, PermSystemRead, PermModelsManage:
		return rank >= 2
	case PermProvidersManage, PermConfigManage:
		return rank >= 3
	default:
		return false
	}
}

func (u *User) Permissions() []string {
	out := make([]string, 0, len(allPerms))
	for _, p := range allPerms {
		if u.Can(p) {
			out = append(out, p)
		}
	}
	return out
}

// AssignableRoles 当前操作者可以授予他人的角色（不能授予同级或更高）。
func AssignableRoles(actor *User) []string {
	switch actor.RoleName() {
	case RoleOwner:
		return []string{RoleUser, RoleStaff, RoleAdmin}
	case RoleAdmin:
		return []string{RoleUser, RoleStaff}
	default:
		return []string{RoleUser}
	}
}

func roleAllowed(list []string, role string) bool {
	role = NormalizeRole(role)
	for _, r := range list {
		if r == role {
			return true
		}
	}
	return false
}

func CanAssignRole(actor *User, role string) bool {
	return roleAllowed(AssignableRoles(actor), role)
}

// CanActOn：只能操作严格低于自己的账号。站长可管管理员/客服，但不能互改另一位站长。
func CanActOn(actor, target *User) bool {
	if actor == nil || target == nil {
		return false
	}
	ar, tr := actor.RoleName(), target.RoleName()
	if ar == RoleOwner && tr != RoleOwner {
		return true
	}
	return RoleRank(ar) > RoleRank(tr)
}
