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

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/kv"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/dbutil"
	roletypes "github.com/getarcaneapp/arcane/types/v2/role"
	"github.com/samber/hot"
	"github.com/samber/mo"
	"go.getarcane.app/kit/normalization"
	kit "go.getarcane.app/kit/pkg"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	permissionCacheTTL             = 60 * time.Second
	legacyRoleBackfillCompletedKey = "migration.legacy_user_roles.v1.completed"
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

// BackfillLegacyRoleAssignments converts pre-RBAC users.roles into global assignments once, gated by a kv marker committed with the rows.
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
		if err := tx.Table("users").Select("id, roles").
			Where("NOT EXISTS (SELECT 1 FROM user_role_assignments ura WHERE ura.user_id = users.id)").
			Scan(&rows).Error; err != nil {
			return fmt.Errorf("failed to read legacy users.roles for backfill: %w", err)
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

// legacyRolesContainsAdminInternal reports whether a legacy users.roles JSON value names "admin"; anything else was a regular user.
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

// AssertGlobalAdminExists returns common.ErrNoGlobalAdminRemains if zero
// non-service users resolve to global administrator permissions. Called at boot
// after the backfill migration; also called from inside mutation paths.
func (s *RoleService) AssertGlobalAdminExists(ctx context.Context) error {
	count, err := s.countEffectiveGlobalAdminsInternal(ctx, s.db.WithContext(ctx), "")
	if err != nil {
		return err
	}
	if count == 0 {
		return common.Classify(common.ErrNoGlobalAdminRemains, errors.New("At least one user must retain a global Admin role assignment")) //nolint:staticcheck // Preserve the existing error message.
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

// lockAssignedUserRowsInternal locks the user rows of everyone currently
// assigned to roleID, in deterministic id order. Role-definition writes call
// this so they serialize with user-domain mutation transactions, whose in-transaction
// privilege checks lock the same rows: without it a role edit could change a
// holder's effective admin status between that check and the mutation commit.
// Returns the affected user ids for post-commit cache invalidation.
func lockAssignedUserRowsInternal(tx *gorm.DB, roleID string) ([]string, error) {
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
	if err := tx.Model(&common.User{}).
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
		ids, err := lockAssignedUserRowsInternal(tx, id)
		if err != nil {
			return err
		}
		affected = ids
		existing.Name = input.Name
		existing.Description = input.Description
		existing.Permissions = permissions
		if err := tx.Save(&existing).Error; err != nil {
			return fmt.Errorf("failed to update role: %w", err)
		}
		out = existing
		return nil
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

		// Collect affected users before deleting the role.
		ids, err := lockAssignedUserRowsInternal(tx, id)
		if err != nil {
			return err
		}
		affected = ids
		if err := tx.Delete(&Role{}, "id = ?", id).Error; err != nil {
			return fmt.Errorf("failed to delete role: %w", err)
		}
		return nil
	})
	if err != nil {
		return err
	}
	// Invalidate caches AFTER the transaction commits so a concurrent
	// cache-miss cannot re-populate with stale data from the not-yet-visible
	// delete. Consistent with UpdateRole / SetUserAssignments.
	for _, uid := range affected {
		s.InvalidateUser(uid)
	}
	return nil
}

// CountUsersAssignedToRole returns how many distinct users hold an assignment
// to the given role (any source, any environment scope).
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

// ---------- User role assignments ----------

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

// replaceUserAssignmentsForSourceInternal replaces the user's assignments for a
// single source (manual or oidc), leaving other sources untouched. References
// are validated inside the tx so a concurrent role/env delete yields a typed
// error (→ 400) rather than an opaque FK violation, and the global-admin guard
// is enforced before commit. Shared by SetUserAssignments and
// ReplaceOidcAssignments.
func (s *RoleService) replaceUserAssignmentsForSourceInternal(ctx context.Context, userID, source string, desired []UserRoleAssignment) error {
	for i := range desired {
		desired[i].UserID = userID
		desired[i].Source = source
	}
	err := dbutil.WithTx(ctx, s.db.DB, func(tx *gorm.DB) error {
		// Lock the target's user row so assignment swaps serialize with
		// user-domain update/delete transactions, whose in-transaction privilege
		// checks lock the same row.
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", userID).
			First(&common.User{}).Error; err != nil {
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
		count, err := s.countEffectiveGlobalAdminsInternal(ctx, tx, "")
		if err != nil {
			return err
		}
		if count == 0 {
			return common.Classify(common.ErrNoGlobalAdminRemains, errors.New("At least one user must retain a global Admin role assignment")) //nolint:staticcheck // Preserve the existing error message.
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.InvalidateUser(userID)
	return nil
}

// SetUserAssignments replaces the user's source='manual' assignments with the
// given desired set. Source='oidc' rows are preserved (use
// ReplaceOidcAssignments for those). Enforces the global-admin guard.
func (s *RoleService) SetUserAssignments(ctx context.Context, userID string, desired []UserRoleAssignment) error {
	return s.replaceUserAssignmentsForSourceInternal(ctx, userID, RoleAssignmentSourceManual, desired)
}

// validateAssignmentsExistInternal verifies every distinct RoleID and
// EnvironmentID referenced by `desired` exists in the database. Returns the
// first missing reference wrapped in an InvalidRoleAssignmentError so the
// handler can map it to a 400 with a descriptive message.
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

// ReplaceOidcAssignments replaces the user's source='oidc' assignments. Manual
// assignments are untouched. An OIDC mapping referencing a since-deleted role or
// environment fails with a typed error; the caller logs and continues login so
// the user simply receives no OIDC-derived assignments. Enforces the
// global-admin guard after the swap.
func (s *RoleService) ReplaceOidcAssignments(ctx context.Context, userID string, desired []UserRoleAssignment) error {
	return s.replaceUserAssignmentsForSourceInternal(ctx, userID, RoleAssignmentSourceOidc, desired)
}

// CountGlobalAdminsExcludingUser returns the number of non-service users (other
// than excludedUserID) whose resolved global permissions satisfy IsGlobalAdmin.
// Used as the authoritative check for "removing this user / demoting this
// assignment would leave the system with no admin."
func (s *RoleService) CountGlobalAdminsExcludingUser(ctx context.Context, excludedUserID string) (int, error) {
	return s.countEffectiveGlobalAdminsInternal(ctx, s.db.WithContext(ctx), excludedUserID)
}

func (s *RoleService) countEffectiveGlobalAdminsInternal(ctx context.Context, tx *gorm.DB, excludedUserID string) (int, error) {
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

// ---------- Permission resolution ----------

// ResolvePermissions returns the effective PermissionSet for a user, caching
// the result per-user for permissionCacheTTL.
func (s *RoleService) ResolvePermissions(ctx context.Context, user *common.User) (*authz.PermissionSet, error) {
	if user == nil {
		return authz.NewPermissionSet(), nil
	}
	if ps, ok, _ := s.userCache.Get(user.ID); ok {
		return ps, nil
	}
	gen := s.userCacheGen.Load()
	ps, err := s.ResolveUserPermissionsInDB(ctx, s.db.WithContext(ctx), user.ID)
	if err != nil {
		return nil, err
	}
	s.cacheFillMu.Lock()
	if s.userCacheGen.Load() == gen {
		s.userCache.Set(user.ID, ps)
	}
	s.cacheFillMu.Unlock()
	return ps, nil
}

// ResolveUserPermissionsInDB resolves permissions using the supplied transaction or database handle.
func (s *RoleService) ResolveUserPermissionsInDB(_ context.Context, tx *gorm.DB, userID string) (*authz.PermissionSet, error) {
	// Scan into raw string for the permissions JSON column to avoid GORM's
	// schema-introspection on anonymous local structs (which can't see the
	// type tags needed to wire database.StringSlice's Scanner).
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

// decodePermissionsJSONInternal parses the JSON-encoded `roles.permissions` column
// into a string slice. The column is `[]` for an empty role.
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

// ResolveExecutionPermissions revalidates durable work against current users,
// key ownership, expiry and permissions without using authentication caches.
func (s *RoleService) ResolveExecutionPermissions(ctx context.Context, userID, keyID string) (*authz.PermissionSet, error) {
	if userID == "" {
		return nil, errors.New("requesting user unavailable")
	}
	db := s.db.WithContext(ctx)
	var user common.User
	if err := db.Select("id").First(&user, "id = ?", userID).Error; err != nil {
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
		permissions, err = s.resolveApiKeyPermissionsInDBInternal(db, keyID)
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

// ResolveApiKeyPermissions returns the PermissionSet for an API key. Caches
// per-key. Falls back to an empty set (deny-all) if the key has no perms.
func (s *RoleService) ResolveApiKeyPermissions(ctx context.Context, apiKeyID string) (*authz.PermissionSet, error) {
	if ps, ok, _ := s.apiKeyCache.Get(apiKeyID); ok {
		return ps, nil
	}
	gen := s.apiKeyCacheGen.Load()
	permissions, err := s.resolveApiKeyPermissionsInDBInternal(s.db.WithContext(ctx), apiKeyID)
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

func (s *RoleService) resolveApiKeyPermissionsInDBInternal(db *gorm.DB, apiKeyID string) (*authz.PermissionSet, error) {
	var grants []ApiKeyPermission
	if err := db.Where("api_key_id = ?", apiKeyID).Find(&grants).Error; err != nil {
		return nil, fmt.Errorf("failed to resolve api key permissions: %w", err)
	}
	permissions := authz.NewPermissionSet()
	for _, grant := range grants {
		if grant.EnvironmentID == nil {
			permissions.AddGlobal(grant.Permission)
		} else {
			permissions.AddEnv(*grant.EnvironmentID, grant.Permission)
		}
	}
	return permissions, nil
}

// SetApiKeyPermissions replaces every permission row on the given API key
// atomically. Validation that the granted permissions don't exceed the
// creator's capabilities happens in the handler layer.
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

// SetApiKeyPermissionsInDB replaces permission rows using the supplied transaction or database handle.
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

// ---------- OIDC role mappings ----------

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
		if err := validateRoleIDsExistInternal(tx, []string{roleID}); err != nil {
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
		if err := validateRoleIDsExistInternal(tx, []string{roleID}); err != nil {
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

func validateRoleIDsExistInternal(tx *gorm.DB, roleIDs []string) error {
	if len(roleIDs) == 0 {
		return nil
	}
	roleIDSet := make(map[string]struct{}, len(roleIDs))
	for _, roleID := range roleIDs {
		if roleID == "" {
			return errors.New("role id is required")
		}
		roleIDSet[roleID] = struct{}{}
	}

	normalized := slices.Collect(maps.Keys(roleIDSet))
	var found []string
	if err := tx.Model(&Role{}).Where("id IN ?", normalized).Pluck("id", &found).Error; err != nil {
		return fmt.Errorf("failed to verify role ids: %w", err)
	}
	foundSet := make(map[string]struct{}, len(found))
	for _, id := range found {
		foundSet[id] = struct{}{}
	}
	for _, id := range normalized {
		if _, ok := foundSet[id]; !ok {
			return common.Classify(common.ErrInvalidRoleAssignment, fmt.Errorf("invalid role assignment: role %q does not exist", id))
		}
	}
	return nil
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

// ReconcileEnvOidcMappings replaces every source='env' row in oidc_role_mappings
// with the set declared by `rawSpec` (a JSON array of role.OidcRoleMappingSpec).
// Called once at boot. Behavior is declarative:
//
//   - rawSpec empty / unset → leaves DB rows alone (purely UI-managed mode).
//   - rawSpec is `[]` → wipes any previously-env-managed rows.
//   - rawSpec is a valid JSON array → upserts each spec, deletes stale env rows.
//
// Manual rows (source='manual') are never touched. Bad JSON or an unknown role
// ID returns an error so a misconfigured deployment fails loudly rather than
// silently dropping mappings.
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
		// Verify every referenced role exists. Done inside the tx so a concurrent
		// role delete can't race past this check.
		for i, sp := range specs {
			var count int64
			if err := tx.Model(&Role{}).Where("id = ?", sp.RoleID).Count(&count).Error; err != nil {
				return fmt.Errorf("OIDC_ROLE_MAPPINGS[%d]: failed to verify role: %w", i, err)
			}
			if count == 0 {
				return fmt.Errorf("OIDC_ROLE_MAPPINGS[%d]: role %q does not exist", i, sp.RoleID)
			}
		}

		// Declarative replace: drop every env-managed row, then insert the new
		// set. Manual rows are untouched.
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

// ---------- Cache helpers ----------

// InvalidateUser drops the cached PermissionSet for one user. Called from
// auth_service after a login that mutates assignments, and from any mutation
// path that doesn't already invalidate explicitly.
func (s *RoleService) InvalidateUser(userID string) {
	s.cacheFillMu.Lock()
	defer s.cacheFillMu.Unlock()
	s.userCacheGen.Add(1)
	s.userCache.Delete(userID)
}

// InvalidateApiKey drops the cached PermissionSet for one API key.
func (s *RoleService) InvalidateApiKey(apiKeyID string) {
	s.cacheFillMu.Lock()
	defer s.cacheFillMu.Unlock()
	s.apiKeyCacheGen.Add(1)
	s.apiKeyCache.Delete(apiKeyID)
}

// ---------- helpers ----------

func validatePermissionsInternal(perms []string) error {
	for _, p := range perms {
		if !authz.IsKnownPermission(p) {
			return common.Classify(common.ErrUnknownPermission, errors.New("Unknown permission: "+

				p))
		}
	}
	return nil
}

// ValidatePermissionsAgainstCaller rejects any permission in `desired` that the
// caller does not hold at global scope. Sudo callers (agent / env access
// tokens, bootstrap paths) bypass entirely. Holding a permission only inside a
// specific environment is intentionally insufficient: roles are reusable
// templates that can later be assigned globally, so an env-scoped grant must
// not let the caller mint a global-capable role.
//
// Unknown permission strings are rejected first with an UnknownPermissionError
// so a caller typo-ing a permission gets a descriptive 400 instead of a
// misleading 403 from the escalation guard below (which would always fire on
// an unknown perm because no PermissionSet contains it). This also gives the
// escalation loop a clean invariant: every perm reaching it is real.
//
// Callers should run this before persisting role permissions to defend against
// privilege escalation if the role mutation endpoints are ever exposed beyond
// global admins.
func (s *RoleService) ValidatePermissionsAgainstCaller(caller *authz.PermissionSet, desired []string) error {
	if err := validatePermissionsInternal(desired); err != nil {
		return err
	}
	return validatePermissionSetAgainstCallerInternal(caller, desired, "")
}

// ValidateRoleAssignmentAgainstCaller rejects assigning a role at the requested
// scope when the caller does not hold every permission in that role at that
// same scope.
func (s *RoleService) ValidateRoleAssignmentAgainstCaller(ctx context.Context, caller *authz.PermissionSet, roleID string, environmentID *string) error {
	role, err := s.GetRole(ctx, roleID)
	if err != nil {
		return err
	}

	desired := []string(role.Permissions)
	if err := validatePermissionsInternal(desired); err != nil {
		return err
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
