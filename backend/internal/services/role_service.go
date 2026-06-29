package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	pkgutils "github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/cache"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/dbutil"
	roletypes "github.com/getarcaneapp/arcane/types/v2/role"
)

// permissionCacheTTL bounds how long a resolved PermissionSet is reused
// before re-querying the DB. The service also invalidates entries explicitly
// on mutation paths, so this TTL is a safety net.
const permissionCacheTTL = 60 * time.Second

// RoleService owns role definitions, user role assignments, OIDC role
// mappings, and API key permissions. It resolves a caller's effective
// PermissionSet on demand and caches the result per-user / per-key for a
// short TTL to keep the hot path off the database.
type RoleService struct {
	db          *database.DB
	userCache   *cache.TTL[*authz.PermissionSet]
	apiKeyCache *cache.TTL[*authz.PermissionSet]
}

func NewRoleService(db *database.DB) *RoleService {
	return &RoleService{
		db:          db,
		userCache:   cache.NewTTL[*authz.PermissionSet](permissionCacheTTL),
		apiKeyCache: cache.NewTTL[*authz.PermissionSet](permissionCacheTTL),
	}
}

// ---------- Boot-time reconciliation & safety checks ----------

// EnsureBuiltInRoles overwrites the permission set on every built-in role to
// match the Go constants. Idempotent. Called at boot after migrations succeed.
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
			role := models.Role{
				BaseModel:   models.BaseModel{ID: id},
				Name:        spec.name,
				Description: new(spec.desc),
				Permissions: models.StringSlice(spec.perm),
				BuiltIn:     true,
			}
			if err := tx.Save(&role).Error; err != nil {
				return fmt.Errorf("failed to upsert built-in role %s: %w", id, err)
			}
		}
		return nil
	})
}

// BackfillLegacyRoleAssignments migrates the pre-RBAC users.roles JSON column
// into rows in user_role_assignments. Safe to call on every boot: a no-op once
// the column is gone.
//
// Users with "admin" in their legacy roles get a global Admin assignment;
// every other user gets a global Viewer assignment. The NULL environment_id
// lands the perms in PermissionSet.Global, which is what ps.Allows(perm, "")
// consults for org-level checks (list environments, read settings, list users,
// etc.) AND for env-scoped checks at the union step. Inserting per-environment
// viewer rows instead would lock non-admins out of the settings area entirely.
//
// Lives here (not as a SQL migration) so the column-existence check is trivial
// in Go and the same code path covers both postgres and sqlite. Idempotent via
// ON CONFLICT DO NOTHING on the (user_id, role_id, env) unique index, so a
// half-finished prior run can be safely retried.
func (s *RoleService) BackfillLegacyRoleAssignments(ctx context.Context) error {
	migrator := s.db.WithContext(ctx).Migrator()
	if !migrator.HasColumn("users", "roles") {
		return nil
	}

	type legacyUser struct {
		ID    string `gorm:"column:id"`
		Roles string `gorm:"column:roles"`
	}

	return dbutil.WithTx(ctx, s.db.DB, func(tx *gorm.DB) error {
		var rows []legacyUser
		if err := tx.Table("users").Select("id, roles").Scan(&rows).Error; err != nil {
			return fmt.Errorf("failed to read legacy users.roles for backfill: %w", err)
		}
		for _, u := range rows {
			roleID := authz.BuiltInRoleViewer
			if legacyRolesContainsAdminInternal(u.Roles) {
				roleID = authz.BuiltInRoleAdmin
			}
			assignment := models.UserRoleAssignment{
				UserID: u.ID,
				RoleID: roleID,
				Source: models.RoleAssignmentSourceManual,
			}
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&assignment).Error; err != nil {
				return fmt.Errorf("failed to backfill assignment for user %s: %w", u.ID, err)
			}
		}
		slog.InfoContext(ctx, "Backfilled legacy users.roles into user_role_assignments", "userCount", len(rows))
		return nil
	})
}

// legacyRolesContainsAdminInternal reports whether a pre-RBAC users.roles JSON
// value contains the literal "admin" (case-insensitive). Empty / null / malformed
// JSON yields false — treat as non-admin and assign Viewer.
func legacyRolesContainsAdminInternal(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "[]" || raw == "null" {
		return false
	}
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

// AssertGlobalAdminExists returns a *common.NoGlobalAdminRemainsError if zero
// non-service users resolve to global administrator permissions. Called at boot
// after the backfill migration; also called from inside mutation paths.
func (s *RoleService) AssertGlobalAdminExists(ctx context.Context) error {
	count, err := s.countEffectiveGlobalAdminsInternal(ctx, s.db.WithContext(ctx), "")
	if err != nil {
		return err
	}
	if count == 0 {
		return &common.NoGlobalAdminRemainsError{}
	}
	return nil
}

// BackfillApiKeyPermissions populates api_key_permissions for every existing
// API key whose row has no permissions yet. Each key inherits a snapshot of
// its owner's current effective permissions (scoped per the key's
// environment_id when set). Idempotent: skips if the table is non-empty.
// BackfillApiKeyPermissions ensures every ownerless (bootstrap) API key has
// its expected permission grants. Called once per boot.
//
// Per-key, not all-or-nothing: a single bootstrap key with zero grants is
// repaired even if other keys are already populated. This recovers env-
// bootstrap keys that pre-date the per-key permission feature, or that were
// created on a deployment where the original SetApiKeyPermissions call failed
// (e.g., the api_key_permissions table didn't exist yet).
//
// User-owned keys are deliberately skipped. A user-owned key with zero grants
// is an intentional "no access" state; rehydrating from the owner's effective
// permissions on every boot would clobber that. User keys are seeded at
// creation time by CreateApiKey instead.
func (s *RoleService) BackfillApiKeyPermissions(ctx context.Context) error {
	// Bootstrap-class keys we'll repair if they have zero grants:
	//   - user_id IS NULL → env-bootstrap keys.
	//   - managed_by IS NOT NULL → admin-static (ADMIN_STATIC_API_KEY) keys,
	//     which DO have a user_id but are infrastructure-managed and must
	//     always carry full perms.
	// Regular user-created keys are deliberately excluded — empty grants on
	// those are an intentional "no access" state we must not overwrite.
	var keys []models.ApiKey
	if err := s.db.WithContext(ctx).Where("user_id IS NULL OR managed_by IS NOT NULL").Find(&keys).Error; err != nil {
		return fmt.Errorf("failed to list bootstrap api keys for backfill: %w", err)
	}
	if len(keys) == 0 {
		return nil
	}

	return dbutil.WithTx(ctx, s.db.DB, func(tx *gorm.DB) error {
		for _, key := range keys {
			var existing int64
			if err := tx.Model(&models.ApiKeyPermission{}).Where("api_key_id = ?", key.ID).Count(&existing).Error; err != nil {
				return fmt.Errorf("failed to count permissions for api key %s: %w", key.ID, err)
			}
			if existing > 0 {
				continue
			}
			perms, err := s.backfillPermsForKeyInternal(ctx, tx, key)
			if err != nil {
				return err
			}
			for _, p := range perms {
				if err := tx.Create(&models.ApiKeyPermission{
					ApiKeyID:      key.ID,
					Permission:    p,
					EnvironmentID: key.EnvironmentID,
				}).Error; err != nil {
					return fmt.Errorf("failed to seed api key permission: %w", err)
				}
			}
			slog.InfoContext(ctx, "Backfilled missing permissions for bootstrap api key", "api_key_id", key.ID, "perm_count", len(perms), "env_id", key.EnvironmentID)
		}
		return nil
	})
}

func (s *RoleService) backfillPermsForKeyInternal(ctx context.Context, tx *gorm.DB, key models.ApiKey) ([]string, error) {
	// Static admin bootstrap keys (no owner, no env scope) get full access.
	if key.UserID == nil && key.EnvironmentID == nil {
		return authz.AllPermissions(), nil
	}
	// Environment bootstrap keys (key bound to a specific env, no owner) get
	// full access scoped to that environment via the auth bridge — replicate
	// that by granting all permissions scoped to that environment. Org-level
	// permissions land in PerEnv[envID] and are unreachable via org-level
	// checks (which always pass envID=""), so over-granting is harmless here.
	if key.UserID == nil && key.EnvironmentID != nil {
		return authz.AllPermissions(), nil
	}
	// Otherwise inherit the owner's current effective permissions.
	var owner models.User
	if err := tx.WithContext(ctx).Where("id = ?", *key.UserID).First(&owner).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to load api key owner: %w", err)
	}
	ps, err := s.resolveUserPermissionsInternal(ctx, tx, owner.ID)
	if err != nil {
		return nil, err
	}
	// Flatten ps into a deduplicated list of permissions for this key's scope.
	seen := make(map[string]struct{}, len(ps.Global))
	for p := range ps.Global {
		seen[p] = struct{}{}
	}
	if key.EnvironmentID != nil {
		if env, ok := ps.PerEnv[*key.EnvironmentID]; ok {
			for p := range env {
				seen[p] = struct{}{}
			}
		}
	}
	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	return out, nil
}

// ---------- Role CRUD ----------

func (s *RoleService) ListRoles(ctx context.Context, params pagination.QueryParams) ([]models.Role, pagination.Response, error) {
	var roles []models.Role
	query := s.db.WithContext(ctx).Model(&models.Role{})

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

func (s *RoleService) ListAllRoles(ctx context.Context) ([]models.Role, error) {
	var roles []models.Role
	if err := s.db.WithContext(ctx).Order("name").Find(&roles).Error; err != nil {
		return nil, fmt.Errorf("failed to list roles: %w", err)
	}
	return roles, nil
}

func (s *RoleService) GetRole(ctx context.Context, id string) (*models.Role, error) {
	return dbutil.FirstWhere[models.Role](ctx, s.db.DB, &common.RoleNotFoundError{}, "id = ?", id)
}

func (s *RoleService) CreateRole(ctx context.Context, name string, description *string, permissions []string) (*models.Role, error) {
	if strings.TrimSpace(name) == "" {
		return nil, errors.New("role name is required")
	}
	if err := validatePermissionsInternal(permissions); err != nil {
		return nil, err
	}
	role := &models.Role{
		Name:        name,
		Description: description,
		Permissions: models.StringSlice(permissions),
		BuiltIn:     false,
	}
	err := dbutil.WithTx(ctx, s.db.DB, func(tx *gorm.DB) error {
		var conflict int64
		if err := tx.Model(&models.Role{}).Where("name = ?", name).Count(&conflict).Error; err != nil {
			return fmt.Errorf("failed to check role name uniqueness: %w", err)
		}
		if conflict > 0 {
			return &common.RoleNameTakenError{}
		}
		return tx.Create(role).Error
	})
	if err != nil {
		return nil, err
	}
	return role, nil
}

func (s *RoleService) UpdateRole(ctx context.Context, id, name string, description *string, permissions []string) (*models.Role, error) {
	if err := validatePermissionsInternal(permissions); err != nil {
		return nil, err
	}
	var out models.Role
	err := dbutil.WithTx(ctx, s.db.DB, func(tx *gorm.DB) error {
		var existing models.Role
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", id).First(&existing).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return &common.RoleNotFoundError{}
			}
			return fmt.Errorf("failed to load role: %w", err)
		}
		if existing.BuiltIn {
			return &common.RoleBuiltInError{}
		}
		if name != existing.Name {
			var conflict int64
			if err := tx.Model(&models.Role{}).Where("name = ? AND id <> ?", name, id).Count(&conflict).Error; err != nil {
				return fmt.Errorf("failed to check role name uniqueness: %w", err)
			}
			if conflict > 0 {
				return &common.RoleNameTakenError{}
			}
		}
		existing.Name = name
		existing.Description = description
		existing.Permissions = models.StringSlice(permissions)
		if err := tx.Save(&existing).Error; err != nil {
			return fmt.Errorf("failed to update role: %w", err)
		}
		out = existing
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.invalidateUsersAssignedToInternal(ctx, id)
	return &out, nil
}

func (s *RoleService) DeleteRole(ctx context.Context, id string) error {
	var affected []string
	err := dbutil.WithTx(ctx, s.db.DB, func(tx *gorm.DB) error {
		var existing models.Role
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", id).First(&existing).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return &common.RoleNotFoundError{}
			}
			return fmt.Errorf("failed to load role: %w", err)
		}
		if existing.BuiltIn {
			return &common.RoleBuiltInError{}
		}
		// Collect users affected before the delete so we can invalidate their caches.
		if err := tx.Model(&models.UserRoleAssignment{}).
			Where("role_id = ?", id).
			Distinct("user_id").
			Pluck("user_id", &affected).Error; err != nil {
			return fmt.Errorf("failed to list affected users: %w", err)
		}
		if err := tx.Delete(&models.Role{}, "id = ?", id).Error; err != nil {
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
		s.userCache.Delete(uid)
	}
	return nil
}

// CountUsersAssignedToRole returns how many distinct users hold an assignment
// to the given role (any source, any environment scope).
func (s *RoleService) CountUsersAssignedToRole(ctx context.Context, roleID string) (int, error) {
	var count int64
	if err := s.db.WithContext(ctx).
		Model(&models.UserRoleAssignment{}).
		Distinct("user_id").
		Where("role_id = ?", roleID).
		Count(&count).Error; err != nil {
		return 0, fmt.Errorf("failed to count users assigned to role: %w", err)
	}
	return int(count), nil
}

// ---------- User role assignments ----------

func (s *RoleService) ListUserAssignments(ctx context.Context, userID string) ([]models.UserRoleAssignment, error) {
	var out []models.UserRoleAssignment
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
func (s *RoleService) replaceUserAssignmentsForSourceInternal(ctx context.Context, userID, source string, desired []models.UserRoleAssignment) error {
	for i := range desired {
		desired[i].UserID = userID
		desired[i].Source = source
	}
	err := dbutil.WithTx(ctx, s.db.DB, func(tx *gorm.DB) error {
		if err := validateAssignmentsExistInternal(tx, desired); err != nil {
			return err
		}
		if err := tx.Where("user_id = ? AND source = ?", userID, source).
			Delete(&models.UserRoleAssignment{}).Error; err != nil {
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
			return &common.NoGlobalAdminRemainsError{}
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.userCache.Delete(userID)
	return nil
}

// SetUserAssignments replaces the user's source='manual' assignments with the
// given desired set. Source='oidc' rows are preserved (use
// ReplaceOidcAssignments for those). Enforces the global-admin guard.
func (s *RoleService) SetUserAssignments(ctx context.Context, userID string, desired []models.UserRoleAssignment) error {
	return s.replaceUserAssignmentsForSourceInternal(ctx, userID, models.RoleAssignmentSourceManual, desired)
}

// validateAssignmentsExistInternal verifies every distinct RoleID and
// EnvironmentID referenced by `desired` exists in the database. Returns the
// first missing reference wrapped in an InvalidRoleAssignmentError so the
// handler can map it to a 400 with a descriptive message.
func validateAssignmentsExistInternal(tx *gorm.DB, desired []models.UserRoleAssignment) error {
	roleIDSet := make(map[string]struct{}, len(desired))
	envIDSet := make(map[string]struct{}, len(desired))
	for _, a := range desired {
		roleIDSet[a.RoleID] = struct{}{}
		if a.EnvironmentID != nil {
			envIDSet[*a.EnvironmentID] = struct{}{}
		}
	}

	if len(roleIDSet) > 0 {
		roleIDs := make([]string, 0, len(roleIDSet))
		for id := range roleIDSet {
			roleIDs = append(roleIDs, id)
		}
		var found []string
		if err := tx.Model(&models.Role{}).Where("id IN ?", roleIDs).Pluck("id", &found).Error; err != nil {
			return fmt.Errorf("failed to verify role ids: %w", err)
		}
		foundSet := make(map[string]struct{}, len(found))
		for _, id := range found {
			foundSet[id] = struct{}{}
		}
		for id := range roleIDSet {
			if _, ok := foundSet[id]; !ok {
				return &common.InvalidRoleAssignmentError{RoleID: id}
			}
		}
	}

	if len(envIDSet) > 0 {
		envIDs := make([]string, 0, len(envIDSet))
		for id := range envIDSet {
			envIDs = append(envIDs, id)
		}
		var found []string
		if err := tx.Model(&models.Environment{}).Where("id IN ?", envIDs).Pluck("id", &found).Error; err != nil {
			return fmt.Errorf("failed to verify environment ids: %w", err)
		}
		foundSet := make(map[string]struct{}, len(found))
		for _, id := range found {
			foundSet[id] = struct{}{}
		}
		for id := range envIDSet {
			if _, ok := foundSet[id]; !ok {
				return &common.InvalidRoleAssignmentError{EnvironmentID: id}
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
func (s *RoleService) ReplaceOidcAssignments(ctx context.Context, userID string, desired []models.UserRoleAssignment) error {
	return s.replaceUserAssignmentsForSourceInternal(ctx, userID, models.RoleAssignmentSourceOidc, desired)
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
func (s *RoleService) ResolvePermissions(ctx context.Context, user *models.User) (*authz.PermissionSet, error) {
	if user == nil {
		return authz.NewPermissionSet(), nil
	}
	if ps, ok := s.userCache.Get(user.ID); ok {
		return ps, nil
	}
	ps, err := s.resolveUserPermissionsInternal(ctx, s.db.WithContext(ctx), user.ID)
	if err != nil {
		return nil, err
	}
	s.userCache.Put(user.ID, ps)
	return ps, nil
}

func (s *RoleService) resolveUserPermissionsInternal(_ context.Context, tx *gorm.DB, userID string) (*authz.PermissionSet, error) {
	// Scan into raw string for the permissions JSON column to avoid GORM's
	// schema-introspection on anonymous local structs (which can't see the
	// type tags needed to wire models.StringSlice's Scanner).
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

// ResolveApiKeyPermissions returns the PermissionSet for an API key. Caches
// per-key. Falls back to an empty set (deny-all) if the key has no perms.
func (s *RoleService) ResolveApiKeyPermissions(ctx context.Context, apiKeyID string) (*authz.PermissionSet, error) {
	if ps, ok := s.apiKeyCache.Get(apiKeyID); ok {
		return ps, nil
	}
	var perms []models.ApiKeyPermission
	if err := s.db.WithContext(ctx).Where("api_key_id = ?", apiKeyID).Find(&perms).Error; err != nil {
		return nil, fmt.Errorf("failed to resolve api key permissions: %w", err)
	}
	ps := authz.NewPermissionSet()
	for _, p := range perms {
		if p.EnvironmentID == nil {
			ps.AddGlobal(p.Permission)
		} else {
			ps.AddEnv(*p.EnvironmentID, p.Permission)
		}
	}
	s.apiKeyCache.Put(apiKeyID, ps)
	return ps, nil
}

// SetApiKeyPermissions replaces every permission row on the given API key
// atomically. Validation that the granted permissions don't exceed the
// creator's capabilities happens in the handler layer.
func (s *RoleService) SetApiKeyPermissions(ctx context.Context, apiKeyID string, grants []models.ApiKeyPermission) error {
	err := dbutil.WithTx(ctx, s.db.DB, func(tx *gorm.DB) error {
		return s.setApiKeyPermissionsInternal(ctx, tx, apiKeyID, grants)
	})
	if err != nil {
		return err
	}
	s.apiKeyCache.Delete(apiKeyID)
	return nil
}

func (s *RoleService) setApiKeyPermissionsInternal(ctx context.Context, tx *gorm.DB, apiKeyID string, grants []models.ApiKeyPermission) error {
	for i := range grants {
		grants[i].ApiKeyID = apiKeyID
	}
	if err := tx.WithContext(ctx).Where("api_key_id = ?", apiKeyID).Delete(&models.ApiKeyPermission{}).Error; err != nil {
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

func (s *RoleService) ListOidcMappings(ctx context.Context) ([]models.OidcRoleMapping, error) {
	var out []models.OidcRoleMapping
	if err := s.db.WithContext(ctx).Order("claim_value, role_id").Find(&out).Error; err != nil {
		return nil, fmt.Errorf("failed to list oidc mappings: %w", err)
	}
	return out, nil
}

func (s *RoleService) GetOidcMapping(ctx context.Context, id string) (*models.OidcRoleMapping, error) {
	return dbutil.FirstWhere[models.OidcRoleMapping](ctx, s.db.DB, &common.OidcMappingNotFoundError{}, "id = ?", id)
}

func (s *RoleService) CreateOidcMapping(ctx context.Context, claimValue, roleID string, environmentID *string) (*models.OidcRoleMapping, error) {
	claimValue = strings.TrimSpace(claimValue)
	roleID = strings.TrimSpace(roleID)
	if claimValue == "" {
		return nil, errors.New("claim value is required")
	}
	if roleID == "" {
		return nil, errors.New("role id is required")
	}
	var mapping models.OidcRoleMapping
	err := dbutil.WithTx(ctx, s.db.DB, func(tx *gorm.DB) error {
		if err := validateRoleIDsExistInternal(tx, []string{roleID}); err != nil {
			return err
		}
		mapping = models.OidcRoleMapping{
			ClaimValue:    claimValue,
			RoleID:        roleID,
			EnvironmentID: environmentID,
			Source:        models.OidcMappingSourceManual,
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

func (s *RoleService) UpdateOidcMapping(ctx context.Context, id, claimValue, roleID string, environmentID *string) (*models.OidcRoleMapping, error) {
	claimValue = strings.TrimSpace(claimValue)
	roleID = strings.TrimSpace(roleID)
	if claimValue == "" {
		return nil, errors.New("claim value is required")
	}
	if roleID == "" {
		return nil, errors.New("role id is required")
	}
	var out models.OidcRoleMapping
	err := dbutil.WithTx(ctx, s.db.DB, func(tx *gorm.DB) error {
		var existing models.OidcRoleMapping
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", id).First(&existing).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return &common.OidcMappingNotFoundError{}
			}
			return fmt.Errorf("failed to load mapping: %w", err)
		}
		if existing.Source == models.OidcMappingSourceEnv {
			return &common.OidcMappingEnvManagedError{}
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

	normalized := make([]string, 0, len(roleIDSet))
	for roleID := range roleIDSet {
		normalized = append(normalized, roleID)
	}
	var found []string
	if err := tx.Model(&models.Role{}).Where("id IN ?", normalized).Pluck("id", &found).Error; err != nil {
		return fmt.Errorf("failed to verify role ids: %w", err)
	}
	foundSet := make(map[string]struct{}, len(found))
	for _, id := range found {
		foundSet[id] = struct{}{}
	}
	for _, id := range normalized {
		if _, ok := foundSet[id]; !ok {
			return &common.InvalidRoleAssignmentError{RoleID: id}
		}
	}
	return nil
}

func (s *RoleService) DeleteOidcMapping(ctx context.Context, id string) error {
	return dbutil.WithTx(ctx, s.db.DB, func(tx *gorm.DB) error {
		var existing models.OidcRoleMapping
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", id).First(&existing).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return &common.OidcMappingNotFoundError{}
			}
			return fmt.Errorf("failed to load mapping: %w", err)
		}
		if existing.Source == models.OidcMappingSourceEnv {
			return &common.OidcMappingEnvManagedError{}
		}
		if err := tx.Delete(&models.OidcRoleMapping{}, "id = ?", id).Error; err != nil {
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
			if err := tx.Model(&models.Role{}).Where("id = ?", sp.RoleID).Count(&count).Error; err != nil {
				return fmt.Errorf("OIDC_ROLE_MAPPINGS[%d]: failed to verify role: %w", i, err)
			}
			if count == 0 {
				return fmt.Errorf("OIDC_ROLE_MAPPINGS[%d]: role %q does not exist", i, sp.RoleID)
			}
		}

		// Declarative replace: drop every env-managed row, then insert the new
		// set. Manual rows are untouched.
		if err := tx.Where("source = ?", models.OidcMappingSourceEnv).Delete(&models.OidcRoleMapping{}).Error; err != nil {
			return fmt.Errorf("failed to clear env-managed mappings: %w", err)
		}
		if len(specs) == 0 {
			slog.InfoContext(ctx, "OIDC_ROLE_MAPPINGS reconciled (empty)", "envManagedCount", 0)
			return nil
		}
		rows := make([]models.OidcRoleMapping, len(specs))
		for i, sp := range specs {
			rows[i] = models.OidcRoleMapping{
				ClaimValue:    sp.ClaimValue,
				RoleID:        sp.RoleID,
				EnvironmentID: sp.EnvironmentID,
				Source:        models.OidcMappingSourceEnv,
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
	s.userCache.Delete(userID)
}

// InvalidateApiKey drops the cached PermissionSet for one API key.
func (s *RoleService) InvalidateApiKey(apiKeyID string) {
	s.apiKeyCache.Delete(apiKeyID)
}

// invalidateUsersAssignedToInternal invalidates every user holding an assignment to
// the given role. Called after a role's permissions change.
func (s *RoleService) invalidateUsersAssignedToInternal(ctx context.Context, roleID string) {
	var userIDs []string
	if err := s.db.WithContext(ctx).
		Model(&models.UserRoleAssignment{}).
		Distinct("user_id").
		Where("role_id = ?", roleID).
		Pluck("user_id", &userIDs).Error; err != nil {
		slog.WarnContext(ctx, "failed to collect users for cache invalidation", "error", err, "role_id", roleID)
		return
	}
	for _, id := range userIDs {
		s.userCache.Delete(id)
	}
}

// ---------- helpers ----------

func validatePermissionsInternal(perms []string) error {
	for _, p := range perms {
		if !authz.IsKnownPermission(p) {
			return &common.UnknownPermissionError{Perm: p}
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
	return validatePermissionSetAgainstCallerInternal(caller, desired, pkgutils.DerefString(environmentID))
}

func validatePermissionSetAgainstCallerInternal(caller *authz.PermissionSet, desired []string, environmentID string) error {
	if caller == nil {
		if len(desired) == 0 {
			return nil
		}
		return &common.RolePermissionEscalationError{Perm: desired[0]}
	}
	if caller.Sudo {
		return nil
	}
	for _, p := range desired {
		if !caller.Allows(p, environmentID) {
			return &common.RolePermissionEscalationError{Perm: p}
		}
	}
	return nil
}
