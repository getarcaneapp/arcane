package group

import (
	"context"
	"testing"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/libtnb/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v3/internal/audit"
	"github.com/getarcaneapp/arcane/backend/v3/internal/common"
	"github.com/getarcaneapp/arcane/backend/v3/internal/database"
	"github.com/getarcaneapp/arcane/backend/v3/internal/role"
	"github.com/getarcaneapp/arcane/backend/v3/internal/user"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/pagination"
)

type groupTestEnv struct {
	service     *GroupService
	roles       *role.RoleService
	db          *gorm.DB
	defaultRole *string
}

func setupGroupService(t *testing.T) groupTestEnv {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&audit.AuditLog{},
		&user.User{},
		&role.Role{},
		&role.RoleBinding{},
		&Group{},
		&GroupMember{},
		&OidcGroupMapping{},
	))
	require.NoError(t, db.Exec("CREATE TABLE environments (id TEXT PRIMARY KEY, name TEXT)").Error)
	require.NoError(t, db.Exec("CREATE TABLE federated_credentials (id TEXT PRIMARY KEY, identity_user_id TEXT)").Error)
	require.NoError(t, db.Exec("INSERT INTO environments (id, name) VALUES ('env-1', 'Production')").Error)
	for _, id := range []string{"alice", "bob", "carol"} {
		require.NoError(t, db.Create(&user.User{ID: id, Username: id}).Error)
	}
	roles := role.NewRoleService(&database.DB{DB: db})
	defaultRole := ""
	return groupTestEnv{
		service: NewGroupService(&database.DB{DB: db}, roles, func(context.Context) string {
			return defaultRole
		}),
		roles:       roles,
		db:          db,
		defaultRole: &defaultRole,
	}
}

func (env groupTestEnv) createGroup(t *testing.T, name string) types.Group {
	t.Helper()
	created, err := env.service.Create(t.Context(), types.CreateGroup{Name: name})
	require.NoError(t, err)
	return created
}

func (env groupTestEnv) bindGroup(t *testing.T, groupID, roleID string, environmentID *string) {
	t.Helper()
	require.NoError(t, env.db.Create(&role.RoleBinding{
		GroupID:       new(groupID),
		RoleID:        roleID,
		EnvironmentID: environmentID,
		Source:        role.BindingSourceManual,
	}).Error)
}

func (env groupTestEnv) memberships(t *testing.T, userID string) []string {
	t.Helper()
	var rows []GroupMember
	require.NoError(t, env.db.Where("user_id = ?", userID).Find(&rows).Error)
	out := make([]string, len(rows))
	for i, row := range rows {
		out[i] = row.GroupID + ":" + row.Source
	}
	return out
}

func TestGroupCRUDReportsCounts(t *testing.T) {
	ctx := t.Context()
	env := setupGroupService(t)

	ops := env.createGroup(t, " Operators ")
	require.Equal(t, "Operators", ops.Name)
	_, err := env.service.Create(ctx, types.CreateGroup{Name: "Operators"})
	require.ErrorIs(t, err, common.ErrGroupNameTaken)
	env.createGroup(t, "Auditors")

	require.NoError(t, env.service.AddMembers(ctx, authz.SudoPermissionSet(), ops.ID, []string{"alice", "bob", "alice"}))
	env.bindGroup(t, ops.ID, authz.BuiltInRoleViewer, nil)
	got, err := env.service.Get(ctx, ops.ID)
	require.NoError(t, err)
	require.Equal(t, 2, got.MemberCount)
	require.Equal(t, 1, got.BindingCount)

	groups, page, err := env.service.List(ctx, pagination.QueryParams{Limit: 20, Sort: "name", Order: "asc"})
	require.NoError(t, err)
	require.EqualValues(t, 2, page.TotalItems)
	require.Equal(t, "Auditors", groups[0].Name)

	updated, err := env.service.Update(ctx, ops.ID, types.UpdateGroup{Name: new("Ops"), Description: new("Runs things")}, map[string]bool{"name": true, "description": true})
	require.NoError(t, err)
	require.Equal(t, "Ops", updated.Name)
	require.Equal(t, "Runs things", *updated.Description)
	renamed, err := env.service.Update(ctx, ops.ID, types.UpdateGroup{Name: new("Ops")}, map[string]bool{"name": true})
	require.NoError(t, err)
	require.Equal(t, "Runs things", *renamed.Description)
	cleared, err := env.service.Update(ctx, ops.ID, types.UpdateGroup{}, map[string]bool{"description": true})
	require.NoError(t, err)
	require.Nil(t, cleared.Description)
	_, err = env.service.Update(ctx, ops.ID, types.UpdateGroup{Name: new("Auditors")}, map[string]bool{"name": true})
	require.ErrorIs(t, err, common.ErrGroupNameTaken)

	members, err := env.service.ListMembers(ctx, ops.ID)
	require.NoError(t, err)
	require.Len(t, members, 2)
	require.Equal(t, "alice", members[0].Username)

	require.NoError(t, env.service.Delete(ctx, authz.SudoPermissionSet(), ops.ID))
	_, err = env.service.Get(ctx, ops.ID)
	require.ErrorIs(t, err, common.ErrGroupNotFound)
	var leftovers int64
	require.NoError(t, env.db.Model(&role.RoleBinding{}).Where("group_id = ?", ops.ID).Count(&leftovers).Error)
	require.Zero(t, leftovers)
}

func TestGroupChangesRequireCallerToCoverGroupBindings(t *testing.T) {
	ctx := t.Context()
	env := setupGroupService(t)
	ops := env.createGroup(t, "Operators")
	env.bindGroup(t, ops.ID, authz.BuiltInRoleEditor, new("env-1"))

	editor, ok := authz.BuiltInRole(authz.BuiltInRoleEditor)
	require.True(t, ok)
	viewer, ok := authz.BuiltInRole(authz.BuiltInRoleViewer)
	require.True(t, ok)
	narrow := authz.NewPermissionSet()
	narrow.AddEnv("env-1", viewer.Permissions...)
	covering := authz.NewPermissionSet()
	covering.AddEnv("env-1", editor.Permissions...)

	require.ErrorIs(t, env.service.AddMembers(ctx, narrow, ops.ID, []string{"bob"}), common.ErrRolePermissionEscalation)
	require.NoError(t, env.service.AddMembers(ctx, covering, ops.ID, []string{"bob"}))
	require.ErrorIs(t, env.service.AddMembers(ctx, covering, ops.ID, []string{"nobody"}), common.ErrBadRequest)

	bobPerms, err := env.roles.ResolvePermissions(ctx, "bob")
	require.NoError(t, err)
	require.True(t, bobPerms.Allows(authz.PermContainersStart, "env-1"))

	require.ErrorIs(t, env.service.RemoveMember(ctx, narrow, ops.ID, "bob"), common.ErrRolePermissionEscalation)
	require.ErrorIs(t, env.service.Delete(ctx, narrow, ops.ID), common.ErrRolePermissionEscalation)
	_, err = env.service.CreateOidcMapping(ctx, narrow, types.OidcGroupMappingSpec{ClaimValue: "ops", GroupID: ops.ID})
	require.ErrorIs(t, err, common.ErrRolePermissionEscalation)

	require.NoError(t, env.service.RemoveMember(ctx, covering, ops.ID, "bob"))
	require.ErrorIs(t, env.service.RemoveMember(ctx, covering, ops.ID, "bob"), common.ErrNotFound)
	bobPerms, err = env.roles.ResolvePermissions(ctx, "bob")
	require.NoError(t, err)
	require.False(t, bobPerms.Allows(authz.PermContainersStart, "env-1"))
}

func TestRemoveMemberKeepsLastGlobalAdmin(t *testing.T) {
	ctx := t.Context()
	env := setupGroupService(t)
	admins := env.createGroup(t, "Admins")
	env.bindGroup(t, admins.ID, authz.BuiltInRoleAdmin, nil)
	require.NoError(t, env.service.AddMembers(ctx, authz.SudoPermissionSet(), admins.ID, []string{"alice"}))

	require.ErrorIs(t, env.service.RemoveMember(ctx, authz.SudoPermissionSet(), admins.ID, "alice"), common.ErrNoGlobalAdminRemains)
	require.ErrorIs(t, env.service.Delete(ctx, authz.SudoPermissionSet(), admins.ID), common.ErrNoGlobalAdminRemains)
	require.Equal(t, []string{admins.ID + ":" + MemberSourceManual}, env.memberships(t, "alice"))
}

func TestSyncOidcMembershipsReplacesOidcMembershipsAndKeepsLastAdmin(t *testing.T) {
	ctx := t.Context()
	env := setupGroupService(t)
	devs := env.createGroup(t, "Developers")
	ops := env.createGroup(t, "Operators")
	admins := env.createGroup(t, "Admins")
	env.bindGroup(t, admins.ID, authz.BuiltInRoleAdmin, nil)
	for claim, groupID := range map[string]string{"devs": devs.ID, "ops": ops.ID, "admins": admins.ID} {
		_, err := env.service.CreateOidcMapping(ctx, authz.SudoPermissionSet(), types.OidcGroupMappingSpec{ClaimValue: claim, GroupID: groupID})
		require.NoError(t, err)
	}
	require.NoError(t, env.service.AddMembers(ctx, authz.SudoPermissionSet(), ops.ID, []string{"bob"}))

	require.NoError(t, env.service.SyncOidcMemberships(ctx, "bob", []string{"devs", "ops", "unmapped"}))
	require.ElementsMatch(t, []string{
		devs.ID + ":" + MemberSourceOidc,
		ops.ID + ":" + MemberSourceOidc,
		ops.ID + ":" + MemberSourceManual,
	}, env.memberships(t, "bob"))

	require.NoError(t, env.service.SyncOidcMemberships(ctx, "bob", []string{"ops"}))
	require.ElementsMatch(t, []string{
		ops.ID + ":" + MemberSourceOidc,
		ops.ID + ":" + MemberSourceManual,
	}, env.memberships(t, "bob"))

	require.NoError(t, env.service.SyncOidcMemberships(ctx, "alice", []string{"admins", "devs"}))
	alicePerms, err := env.roles.ResolvePermissions(ctx, "alice")
	require.NoError(t, err)
	require.True(t, alicePerms.IsGlobalAdmin())

	require.NoError(t, env.service.SyncOidcMemberships(ctx, "alice", nil))
	require.Contains(t, env.memberships(t, "alice"), admins.ID+":"+MemberSourceOidc)
	require.NoError(t, env.roles.AssertGlobalAdminExists(ctx, env.db))
}

func TestReconcileEnvOidcMappingsCreatesGroupsAndReplacesEnvRows(t *testing.T) {
	ctx := t.Context()
	env := setupGroupService(t)
	manualGroup := env.createGroup(t, "Manual")
	manual, err := env.service.CreateOidcMapping(ctx, authz.SudoPermissionSet(), types.OidcGroupMappingSpec{ClaimValue: "manual", GroupID: manualGroup.ID})
	require.NoError(t, err)

	require.NoError(t, env.service.ReconcileEnvOidcMappings(ctx, "alpha=Alpha Team, beta = Beta,"))
	mappings, err := env.service.ListOidcMappings(ctx)
	require.NoError(t, err)
	require.Len(t, mappings, 3)
	byClaim := map[string]types.OidcGroupMapping{}
	for _, mapping := range mappings {
		byClaim[mapping.ClaimValue] = mapping
	}
	require.Equal(t, "Alpha Team", byClaim["alpha"].GroupName)
	require.Equal(t, MappingSourceEnv, byClaim["alpha"].Source)
	require.Equal(t, "Beta", byClaim["beta"].GroupName)
	require.Equal(t, MappingSourceManual, byClaim["manual"].Source)

	_, err = env.service.UpdateOidcMapping(ctx, authz.SudoPermissionSet(), byClaim["alpha"].ID, types.OidcGroupMappingSpec{ClaimValue: "alpha", GroupID: manualGroup.ID})
	require.ErrorIs(t, err, common.ErrOidcMappingEnvManaged)
	require.ErrorIs(t, env.service.DeleteOidcMapping(ctx, authz.SudoPermissionSet(), byClaim["beta"].ID), common.ErrOidcMappingEnvManaged)

	require.NoError(t, env.service.ReconcileEnvOidcMappings(ctx, "alpha=Alpha Team"))
	mappings, err = env.service.ListOidcMappings(ctx)
	require.NoError(t, err)
	require.Len(t, mappings, 2)
	var alphaGroups int64
	require.NoError(t, env.db.Model(&Group{}).Where("name = ?", "Alpha Team").Count(&alphaGroups).Error)
	require.EqualValues(t, 1, alphaGroups)

	require.NoError(t, env.service.ReconcileEnvOidcMappings(ctx, "  "))
	require.Error(t, env.service.ReconcileEnvOidcMappings(ctx, "missing-separator"))
	require.Error(t, env.service.ReconcileEnvOidcMappings(ctx, "=NoClaim"))

	updated, err := env.service.UpdateOidcMapping(ctx, authz.SudoPermissionSet(), manual.ID, types.OidcGroupMappingSpec{ClaimValue: "renamed", GroupID: manualGroup.ID})
	require.NoError(t, err)
	require.Equal(t, "renamed", updated.ClaimValue)
	require.NoError(t, env.service.DeleteOidcMapping(ctx, authz.SudoPermissionSet(), manual.ID))
}

func TestApplyDefaultRoleBindsConfiguredNonAdminRole(t *testing.T) {
	ctx := t.Context()
	env := setupGroupService(t)
	bindingCount := func(userID string) int64 {
		var count int64
		require.NoError(t, env.db.Model(&role.RoleBinding{}).Where("user_id = ?", userID).Count(&count).Error)
		return count
	}

	require.NoError(t, env.service.ApplyDefaultRole(ctx, "alice"))
	require.Zero(t, bindingCount("alice"))

	for _, roleID := range []string{authz.BuiltInRoleAdmin, "role_missing"} {
		*env.defaultRole = roleID
		require.NoError(t, env.service.ApplyDefaultRole(ctx, "alice"))
		require.Zero(t, bindingCount("alice"), roleID)
	}

	*env.defaultRole = authz.BuiltInRoleViewer
	require.NoError(t, env.service.ApplyDefaultRole(ctx, "alice"))
	alicePerms, err := env.roles.ResolvePermissions(ctx, "alice")
	require.NoError(t, err)
	require.True(t, alicePerms.Allows(authz.PermContainersList, ""))

	require.NoError(t, env.service.SyncOidcMemberships(ctx, "bob", nil))
	require.EqualValues(t, 1, bindingCount("bob"))

	devs := env.createGroup(t, "Developers")
	_, err = env.service.CreateOidcMapping(ctx, authz.SudoPermissionSet(), types.OidcGroupMappingSpec{ClaimValue: "devs", GroupID: devs.ID})
	require.NoError(t, err)
	require.NoError(t, env.service.SyncOidcMemberships(ctx, "carol", []string{"devs"}))
	require.Zero(t, bindingCount("carol"))
}
