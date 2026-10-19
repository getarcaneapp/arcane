package role

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	roletypes "github.com/getarcaneapp/arcane/types/v2/role"
	"github.com/samber/hot"
	"github.com/samber/mo"
	"go.getarcane.app/kit/normalization"
	"go.getarcane.app/kit/pkg"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/kv"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/dbutil"
)

const (
	permissionCacheTTL                   = 60 * time.Second
	legacyRoleBackfillCompletedKey       = "migration.legacy_user_roles.v1.completed"
	globalAdminGuardLockKey        int64 = 0x617263616e65
)

type RoleService struct {
	db             *database.DB
	userCache      *hot.HotCache[string, *authz.PermissionSet]
	apiKeyCache    *hot.HotCache[string, *authz.PermissionSet]
	cacheFillMu    sync.Mutex
	userCacheGen   atomic.Uint64
	apiKeyCacheGen atomic.Uint64
}

func NewRoleService(db *database.DB) *RoleService {
	return &RoleService{
		db: db,
		userCache: hot.NewHotCache[string, *authz.PermissionSet](hot.LRU, 2048).
			WithTTL(permissionCacheTTL).
			WithJanitor().
			Build(),
		apiKeyCache: hot.NewHotCache[string, *authz.PermissionSet](hot.LRU, 2048).
			WithTTL(permissionCacheTTL).
			WithJanitor().
			Build(),
	}
}

func (s *RoleService) EnsureBuiltInRoles(ctx context.Context) error {
	builtIns := map[string]struct {
		name string
		desc string
		perm []string
	}{
		authz.BuiltInRoleAdmin:         {"Admin", "Full administrative access", authz.AllPermissions()},
		authz.BuiltInRoleEditor:        {"Editor", "Read and write on Docker resources", authz.BuiltInEditorPermissions()},
		authz.BuiltInRoleNoShellEditor: {"No-Shell Editor", "Editor without interactive container shell access", authz.BuiltInNoShellEditorPermissions()},
		authz.BuiltInRoleDeployer:      {"Deployer", "Deploy and lifecycle containers and projects", authz.BuiltInDeployerPermissions()},
		authz.BuiltInRoleMonitor:       {"Monitor", "Observability-only access: logs, dashboards, events", authz.BuiltInMonitorPermissions()},
		authz.BuiltInRoleViewer:        {"Viewer", "Read-only access to all resources", authz.BuiltInViewerPermissions()},
	}

	return dbutil.WithTx(ctx, s.db.DB, func(tx *gorm.DB) error {
		for id, spec := range builtIns {
			role := Role{
				ID:          id,
				Name:        spec.name,
				Description: new(spec.desc),
				Permissions: database.StringSlice(spec.perm),
				BuiltIn:     true,
			}
			if err := tx.Save(&role).Error; err != nil {
				return fmt.Errorf("failed to upsert built-in role %s: %w", id, err)
			}
		}
		return nil
	})
}

// BackfillLegacyRoleAssignments converts pre-RBAC users.roles into global assignments once.
func (s *RoleService) BackfillLegacyRoleAssignments(ctx context.Context) error {
	migrator := s.db.WithContext(ctx).Migrator()
	if !migrator.HasColumn("users", "roles") {
		return nil
	}

	type legacyUser struct {
		ID    string `gorm:"column:id"`
		Roles string `gorm:"column:roles"`
	}

	var inserted int64
	err := dbutil.WithTx(ctx, s.db.DB, func(tx *gorm.DB) error {
		claimed, err := kv.NewKVService(&database.DB{DB: tx}).CreateIfAbsent(ctx, legacyRoleBackfillCompletedKey, time.Now().UTC().Format(time.RFC3339))
		if err != nil {
			return fmt.Errorf("failed to claim legacy role backfill marker: %w", err)
		}
		if !claimed {
			return nil
		}
		var rows []legacyUser
		if loadLegacyUsersErr := tx.Table("users").Select("id, roles").
			Where("NOT EXISTS (SELECT 1 FROM user_role_assignments ura WHERE ura.user_id = users.id)").
			Scan(&rows).Error; loadLegacyUsersErr != nil {
			return fmt.Errorf("failed to read legacy users.roles for backfill: %w", loadLegacyUsersErr)
		}
		for _, u := range rows {
			roleID := kit.Ternary(legacyRolesContainsAdminInternal(u.Roles), authz.BuiltInRoleAdmin, authz.BuiltInRoleViewer)
			assignment := UserRoleAssignment{
				UserID: u.ID,
				RoleID: roleID,
				Source: RoleAssignmentSourceManual,
			}
			result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&assignment)
			if result.Error != nil {
				return fmt.Errorf("failed to backfill assignment for user %s: %w", u.ID, result.Error)
			}
			inserted += result.RowsAffected
		}
		return nil
	})
	if err != nil {
		return err
	}
	if inserted > 0 {
		slog.InfoContext(ctx, "Backfilled legacy users.roles into user_role_assignments", "assignmentCount", inserted)
	}
	return nil
}

// legacyRolesContainsAdminInternal reports whether a legacy users.roles JSON value names "admin".
func legacyRolesContainsAdminInternal(raw string) bool {
	var roles []string
	if err := json.Unmarshal([]byte(raw), &roles); err != nil {
		return false
	}
	for _, r := range roles {
		if strings.EqualFold(strings.TrimSpace(r), "admin") {
			return true
		}
	}
	return false
}

// AssertGlobalAdminExists returns common.ErrNoGlobalAdminRemains when no non-service user is a global admin in tx.
func (s *RoleService) AssertGlobalAdminExists(ctx context.Context, tx *gorm.DB) error {
	count, err := s.CountGlobalAdmins(ctx, tx, "")
	if err != nil {
		return err
	}
	if count == 0 {
		return common.Classify(common.ErrNoGlobalAdminRemains, errors.New("At least one user must retain a global Admin role assignment")) //nolint:staticcheck // Preserve the existing error message.
	}
	return nil
}

func (s *RoleService) LockGlobalAdminGuard(tx *gorm.DB) error {
	if tx.Name() != "postgres" {
		return nil
	}
	if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", globalAdminGuardLockKey).Error; err != nil {
		return fmt.Errorf("failed to lock global admin guard: %w", err)
	}
	return nil
}

func (s *RoleService) ListRoles(ctx context.Context, params pagination.QueryParams) ([]Role, pagination.Response, error) {
	var roles []Role
	query := s.db.WithContext(ctx).Model(&Role{})

	if term := strings.TrimSpace(params.Search); term != "" {
		pattern := "%" + term + "%"
		query = query.Where("name LIKE ? OR COALESCE(description, '') LIKE ?", pattern, pattern)
	}

	resp, err := pagination.PaginateAndSortDB(params, query, &roles)
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to paginate roles: %w", err)
	}
	return roles, resp, nil
}

func (s *RoleService) ListAllRoles(ctx context.Context) ([]Role, error) {
	var roles []Role
	if err := s.db.WithContext(ctx).Order("name").Find(&roles).Error; err != nil {
		return nil, fmt.Errorf("failed to list roles: %w", err)
	}
	return roles, nil
}

func (s *RoleService) GetRole(ctx context.Context, id string) (*Role, error) {
	return dbutil.FirstWhere[Role](ctx, s.db.DB, common.Classify(common.ErrRoleNotFound, errors.New("Role not found")), "id = ?", id)
}

func (s *RoleService) CreateRole(ctx context.Context, name string, description *string, permissions []string) (*Role, error) {
	input := roletypes.CreateRole{Name: name, Description: description, Permissions: permissions}
	if err := normalization.Normalize(&input); err != nil {
		return nil, err
	}
	if err := validatePermissionsInternal(permissions); err != nil {
		return nil, err
	}
	role := &Role{
		Name:        input.Name,
		Description: input.Description,
		Permissions: database.StringSlice(permissions),
		BuiltIn:     false,
	}
	err := dbutil.WithTx(ctx, s.db.DB, func(tx *gorm.DB) error {
		var conflict int64
		if err := tx.Model(&Role{}).Where("name = ?", input.Name).Count(&conflict).Error; err != nil {
			return fmt.Errorf("failed to check role name uniqueness: %w", err)
		}
		if conflict > 0 {
			return common.Classify(common.ErrRoleNameTaken, errors.New("Role name already in use"))
		}
		return tx.Create(role).Error
	})
	if err != nil {
		return nil, err
	}
	return role, nil
}

// lockAssignedUserRows locks the users holding roleID so role writes serialize with user-domain privilege checks.
func lockAssignedUserRows(tx *gorm.DB, roleID string) ([]string, error) {
	var ids []string
	if err := tx.Model(&UserRoleAssignment{}).
		Where("role_id = ?", roleID).
		Distinct("user_id").
		Pluck("user_id", &ids).Error; err != nil {
		return nil, fmt.Errorf("failed to list users assigned to role: %w", err)
	}
	if len(ids) == 0 {
		return ids, nil
	}
	var locked []string
	if err := tx.Table("users").
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id IN ?", ids).
		Order("id").
		Pluck("id", &locked).Error; err != nil {
		return nil, fmt.Errorf("failed to lock users assigned to role: %w", err)
	}
	return ids, nil
}

func (s *RoleService) UpdateRole(ctx context.Context, id, name string, description *string, permissions []string) (*Role, error) {
	input := roletypes.UpdateRole{Name: name, Description: description, Permissions: permissions}
	if err := normalization.Normalize(&input); err != nil {
		return nil, err
	}
	if err := validatePermissionsInternal(permissions); err != nil {
		return nil, err
	}
	var out Role
	var affected []string
	err := dbutil.WithTx(ctx, s.db.DB, func(tx *gorm.DB) error {
		if err := s.LockGlobalAdminGuard(tx); err != nil {
			return err
		}
		var existing Role
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", id).First(&existing).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return common.Classify(common.ErrRoleNotFound, errors.New("Role not found"))
			}
			return fmt.Errorf("failed to load role: %w", err)
		}
		if existing.BuiltIn {
			return common.Classify(common.ErrRoleBuiltIn, errors.New("Built-in role cannot be modified")) //nolint:staticcheck // Preserve the existing error message.
		}
		if input.Name != existing.Name {
			var conflict int64
			if err := tx.Model(&Role{}).Where("name = ? AND id <> ?", input.Name, id).Count(&conflict).Error; err != nil {
				return fmt.Errorf("failed to check role name uniqueness: %w", err)
			}
			if conflict > 0 {
				return common.Classify(common.ErrRoleNameTaken, errors.New("Role name already in use"))
			}
		}
		ids, err := lockAssignedUserRows(tx, id)
		if err != nil {
			return err
		}
		affected = ids
		existing.Name = input.Name
		existing.Description = input.Description
		existing.Permissions = permissions
		if updateRoleErr := tx.Save(&existing).Error; updateRoleErr != nil {
			return fmt.Errorf("failed to update role: %w", updateRoleErr)
		}
		out = existing
		// Narrowing a role can demote every holder, so re-check the global-admin guard.
		if len(ids) == 0 {
			return nil
		}
		return s.AssertGlobalAdminExists(ctx, tx)
	})
	if err != nil {
		return nil, err
	}
	for _, userID := range affected {
		s.InvalidateUser(userID)
	}
	return &out, nil
}

func (s *RoleService) DeleteRole(ctx context.Context, id string) error {
	var affected []string
	err := dbutil.WithTx(ctx, s.db.DB, func(tx *gorm.DB) error {
		if err := s.LockGlobalAdminGuard(tx); err != nil {
			return err
		}
		var existing Role
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", id).First(&existing).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return common.Classify(common.ErrRoleNotFound, errors.New("Role not found"))
			}
			return fmt.Errorf("failed to load role: %w", err)
		}
		if existing.BuiltIn {
			return common.Classify(common.ErrRoleBuiltIn, errors.New("Built-in role cannot be modified")) //nolint:staticcheck // Preserve the existing error message.
		}

		var federatedCredentialCount int64
		if countErr := tx.Table("federated_credentials").Where("role_id = ?", id).Count(&federatedCredentialCount).Error; countErr != nil {
			return fmt.Errorf("failed to check federated credentials for role: %w", countErr)
		}
		if federatedCredentialCount > 0 {
			return common.ErrRoleUsedByFederatedCredential
		}

		ids, err := lockAssignedUserRows(tx, id)
		if err != nil {
			return err
		}
		affected = ids
		if deleteRoleErr := tx.Delete(&Role{}, "id = ?", id).Error; deleteRoleErr != nil {
			return fmt.Errorf("failed to delete role: %w", deleteRoleErr)
		}
		if len(ids) == 0 {
			return nil
		}
		return s.AssertGlobalAdminExists(ctx, tx)
	})
	if err != nil {
		return err
	}
	// Invalidate after commit so a concurrent cache miss cannot refill stale permissions.
	for _, uid := range affected {
		s.InvalidateUser(uid)
	}
	return nil
}

// CountUsersAssignedToRole counts distinct users assigned to roleID in any source or scope.
func (s *RoleService) CountUsersAssignedToRole(ctx context.Context, roleID string) (int, error) {
	var count int64
	if err := s.db.WithContext(ctx).
		Model(&UserRoleAssignment{}).
		Distinct("user_id").
		Where("role_id = ?", roleID).
		Count(&count).Error; err != nil {
		return 0, fmt.Errorf("failed to count users assigned to role: %w", err)
	}
	return int(count), nil
}

func (s *RoleService) ListUserAssignments(ctx context.Context, userID string) ([]UserRoleAssignment, error) {
	var out []UserRoleAssignment
	if err := s.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("source ASC, role_id ASC").
		Find(&out).Error; err != nil {
		return nil, fmt.Errorf("failed to list user assignments: %w", err)
	}
	return out, nil
}

// ReplaceUserAssignments replaces the user's assignments for one source and enforces the global-admin guard.
func (s *RoleService) ReplaceUserAssignments(ctx context.Context, userID, source string, desired []UserRoleAssignment) error {
	seen := make(map[string]struct{}, len(desired))
	desired = slices.DeleteFunc(desired, func(a UserRoleAssignment) bool {
		key := assignmentKeyInternal(a)
		if _, dup := seen[key]; dup {
			return true
		}
		seen[key] = struct{}{}
		return false
	})
	for i := range desired {
		desired[i].UserID = userID
		desired[i].Source = source
	}
	err := dbutil.WithTx(ctx, s.db.DB, func(tx *gorm.DB) error {
		if err := s.LockGlobalAdminGuard(tx); err != nil {
			return err
		}
		// Lock the user row so assignment swaps serialize with user-domain privilege checks.
		var locked struct{ ID string }
		if err := tx.Table("users").
			Select("id").
			Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", userID).
			Take(&locked).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return common.ErrUserNotFound
			}
			return fmt.Errorf("failed to lock user for assignment update: %w", err)
		}
		if err := validateAssignmentsExistInternal(tx, desired); err != nil {
			return err
		}
		if err := tx.Where("user_id = ? AND source = ?", userID, source).
			Delete(&UserRoleAssignment{}).Error; err != nil {
			return fmt.Errorf("failed to clear %s assignments: %w", source, err)
		}
		if len(desired) > 0 {
			if err := tx.Create(&desired).Error; err != nil {
				return fmt.Errorf("failed to insert %s assignments: %w", source, err)
			}
		}
		return s.AssertGlobalAdminExists(ctx, tx)
	})
	if err != nil {
		return err
	}
	s.InvalidateUser(userID)
	return nil
}

func assignmentKeyInternal(a UserRoleAssignment) string {
	envID := ""
	if a.EnvironmentID != nil {
		envID = *a.EnvironmentID
	}
	return a.RoleID + "\x00" + envID
}

// validateAssignmentsExistInternal returns ErrInvalidRoleAssignment for the first missing role or environment.
func validateAssignmentsExistInternal(tx *gorm.DB, desired []UserRoleAssignment) error {
	roleIDSet := make(map[string]struct{}, len(desired))
	envIDSet := make(map[string]struct{}, len(desired))
	for _, a := range desired {
		roleIDSet[a.RoleID] = struct{}{}
		if a.EnvironmentID != nil {
			envIDSet[*a.EnvironmentID] = struct{}{}
		}
	}

	if len(roleIDSet) > 0 {
		roleIDs := slices.Collect(maps.Keys(roleIDSet))
		var found []string
		if err := tx.Model(&Role{}).Where("id IN ?", roleIDs).Pluck("id", &found).Error; err != nil {
			return fmt.Errorf("failed to verify role ids: %w", err)
		}
		foundSet := make(map[string]struct{}, len(found))
		for _, id := range found {
			foundSet[id] = struct{}{}
		}
		for id := range roleIDSet {
			if _, ok := foundSet[id]; !ok {
				return common.Classify(common.ErrInvalidRoleAssignment, fmt.Errorf("invalid role assignment: role %q does not exist", id))
			}
		}
	}

	if len(envIDSet) > 0 {
		envIDs := slices.Collect(maps.Keys(envIDSet))
		var found []string
		if err := tx.Table("environments").Where("id IN ?", envIDs).Pluck("id", &found).Error; err != nil {
			return fmt.Errorf("failed to verify environment ids: %w", err)
		}
		foundSet := make(map[string]struct{}, len(found))
		for _, id := range found {
			foundSet[id] = struct{}{}
		}
		for id := range envIDSet {
			if _, ok := foundSet[id]; !ok {
				return common.Classify(common.ErrInvalidRoleAssignment, fmt.Errorf("invalid role assignment: environment %q does not exist", id))
			}
		}
	}
	return nil
}

func (s *RoleService) CountGlobalAdmins(ctx context.Context, tx *gorm.DB, excludedUserID string) (int, error) {
	type globalPermissionRow struct {
		UserID      string `gorm:"column:user_id"`
		Permissions string `gorm:"column:permissions"`
	}

	var rows []globalPermissionRow
	query := tx.WithContext(ctx).
		Table("users AS u").
		Select("u.id AS user_id, r.permissions AS permissions").
		Joins("INNER JOIN user_role_assignments ura ON ura.user_id = u.id AND ura.environment_id IS NULL").
		Joins("INNER JOIN roles r ON r.id = ura.role_id").
		Where("u.is_service_account = ?", false)
	if strings.TrimSpace(excludedUserID) != "" {
		query = query.Where("u.id <> ?", excludedUserID)
	}
	if err := query.Scan(&rows).Error; err != nil {
		return 0, fmt.Errorf("failed to list global role permissions for admin count: %w", err)
	}

	permissionsByUser := make(map[string]*authz.PermissionSet, len(rows))
	for _, r := range rows {
		ps := permissionsByUser[r.UserID]
		if ps == nil {
			ps = authz.NewPermissionSet()
			permissionsByUser[r.UserID] = ps
		}
		perms, err := decodePermissionsJSONInternal(r.Permissions)
		if err != nil {
			return 0, fmt.Errorf("failed to decode role permissions: %w", err)
		}
		ps.AddGlobal(perms...)
	}

	count := 0
	for _, ps := range permissionsByUser {
		if ps.IsGlobalAdmin() {
			count++
		}
	}
	return count, nil
}

// ResolvePermissions returns the user's effective permissions, cached for permissionCacheTTL.
func (s *RoleService) ResolvePermissions(ctx context.Context, userID string) (*authz.PermissionSet, error) {
	if userID == "" {
		return authz.NewPermissionSet(), nil
	}
	if ps, ok, _ := s.userCache.Get(userID); ok {
		return ps, nil
	}
	gen := s.userCacheGen.Load()
	ps, err := s.ResolveUserPermissionsInDB(ctx, s.db.WithContext(ctx), userID)
	if err != nil {
		return nil, err
	}
	s.cacheFillMu.Lock()
	if s.userCacheGen.Load() == gen {
		s.userCache.Set(userID, ps)
	}
	s.cacheFillMu.Unlock()
	return ps, nil
}

// ResolveUserPermissionsInDB resolves the user's permissions in tx.
func (s *RoleService) ResolveUserPermissionsInDB(_ context.Context, tx *gorm.DB, userID string) (*authz.PermissionSet, error) {
	// Scan permissions as a raw string; GORM cannot wire StringSlice on anonymous structs.
	type row struct {
		Permissions   string  `gorm:"column:permissions"`
		EnvironmentID *string `gorm:"column:environment_id"`
	}
	var rows []row
	if err := tx.Table("user_role_assignments AS ura").
		Select("r.permissions AS permissions, ura.environment_id AS environment_id").
		Joins("INNER JOIN roles r ON r.id = ura.role_id").
		Where("ura.user_id = ?", userID).
		Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("failed to resolve user permissions: %w", err)
	}
	ps := authz.NewPermissionSet()
	for _, r := range rows {
		perms, err := decodePermissionsJSONInternal(r.Permissions)
		if err != nil {
			return nil, fmt.Errorf("failed to decode role permissions: %w", err)
		}
		if r.EnvironmentID == nil {
			ps.AddGlobal(perms...)
		} else {
			ps.AddEnv(*r.EnvironmentID, perms...)
		}
	}
	return ps, nil
}

// decodePermissionsJSONInternal parses the JSON roles.permissions column.
func decodePermissionsJSONInternal(raw string) ([]string, error) {
	if raw == "" || raw == "[]" {
		return nil, nil
	}
	var out []string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ResolveExecutionPermissions revalidates durable work against current users and keys, bypassing caches.
func (s *RoleService) ResolveExecutionPermissions(ctx context.Context, userID, keyID string) (*authz.PermissionSet, error) {
	if userID == "" {
		return nil, errors.New("requesting user unavailable")
	}
	db := s.db.WithContext(ctx)
	var user struct{ ID string }
	if err := db.Table("users").Select("id").Where("id = ?", userID).Take(&user).Error; err != nil {
		return nil, fmt.Errorf("failed to load requesting user: %w", err)
	}
	if keyID == "" {
		return s.ResolveUserPermissionsInDB(ctx, db, userID)
	}
	var key struct {
		ID            string
		Kind          string
		UserID        *string
		EnvironmentID *string
		ExpiresAt     *time.Time
	}
	if err := db.Table("api_keys").Select("id, kind, user_id, environment_id, expires_at").Where("id = ?", keyID).Take(&key).Error; err != nil {
		return nil, fmt.Errorf("failed to load requesting API key: %w", err)
	}
	if key.UserID == nil || *key.UserID != userID || (key.ExpiresAt != nil && !key.ExpiresAt.After(time.Now())) {
		return nil, errors.New("requesting API key is no longer valid")
	}
	var permissions *authz.PermissionSet
	var err error
	switch key.Kind {
	case "personal":
		permissions, err = s.ResolveUserPermissionsInDB(ctx, db, userID)
	case "scoped":
		permissions, err = s.resolveApiKeyPermissionsInDBInternal(ctx, db, keyID)
	default:
		return nil, errors.New("requesting API key kind is invalid")
	}
	if err != nil {
		return nil, err
	}
	if key.EnvironmentID == nil {
		return permissions, nil
	}
	if *key.EnvironmentID == "" {
		return nil, errors.New("requesting API key environment scope is invalid")
	}
	restricted := authz.NewPermissionSet()
	for permission := range permissions.Global {
		restricted.AddEnv(*key.EnvironmentID, permission)
	}
	for permission := range permissions.PerEnv[*key.EnvironmentID] {
		restricted.AddEnv(*key.EnvironmentID, permission)
	}
	return restricted, nil
}

// ResolveApiKeyPermissions returns the API key's permissions, cached per key.
func (s *RoleService) ResolveApiKeyPermissions(ctx context.Context, apiKeyID string) (*authz.PermissionSet, error) {
	if ps, ok, _ := s.apiKeyCache.Get(apiKeyID); ok {
		return ps, nil
	}
	gen := s.apiKeyCacheGen.Load()
	permissions, err := s.resolveApiKeyPermissionsInDBInternal(ctx, s.db.WithContext(ctx), apiKeyID)
	if err != nil {
		return nil, err
	}
	s.cacheFillMu.Lock()
	if s.apiKeyCacheGen.Load() == gen {
		s.apiKeyCache.Set(apiKeyID, permissions)
	}
	s.cacheFillMu.Unlock()
	return permissions, nil
}

func (s *RoleService) resolveApiKeyPermissionsInDBInternal(ctx context.Context, db *gorm.DB, apiKeyID string) (*authz.PermissionSet, error) {
	var key struct{ UserID *string }
	if err := db.Table("api_keys").Select("user_id").Where("id = ?", apiKeyID).Take(&key).Error; err != nil {
		return nil, fmt.Errorf("failed to load api key owner: %w", err)
	}
	if kit.FromPtr(key.UserID) == "" {
		return authz.NewPermissionSet(), nil
	}
	owner, err := s.ResolveUserPermissionsInDB(ctx, db, *key.UserID)
	if err != nil {
		return nil, err
	}
	var grants []ApiKeyPermission
	if findGrantsErr := db.Where("api_key_id = ?", apiKeyID).Find(&grants).Error; findGrantsErr != nil {
		return nil, fmt.Errorf("failed to resolve api key permissions: %w", findGrantsErr)
	}
	permissions := authz.NewPermissionSet()
	for _, grant := range grants {
		switch {
		case grant.EnvironmentID != nil:
			if owner.Allows(grant.Permission, *grant.EnvironmentID) {
				permissions.AddEnv(*grant.EnvironmentID, grant.Permission)
			}
		case owner.Allows(grant.Permission, ""):
			permissions.AddGlobal(grant.Permission)
		default:
			for envID, envPermissions := range owner.PerEnv {
				if _, ok := envPermissions[grant.Permission]; ok {
					permissions.AddEnv(envID, grant.Permission)
				}
			}
		}
	}
	return permissions, nil
}

// SetApiKeyPermissions atomically replaces the API key's permission rows.
func (s *RoleService) SetApiKeyPermissions(ctx context.Context, apiKeyID string, grants []ApiKeyPermission) error {
	err := dbutil.WithTx(ctx, s.db.DB, func(tx *gorm.DB) error {
		return s.SetApiKeyPermissionsInDB(ctx, tx, apiKeyID, grants)
	})
	if err != nil {
		return err
	}
	s.InvalidateApiKey(apiKeyID)
	return nil
}

// SetApiKeyPermissionsInDB replaces the API key's permission rows in tx.
func (s *RoleService) SetApiKeyPermissionsInDB(ctx context.Context, tx *gorm.DB, apiKeyID string, grants []ApiKeyPermission) error {
	for i := range grants {
		grants[i].ApiKeyID = apiKeyID
	}
	if err := tx.WithContext(ctx).Where("api_key_id = ?", apiKeyID).Delete(&ApiKeyPermission{}).Error; err != nil {
		return fmt.Errorf("failed to clear api key permissions: %w", err)
	}
	if len(grants) > 0 {
		if err := tx.WithContext(ctx).Create(&grants).Error; err != nil {
			return fmt.Errorf("failed to insert api key permissions: %w", err)
		}
	}
	return nil
}

func (s *RoleService) ListOidcMappings(ctx context.Context) ([]OidcRoleMapping, error) {
	var out []OidcRoleMapping
	if err := s.db.WithContext(ctx).Order("claim_value, role_id").Find(&out).Error; err != nil {
		return nil, fmt.Errorf("failed to list oidc mappings: %w", err)
	}
	return out, nil
}

func (s *RoleService) GetOidcMapping(ctx context.Context, id string) (*OidcRoleMapping, error) {
	return dbutil.FirstWhere[OidcRoleMapping](ctx, s.db.DB, common.Classify(common.ErrOidcMappingNotFound, errors.New("OIDC role mapping not found")), "id = ?", id)
}

func (s *RoleService) CreateOidcMapping(ctx context.Context, claimValue, roleID string, environmentID *string) (*OidcRoleMapping, error) {
	claimValue = strings.TrimSpace(claimValue)
	roleID = strings.TrimSpace(roleID)
	if claimValue == "" {
		return nil, errors.New("claim value is required")
	}
	if roleID == "" {
		return nil, errors.New("role id is required")
	}
	var mapping OidcRoleMapping
	err := dbutil.WithTx(ctx, s.db.DB, func(tx *gorm.DB) error {
		if err := validateAssignmentsExistInternal(tx, []UserRoleAssignment{{RoleID: roleID, EnvironmentID: environmentID}}); err != nil {
			return err
		}
		mapping = OidcRoleMapping{
			ClaimValue:    claimValue,
			RoleID:        roleID,
			EnvironmentID: environmentID,
			Source:        OidcMappingSourceManual,
		}
		if err := tx.Create(&mapping).Error; err != nil {
			return fmt.Errorf("failed to create oidc mapping: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &mapping, nil
}

func (s *RoleService) UpdateOidcMapping(ctx context.Context, id, claimValue, roleID string, environmentID *string) (*OidcRoleMapping, error) {
	claimValue = strings.TrimSpace(claimValue)
	roleID = strings.TrimSpace(roleID)
	if claimValue == "" {
		return nil, errors.New("claim value is required")
	}
	if roleID == "" {
		return nil, errors.New("role id is required")
	}
	var out OidcRoleMapping
	err := dbutil.WithTx(ctx, s.db.DB, func(tx *gorm.DB) error {
		var existing OidcRoleMapping
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", id).First(&existing).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return common.Classify(common.ErrOidcMappingNotFound, errors.New("OIDC role mapping not found"))
			}
			return fmt.Errorf("failed to load mapping: %w", err)
		}
		if existing.Source == OidcMappingSourceEnv {
			return common.Classify(common.ErrOidcMappingEnvManaged, errors.New("OIDC role mapping is managed by OIDC_ROLE_MAPPINGS and cannot be edited at runtime"))
		}
		if err := validateAssignmentsExistInternal(tx, []UserRoleAssignment{{RoleID: roleID, EnvironmentID: environmentID}}); err != nil {
			return err
		}
		existing.ClaimValue = claimValue
		existing.RoleID = roleID
		existing.EnvironmentID = environmentID
		if err := tx.Save(&existing).Error; err != nil {
			return fmt.Errorf("failed to update mapping: %w", err)
		}
		out = existing
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (s *RoleService) DeleteOidcMapping(ctx context.Context, id string) error {
	return dbutil.WithTx(ctx, s.db.DB, func(tx *gorm.DB) error {
		var existing OidcRoleMapping
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", id).First(&existing).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return common.Classify(common.ErrOidcMappingNotFound, errors.New("OIDC role mapping not found"))
			}
			return fmt.Errorf("failed to load mapping: %w", err)
		}
		if existing.Source == OidcMappingSourceEnv {
			return common.Classify(common.ErrOidcMappingEnvManaged, errors.New("OIDC role mapping is managed by OIDC_ROLE_MAPPINGS and cannot be edited at runtime"))
		}
		if err := tx.Delete(&OidcRoleMapping{}, "id = ?", id).Error; err != nil {
			return fmt.Errorf("failed to delete mapping: %w", err)
		}
		return nil
	})
}

// ReconcileEnvOidcMappings replaces source='env' OIDC mappings with rawSpec at boot.
// Empty rawSpec leaves rows alone; manual rows are never touched.
func (s *RoleService) ReconcileEnvOidcMappings(ctx context.Context, rawSpec string) error {
	rawSpec = strings.TrimSpace(rawSpec)
	if rawSpec == "" {
		return nil
	}
	var specs []roletypes.OidcRoleMappingSpec
	if err := json.Unmarshal([]byte(rawSpec), &specs); err != nil {
		return fmt.Errorf("invalid OIDC_ROLE_MAPPINGS JSON: %w", err)
	}
	for i, sp := range specs {
		if strings.TrimSpace(sp.ClaimValue) == "" {
			return fmt.Errorf("OIDC_ROLE_MAPPINGS[%d]: claimValue is required", i)
		}
		if strings.TrimSpace(sp.RoleID) == "" {
			return fmt.Errorf("OIDC_ROLE_MAPPINGS[%d]: roleId is required", i)
		}
	}

	return dbutil.WithTx(ctx, s.db.DB, func(tx *gorm.DB) error {
		// Verify roles inside the tx so a concurrent role delete cannot race past it.
		roleIDs := make([]string, 0, len(specs))
		for _, sp := range specs {
			roleIDs = append(roleIDs, sp.RoleID)
		}
		var existingIDs []string
		if err := tx.Model(&Role{}).Where("id IN ?", roleIDs).Pluck("id", &existingIDs).Error; err != nil {
			return fmt.Errorf("OIDC_ROLE_MAPPINGS: failed to verify roles: %w", err)
		}
		for i, sp := range specs {
			if !slices.Contains(existingIDs, sp.RoleID) {
				return fmt.Errorf("OIDC_ROLE_MAPPINGS[%d]: role %q does not exist", i, sp.RoleID)
			}
		}

		if err := tx.Where("source = ?", OidcMappingSourceEnv).Delete(&OidcRoleMapping{}).Error; err != nil {
			return fmt.Errorf("failed to clear env-managed mappings: %w", err)
		}
		if len(specs) == 0 {
			slog.InfoContext(ctx, "OIDC_ROLE_MAPPINGS reconciled (empty)", "envManagedCount", 0)
			return nil
		}
		rows := make([]OidcRoleMapping, len(specs))
		for i, sp := range specs {
			rows[i] = OidcRoleMapping{
				ClaimValue:    sp.ClaimValue,
				RoleID:        sp.RoleID,
				EnvironmentID: sp.EnvironmentID,
				Source:        OidcMappingSourceEnv,
			}
		}
		if err := tx.Create(&rows).Error; err != nil {
			return fmt.Errorf("failed to insert env-managed mappings: %w", err)
		}
		slog.InfoContext(ctx, "OIDC_ROLE_MAPPINGS reconciled", "envManagedCount", len(rows))
		return nil
	})
}

// InvalidateUser drops the cached permissions for one user.
func (s *RoleService) InvalidateUser(userID string) {
	s.cacheFillMu.Lock()
	defer s.cacheFillMu.Unlock()
	s.userCacheGen.Add(1)
	s.userCache.Delete(userID)
	s.apiKeyCacheGen.Add(1)
	s.apiKeyCache.Purge()
}

func (s *RoleService) Stop() {
	s.userCache.StopJanitor()
	s.apiKeyCache.StopJanitor()
}

// InvalidateApiKey drops the cached permissions for one API key.
func (s *RoleService) InvalidateApiKey(apiKeyID string) {
	s.cacheFillMu.Lock()
	defer s.cacheFillMu.Unlock()
	s.apiKeyCacheGen.Add(1)
	s.apiKeyCache.Delete(apiKeyID)
}

func validatePermissionsInternal(perms []string) error {
	for _, p := range perms {
		if !authz.IsKnownPermission(p) {
			return common.Classify(common.ErrUnknownPermission, errors.New("Unknown permission: "+

				p))
		}
	}
	return nil
}

// ValidatePermissionsAgainstCaller rejects unknown permissions, then any the caller lacks globally.
// Env-scoped grants are insufficient because roles can later be assigned globally.
func (s *RoleService) ValidatePermissionsAgainstCaller(caller *authz.PermissionSet, desired []string) error {
	if err := validatePermissionsInternal(desired); err != nil {
		return err
	}
	return validatePermissionSetAgainstCallerInternal(caller, desired, "")
}

// ValidateRoleAssignmentAgainstCaller rejects a role whose permissions the caller lacks at that scope.
func (s *RoleService) ValidateRoleAssignmentAgainstCaller(ctx context.Context, caller *authz.PermissionSet, roleID string, environmentID *string) error {
	role, err := s.GetRole(ctx, roleID)
	if err != nil {
		return err
	}

	desired := []string(role.Permissions)
	if validatePermissionsErr := validatePermissionsInternal(desired); validatePermissionsErr != nil {
		return validatePermissionsErr
	}
	return validatePermissionSetAgainstCallerInternal(caller, desired, mo.PointerToOption(environmentID).OrEmpty())
}

func validatePermissionSetAgainstCallerInternal(caller *authz.PermissionSet, desired []string, environmentID string) error {
	if caller == nil {
		if len(desired) == 0 {
			return nil
		}
		return common.Classify(common.ErrRolePermissionEscalation, errors.New("cannot grant a permission you do not hold: "+

			desired[0]))
	}
	if caller.Sudo {
		return nil
	}
	for _, p := range desired {
		if !caller.Allows(p, environmentID) {
			return common.Classify(common.ErrRolePermissionEscalation, errors.New("cannot grant a permission you do not hold: "+

				p))
		}
	}
	return nil
}
