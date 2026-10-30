package store

import "testing"

func TestRoleRankAndCan(t *testing.T) {
	owner := &User{Role: RoleOwner, IsAdmin: true}
	admin := &User{Role: RoleAdmin, IsAdmin: true}
	staff := &User{Role: RoleStaff, IsAdmin: true}
	user := &User{Role: RoleUser}

	if !owner.Can(PermProvidersManage) || !owner.Can(PermUsersRole) {
		t.Fatal("owner should hold keys and role grants")
	}
	if admin.Can(PermProvidersManage) || admin.Can(PermConfigManage) {
		t.Fatal("admin must not manage provider keys or pay config")
	}
	if !admin.Can(PermUsersRole) || !admin.Can(PermModelsManage) {
		t.Fatal("admin should manage roles and models")
	}
	if staff.Can(PermUsersRole) || staff.Can(PermModelsManage) || staff.Can(PermSystemRead) {
		t.Fatal("staff should not manage roles/models/system")
	}
	if !staff.Can(PermUsersWrite) || !staff.Can(PermStatsRead) {
		t.Fatal("staff should operate users and read stats")
	}
	if user.Can(PermUsersRead) || user.IsStaffPlus() {
		t.Fatal("regular user must stay out of admin")
	}
}

func TestCanActOn(t *testing.T) {
	owner := &User{Role: RoleOwner}
	admin := &User{Role: RoleAdmin}
	staff := &User{Role: RoleStaff}
	user := &User{Role: RoleUser}

	if !CanActOn(owner, admin) || !CanActOn(owner, staff) || !CanActOn(admin, staff) || !CanActOn(staff, user) {
		t.Fatal("higher rank should act on lower")
	}
	if CanActOn(admin, owner) || CanActOn(admin, admin) || CanActOn(owner, owner) || CanActOn(staff, admin) {
		t.Fatal("same or higher rank must be denied")
	}
	if CanAssignRole(admin, RoleOwner) || CanAssignRole(admin, RoleAdmin) {
		t.Fatal("admin cannot grant owner/admin")
	}
	if !CanAssignRole(owner, RoleAdmin) || !CanAssignRole(admin, RoleStaff) {
		t.Fatal("owner/admin should grant the ranks below them")
	}
}
