package role

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/samber/hot"
	"go.getarcane.app/kit/normalization"
	"go.getarcane.app/kit/pkg"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/getarcaneapp/arcane/backend/v3/internal/audit"
	"github.com/getarcaneapp/arcane/backend/v3/internal/common"
	"github.com/getarcaneapp/arcane/backend/v3/internal/database"
	"github.com/getarcaneapp/arcane/backend/v3/internal/role/children/binding"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/dbutil"
)

const (
	permissionCacheTTL            = 60 * time.Second
	globalAdminGuardLockKey int64 = 0x617263616e65
	userBindingsCondition         = "role_bindings.user_id = ? OR role_bindings.group_id IN (SELECT group_id FROM group_members WHERE user_id = ?)"
)

type RoleService struct {
	db             *database.DB
	userCache      *hot.HotCache[string, *authz.PermissionSet]
	apiKeyCache    *hot.HotCache[string, *authz.PermissionSet]
	apiKeyOwners   map[string]map[string]struct{}
	cacheFillMu    sync.Mutex
	userCacheGen   atomic.Uint64
	apiKeyCacheGen atomic.Uint64
	bindings       *binding.BindingService
}

func NewRoleService(db *database.DB) *RoleService {
	service := &RoleService{
		db: db,
		userCache: hot.NewHotCache[string, *authz.PermissionSet](hot.LRU, 2048).
			WithTTL(permissionCacheTTL).
			WithJanitor().
			Build(),
		apiKeyCache: hot.NewHotCache[string, *authz.PermissionSet](hot.LRU, 2048).
			WithTTL(permissionCacheTTL).
			WithJanitor().
			Build(),
		apiKeyOwners: map[string]map[string]struct{}{},
	}
	service.bindings = binding.NewBindingService(
		db,
		binding.RoleLookup{
			Role:         service.lookupRole,
			LockRole:     service.lockRole,
			UserBindings: service.ListUserBindings,
		},
		binding.Guard{
			Lock:        service.LockGlobalAdminGuard,
			AssertAdmin: service.AssertGlobalAdminExists,
			Authorize: func(ctx context.Context, tx *gorm.DB, caller *authz.PermissionSet, roleID string, environmentID *string, selector *types.ResourceSelector) error {
				return service.ValidateBindingAgainstCaller(ctx, tx, caller, roleID, environmentID, selector)
			},
			Invalidate: service.InvalidateBindingSubjects,
		},
	)
	return service
}

// AssertGlobalAdminExists returns common.ErrNoGlobalAdminRemains when no non-service user is a global admin in tx.
func (s *RoleService) AssertGlobalAdminExists(ctx context.Context, tx *gorm.DB) error {
	count, err := s.CountAdmins(ctx, tx)
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

func roleFromDefinition(definition authz.RoleDefinition) Role {
	return Role{
		BaseModel:   database.BaseModel{ID: definition.ID},
		Name:        definition.Name,
		Description: new(definition.Description),
		Permissions: database.StringSlice(definition.Permissions),
		BuiltIn:     true,
	}
}

func (s *RoleService) ListRoles(ctx context.Context, params pagination.QueryParams) ([]Role, pagination.Response, error) {
	var custom []Role
	if err := s.db.WithContext(ctx).Where("built_in = ?", false).Find(&custom).Error; err != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to list roles: %w", err)
	}
	roles := make([]Role, 0, len(custom)+len(authz.BuiltInRoles()))
	for _, definition := range authz.BuiltInRoles() {
		roles = append(roles, roleFromDefinition(definition))
	}
	result := pagination.Config[Role]{
		SearchAccessors: []pagination.SearchAccessor[Role]{
			func(r Role) (string, error) {
				return r.Name, nil
			},
			func(r Role) (string, error) {
				return kit.FromPtr(r.Description), nil
			},
		},
		SortBindings: []pagination.SortBinding[Role]{
			{
				Key: "name",
				Fn: func(a, b Role) int {
					return strings.Compare(a.Name, b.Name)
				},
			},
			{
				Key: "builtIn",
				Fn: func(a, b Role) int {
					return cmp.Compare(kit.Ternary(a.BuiltIn, 1, 0), kit.Ternary(b.BuiltIn, 1, 0))
				},
			},
			{
				Key: "createdAt",
				Fn: func(a, b Role) int {
					return a.CreatedAt.Compare(b.CreatedAt)
				},
			},
		},
	}.SearchOrderAndPaginate(append(roles, custom...), params)
	return result.Items, pagination.BuildResponse(result.TotalCount, result.TotalAvailable, params), nil
}

func (s *RoleService) GetRole(ctx context.Context, id string) (*Role, error) {
	return findRole(ctx, s.db.DB, id)
}

func findRole(ctx context.Context, db *gorm.DB, id string) (*Role, error) {
	if definition, ok := authz.BuiltInRole(id); ok {
		return new(roleFromDefinition(definition)), nil
	}
	return dbutil.FirstWhere[Role](ctx, db, common.Classify(common.ErrRoleNotFound, errors.New("Role not found")), "id = ? AND built_in = ?", id, false)
}

func (s *RoleService) lookupRole(ctx context.Context, roleID string) (types.Role, error) {
	role, err := s.GetRole(ctx, roleID)
	if err != nil {
		return types.Role{}, err
	}
	return roleDTO(*role), nil
}

func (s *RoleService) lockRole(ctx context.Context, tx *gorm.DB, roleID string) (types.Role, error) {
	if definition, ok := authz.BuiltInRole(roleID); ok {
		return roleDTO(roleFromDefinition(definition)), nil
	}
	role, err := loadCustomRoleForUpdate(tx.WithContext(ctx), roleID)
	if err != nil {
		return types.Role{}, err
	}
	return roleDTO(role), nil
}

func roleDTO(role Role) types.Role {
	return types.Role{
		ID:          role.ID,
		Name:        role.Name,
		Description: role.Description,
		Permissions: role.Permissions,
		BuiltIn:     role.BuiltIn,
		CreatedAt:   role.CreatedAt,
		UpdatedAt:   role.UpdatedAt,
	}
}

func (s *RoleService) CreateRole(ctx context.Context, name string, description *string, permissions []string) (*Role, error) {
	input := types.Role{Name: name, Description: description}
	if err := normalization.Normalize(&input); err != nil {
		return nil, common.Classify(common.ErrValidation, err)
	}
	if input.Name == "" {
		return nil, common.Classify(common.ErrValidation, errors.New("role name is required"))
	}
	if len(permissions) == 0 {
		return nil, common.Classify(common.ErrValidation, errors.New("role requires at least one permission"))
	}
	if err := validatePermissions(permissions); err != nil {
		return nil, err
	}
	role := &Role{
		Name:        input.Name,
		Description: input.Description,
		Permissions: database.StringSlice(permissions),
	}
	err := dbutil.WithTx(ctx, s.db.DB, func(tx *gorm.DB) error {
		var conflict int64
		if err := tx.Model(&Role{}).Where("name = ?", input.Name).Count(&conflict).Error; err != nil {
			return fmt.Errorf("failed to check role name uniqueness: %w", err)
		}
		if conflict > 0 {
			return common.Classify(common.ErrRoleNameTaken, errors.New("Role name already in use"))
		}
		if err := tx.Create(role).Error; err != nil {
			return err
		}
		return audit.RecordInTx(ctx, tx, types.AuditEntry{
			Action:     types.AuditActionRoleCreate,
			TargetType: types.AuditTargetRole,
			TargetID:   role.ID,
			TargetName: role.Name,
			After:      roleSnapshot(*role),
		})
	})
	if err != nil {
		return nil, err
	}
	return role, nil
}

func roleSnapshot(role Role) map[string]any {
	return map[string]any{
		"name":        role.Name,
		"description": role.Description,
		"permissions": []string(role.Permissions),
	}
}

func lockBoundUserRows(tx *gorm.DB, roleID string) ([]string, error) {
	var ids []string
	if err := tx.Raw(
		"SELECT user_id FROM role_bindings WHERE role_id = ? AND user_id IS NOT NULL "+
			"UNION SELECT gm.user_id FROM group_members gm INNER JOIN role_bindings rb ON rb.group_id = gm.group_id WHERE rb.role_id = ?",
		roleID, roleID,
	).Scan(&ids).Error; err != nil {
		return nil, fmt.Errorf("failed to list users bound to role: %w", err)
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
		return nil, fmt.Errorf("failed to lock users bound to role: %w", err)
	}
	return ids, nil
}

func loadCustomRoleForUpdate(tx *gorm.DB, id string) (Role, error) {
	if _, builtIn := authz.BuiltInRole(id); builtIn {
		return Role{}, common.Classify(common.ErrRoleBuiltIn, errors.New("Built-in role cannot be modified")) //nolint:staticcheck // Preserve the existing error message.
	}
	var existing Role
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND built_in = ?", id, false).First(&existing).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Role{}, common.Classify(common.ErrRoleNotFound, errors.New("Role not found"))
		}
		return Role{}, fmt.Errorf("failed to load role: %w", err)
	}
	return existing, nil
}

func (s *RoleService) UpdateRole(ctx context.Context, caller *authz.PermissionSet, id, name string, description *string, permissions []string) (*Role, error) {
	input := types.Role{Name: name, Description: description}
	if err := normalization.Normalize(&input); err != nil {
		return nil, common.Classify(common.ErrValidation, err)
	}
	if len(permissions) == 0 {
		return nil, common.Classify(common.ErrValidation, errors.New("role requires at least one permission"))
	}
	if err := validatePermissions(permissions); err != nil {
		return nil, err
	}
	var out Role
	var affected []string
	err := dbutil.WithTx(ctx, s.db.DB, func(tx *gorm.DB) error {
		if lockErr := s.LockGlobalAdminGuard(tx); lockErr != nil {
			return lockErr
		}
		existing, err := loadCustomRoleForUpdate(tx, id)
		if err != nil {
			return err
		}
		if defineErr := authz.CanDefineRole(caller, existing.Permissions); defineErr != nil {
			return defineErr
		}
		if _, validateErr := s.validateRoleBindingsAgainstCaller(ctx, tx, caller, id); validateErr != nil {
			return validateErr
		}
		if input.Name != existing.Name {
			var conflict int64
			if countErr := tx.Model(&Role{}).Where("name = ? AND id <> ?", input.Name, id).Count(&conflict).Error; countErr != nil {
				return fmt.Errorf("failed to check role name uniqueness: %w", countErr)
			}
			if conflict > 0 {
				return common.Classify(common.ErrRoleNameTaken, errors.New("Role name already in use"))
			}
		}
		if affected, err = lockBoundUserRows(tx, id); err != nil {
			return err
		}
		out = Role{
			BaseModel:   existing.BaseModel,
			Name:        input.Name,
			Description: input.Description,
			Permissions: permissions,
		}
		if updateRoleErr := tx.Save(&out).Error; updateRoleErr != nil {
			return fmt.Errorf("failed to update role: %w", updateRoleErr)
		}
		return audit.RecordInTx(ctx, tx, types.AuditEntry{
			Action:     types.AuditActionRoleUpdate,
			TargetType: types.AuditTargetRole,
			TargetID:   out.ID,
			TargetName: out.Name,
			Before:     roleSnapshot(existing),
			After:      roleSnapshot(out),
		})
	})
	if err != nil {
		return nil, err
	}
	s.InvalidateUsers(affected...)
	return &out, nil
}

func (s *RoleService) DeleteRole(ctx context.Context, caller *authz.PermissionSet, id string) error {
	if _, err := s.GetRole(ctx, id); err != nil {
		return err
	}
	var affected []string
	err := dbutil.WithTx(ctx, s.db.DB, func(tx *gorm.DB) error {
		if lockErr := s.LockGlobalAdminGuard(tx); lockErr != nil {
			return lockErr
		}
		existing, err := loadCustomRoleForUpdate(tx, id)
		if err != nil {
			return err
		}
		bindings, validateErr := s.validateRoleBindingsAgainstCaller(ctx, tx, caller, id)
		if validateErr != nil {
			return validateErr
		}
		var federatedCredentialCount int64
		if countErr := tx.Table("federated_credentials").Where("role_id = ?", id).Count(&federatedCredentialCount).Error; countErr != nil {
			return fmt.Errorf("failed to check federated credentials for role: %w", countErr)
		}
		if federatedCredentialCount > 0 {
			return common.ErrRoleUsedByFederatedCredential
		}
		ids, err := lockBoundUserRows(tx, id)
		if err != nil {
			return err
		}
		affected = ids
		if deleteBindingsErr := tx.Where("role_id = ?", id).Delete(&RoleBinding{}).Error; deleteBindingsErr != nil {
			return fmt.Errorf("failed to delete role bindings: %w", deleteBindingsErr)
		}
		if deleteRoleErr := tx.Delete(&Role{}, "id = ?", id).Error; deleteRoleErr != nil {
			return fmt.Errorf("failed to delete role: %w", deleteRoleErr)
		}
		return audit.RecordInTx(ctx, tx, types.AuditEntry{
			Action:     types.AuditActionRoleDelete,
			TargetType: types.AuditTargetRole,
			TargetID:   existing.ID,
			TargetName: existing.Name,
			Before:     roleSnapshot(existing),
			Metadata:   map[string]any{"bindingCount": len(bindings)},
		})
	})
	if err != nil {
		return err
	}
	// Invalidate after commit so a concurrent cache miss cannot refill stale permissions.
	s.InvalidateUsers(affected...)
	return nil
}

func (s *RoleService) CountBindingsForRole(ctx context.Context, roleID string) (int, error) {
	var count int64
	if err := s.db.WithContext(ctx).Model(&RoleBinding{}).Where("role_id = ?", roleID).Count(&count).Error; err != nil {
		return 0, fmt.Errorf("failed to count role bindings: %w", err)
	}
	return int(count), nil
}

func (s *RoleService) CountAdmins(ctx context.Context, tx *gorm.DB, exclude ...string) (int, error) {
	query := tx.WithContext(ctx).
		Table("users AS u").
		Where("u.is_service_account = ?", false).
		Where(
			"EXISTS (SELECT 1 FROM role_bindings rb WHERE rb.role_id = ? AND rb.environment_id IS NULL AND rb.selector_hash = '' "+
				"AND (rb.user_id = u.id OR rb.group_id IN (SELECT gm.group_id FROM group_members gm WHERE gm.user_id = u.id)))",
			authz.BuiltInRoleAdmin,
		)
	if len(exclude) > 0 {
		query = query.Where("u.id NOT IN ?", exclude)
	}
	var count int64
	if err := query.Count(&count).Error; err != nil {
		return 0, fmt.Errorf("failed to count admins: %w", err)
	}
	return int(count), nil
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

func userBindings(tx *gorm.DB, userID string) ([]RoleBinding, error) {
	var bindings []RoleBinding
	if err := tx.Where(userBindingsCondition, userID, userID).Order("created_at, id").Find(&bindings).Error; err != nil {
		return nil, fmt.Errorf("failed to load user role bindings: %w", err)
	}
	return bindings, nil
}

func rolesByID(tx *gorm.DB, bindings []RoleBinding) (map[string]Role, error) {
	out := make(map[string]Role, len(bindings))
	var customIDs []string
	for _, b := range bindings {
		if definition, ok := authz.BuiltInRole(b.RoleID); ok {
			out[b.RoleID] = roleFromDefinition(definition)
			continue
		}
		customIDs = append(customIDs, b.RoleID)
	}
	if len(customIDs) == 0 {
		return out, nil
	}
	var custom []Role
	if err := tx.Where("id IN ? AND built_in = ?", slices.Compact(slices.Sorted(slices.Values(customIDs))), false).Find(&custom).Error; err != nil {
		return nil, fmt.Errorf("failed to load bound roles: %w", err)
	}
	for _, r := range custom {
		out[r.ID] = r
	}
	return out, nil
}

// ResolveUserPermissionsInDB resolves the user's permissions in tx.
func (s *RoleService) ResolveUserPermissionsInDB(_ context.Context, tx *gorm.DB, userID string) (*authz.PermissionSet, error) {
	bindings, err := userBindings(tx, userID)
	if err != nil {
		return nil, err
	}
	roles, err := rolesByID(tx, bindings)
	if err != nil {
		return nil, err
	}
	ps := &authz.PermissionSet{
		Global:    map[string]struct{}{},
		PerEnv:    map[string]map[string]struct{}{},
		Scoped:    map[string]map[string][]types.ResourceSelector{},
		SubjectID: userID,
	}
	for _, b := range bindings {
		permissions := []string(roles[b.RoleID].Permissions)
		envID := kit.FromPtr(b.EnvironmentID)
		if b.Selector == nil {
			ps.AddEnv(envID, permissions...)
			continue
		}
		granted, _ := authz.EffectiveBindingPermissions(permissions, b.Selector)
		ps.AddScopedEnv(envID, *b.Selector, granted...)
	}
	return ps, nil
}

func (s *RoleService) ListUserBindings(ctx context.Context, userID string) ([]types.RoleBindingSummary, error) {
	db := s.db.WithContext(ctx)
	bindings, err := userBindings(db, userID)
	if err != nil {
		return nil, err
	}
	roles, err := rolesByID(db, bindings)
	if err != nil {
		return nil, err
	}
	var groups []struct {
		ID   string
		Name string
	}
	if groupsErr := db.Table("groups").Select("id, name").Where("id IN (SELECT group_id FROM group_members WHERE user_id = ?)", userID).Scan(&groups).Error; groupsErr != nil {
		return nil, fmt.Errorf("failed to load user groups: %w", groupsErr)
	}
	groupNames := make(map[string]string, len(groups))
	for _, g := range groups {
		groupNames[g.ID] = g.Name
	}
	out := make([]types.RoleBindingSummary, len(bindings))
	for i, b := range bindings {
		out[i] = types.RoleBindingSummary{
			ID:            b.ID,
			RoleID:        b.RoleID,
			RoleName:      roles[b.RoleID].Name,
			EnvironmentID: b.EnvironmentID,
			Selector:      b.Selector,
			Source:        b.Source,
			GroupID:       b.GroupID,
			GroupName:     kit.Ternary(b.GroupID != nil, new(groupNames[kit.FromPtr(b.GroupID)]), nil),
		}
	}
	return out, nil
}

func (s *RoleService) ListUserGroups(ctx context.Context, userID string) ([]types.GroupRef, error) {
	refs := []types.GroupRef{}
	if err := s.db.WithContext(ctx).
		Table("group_members AS gm").
		Select("g.id AS id, g.name AS name, gm.source AS source").
		Joins("INNER JOIN groups g ON g.id = gm.group_id").
		Where("gm.user_id = ?", userID).
		Order("g.name, gm.source").
		Scan(&refs).Error; err != nil {
		return nil, fmt.Errorf("failed to list user groups: %w", err)
	}
	return refs, nil
}

func (s *RoleService) BindUser(ctx context.Context, userID, roleID string) error {
	if err := s.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&RoleBinding{
		UserID: new(userID),
		RoleID: roleID,
		Source: BindingSourceManual,
	}).Error; err != nil {
		return fmt.Errorf("failed to bind role %s to user %s: %w", roleID, userID, err)
	}
	s.InvalidateUsers(userID)
	return nil
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
	if kit.FromPtr(key.UserID) != userID || (key.ExpiresAt != nil && !key.ExpiresAt.After(time.Now())) {
		return nil, errors.New("requesting API key is no longer valid")
	}
	var permissions *authz.PermissionSet
	var err error
	switch key.Kind {
	case "personal":
		permissions, err = s.ResolveUserPermissionsInDB(ctx, db, userID)
	case "scoped":
		permissions, _, err = s.resolveApiKeyPermissionsInDB(ctx, db, keyID)
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
	restricted := &authz.PermissionSet{
		Global:    map[string]struct{}{},
		PerEnv:    map[string]map[string]struct{}{},
		Scoped:    map[string]map[string][]types.ResourceSelector{},
		SubjectID: permissions.SubjectID,
	}
	for permission := range permissions.Global {
		restricted.AddEnv(*key.EnvironmentID, permission)
	}
	for permission := range permissions.PerEnv[*key.EnvironmentID] {
		restricted.AddEnv(*key.EnvironmentID, permission)
	}
	for _, scopeEnvID := range []string{"", *key.EnvironmentID} {
		for permission, selectors := range permissions.Scoped[scopeEnvID] {
			for _, selector := range selectors {
				restricted.AddScopedEnv(*key.EnvironmentID, selector, permission)
			}
		}
	}
	return restricted, nil
}

// ResolveApiKeyPermissions returns the API key's permissions, cached per key.
func (s *RoleService) ResolveApiKeyPermissions(ctx context.Context, apiKeyID string) (*authz.PermissionSet, error) {
	if ps, ok, _ := s.apiKeyCache.Get(apiKeyID); ok {
		return ps, nil
	}
	gen := s.apiKeyCacheGen.Load()
	permissions, ownerID, err := s.resolveApiKeyPermissionsInDB(ctx, s.db.WithContext(ctx), apiKeyID)
	if err != nil {
		return nil, err
	}
	s.cacheFillMu.Lock()
	if s.apiKeyCacheGen.Load() == gen {
		s.apiKeyCache.Set(apiKeyID, permissions)
		if s.apiKeyOwners[ownerID] == nil {
			s.apiKeyOwners[ownerID] = map[string]struct{}{}
		}
		s.apiKeyOwners[ownerID][apiKeyID] = struct{}{}
	}
	s.cacheFillMu.Unlock()
	return permissions, nil
}

func inheritOwnerGrant(dst, owner *authz.PermissionSet, permission, envID string) {
	if owner.Scope(permission, envID).IsUnrestricted() {
		dst.AddEnv(envID, permission)
		return
	}
	for _, selector := range slices.Concat(owner.Scoped[""][permission], kit.Ternary(envID != "", owner.Scoped[envID][permission], nil)) {
		dst.AddScopedEnv(envID, selector, permission)
	}
}

func (s *RoleService) resolveApiKeyPermissionsInDB(ctx context.Context, db *gorm.DB, apiKeyID string) (*authz.PermissionSet, string, error) {
	var key struct{ UserID *string }
	if err := db.Table("api_keys").Select("user_id").Where("id = ?", apiKeyID).Take(&key).Error; err != nil {
		return nil, "", fmt.Errorf("failed to load api key owner: %w", err)
	}
	ownerID := kit.FromPtr(key.UserID)
	if ownerID == "" {
		return authz.NewPermissionSet(), "", nil
	}
	owner, err := s.ResolveUserPermissionsInDB(ctx, db, ownerID)
	if err != nil {
		return nil, "", err
	}
	var grants []ApiKeyPermission
	if findGrantsErr := db.Where("api_key_id = ?", apiKeyID).Find(&grants).Error; findGrantsErr != nil {
		return nil, "", fmt.Errorf("failed to resolve api key permissions: %w", findGrantsErr)
	}
	permissions := &authz.PermissionSet{
		Global:    map[string]struct{}{},
		PerEnv:    map[string]map[string]struct{}{},
		Scoped:    map[string]map[string][]types.ResourceSelector{},
		SubjectID: ownerID,
	}
	for _, grant := range grants {
		if grant.EnvironmentID != nil {
			inheritOwnerGrant(permissions, owner, grant.Permission, *grant.EnvironmentID)
			continue
		}
		inheritOwnerGrant(permissions, owner, grant.Permission, "")
		if owner.Scope(grant.Permission, "").IsUnrestricted() {
			continue
		}
		for envID := range owner.PerEnv {
			inheritOwnerGrant(permissions, owner, grant.Permission, envID)
		}
	}
	return permissions, ownerID, nil
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

func (s *RoleService) InvalidateUsers(userIDs ...string) {
	s.cacheFillMu.Lock()
	defer s.cacheFillMu.Unlock()
	s.userCacheGen.Add(1)
	s.apiKeyCacheGen.Add(1)
	for _, userID := range userIDs {
		s.userCache.Delete(userID)
		for keyID := range s.apiKeyOwners[userID] {
			s.apiKeyCache.Delete(keyID)
		}
		delete(s.apiKeyOwners, userID)
	}
}

func (s *RoleService) InvalidateBindingSubjects(ctx context.Context, userID, groupID *string) {
	var ids []string
	if userID != nil {
		ids = append(ids, *userID)
	}
	if groupID != nil {
		var members []string
		if err := s.db.WithContext(ctx).Table("group_members").Where("group_id = ?", *groupID).Distinct("user_id").Pluck("user_id", &members).Error; err != nil {
			slog.WarnContext(ctx, "Failed to list group members for permission invalidation; purging every cached permission", "groupId", *groupID, "error", err)
			s.cacheFillMu.Lock()
			defer s.cacheFillMu.Unlock()
			s.userCacheGen.Add(1)
			s.apiKeyCacheGen.Add(1)
			s.userCache.Purge()
			s.apiKeyCache.Purge()
			s.apiKeyOwners = map[string]map[string]struct{}{}
			return
		}
		ids = append(ids, members...)
	}
	s.InvalidateUsers(ids...)
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

func validatePermissions(perms []string) error {
	for _, p := range perms {
		if !authz.IsKnownPermission(p) {
			return common.Classify(common.ErrUnknownPermission, errors.New("Unknown permission: "+p))
		}
	}
	return nil
}

func (s *RoleService) validateRoleBindingsAgainstCaller(ctx context.Context, tx *gorm.DB, caller *authz.PermissionSet, roleID string) ([]RoleBinding, error) {
	var bindings []RoleBinding
	if err := tx.Where("role_id = ?", roleID).Find(&bindings).Error; err != nil {
		return nil, fmt.Errorf("failed to list role bindings: %w", err)
	}
	for _, b := range bindings {
		if err := s.ValidateBindingAgainstCaller(ctx, tx, caller, roleID, b.EnvironmentID, b.Selector); err != nil {
			return nil, err
		}
	}
	return bindings, nil
}

func (s *RoleService) ValidateRoleDefinitionAgainstCaller(caller *authz.PermissionSet, permissions []string) error {
	if err := validatePermissions(permissions); err != nil {
		return err
	}
	return authz.CanDefineRole(caller, permissions)
}

func (s *RoleService) ValidateBindingAgainstCaller(ctx context.Context, db *gorm.DB, caller *authz.PermissionSet, roleID string, environmentID *string, selector *types.ResourceSelector) error {
	role, err := s.lockRole(ctx, db, roleID)
	if err != nil {
		return err
	}
	return authz.CanBind(caller, role.Permissions, kit.FromPtr(environmentID), selector)
}

// BuildPermissionsManifest maps the authz-owned permission catalog into
// the public API manifest shape used by the frontend role editor.
func BuildPermissionsManifest() types.PermissionsManifest {
	catalog := authz.PermissionCatalog()
	resources := make([]types.PermissionResource, len(catalog))
	for i, resource := range catalog {
		actions := make([]types.PermissionAction, len(resource.Actions))
		for j, action := range resource.Actions {
			actions[j] = types.PermissionAction{
				Key:         action.Key,
				Permission:  action.Permission,
				Label:       action.Label,
				Description: action.Description,
				Requires:    requiredPermissionsForPicker(action.Permission),
				Selectors:   action.Selectors,
			}
		}
		resources[i] = types.PermissionResource{
			Key:     resource.Key,
			Label:   resource.Label,
			Scope:   resource.Scope,
			Actions: actions,
		}
	}
	editor, _ := authz.BuiltInRole(authz.BuiltInRoleEditor)
	return types.PermissionsManifest{
		Resources:      resources,
		AccessSurfaces: buildAccessSurfaceManifest(),
		Presets: []types.PermissionPreset{
			{
				Key:         "editor",
				Label:       "All permissions (non-admin)",
				Description: "Matches the built-in Editor permission set.",
				Permissions: editor.Permissions,
			},
		},
	}
}

func buildAccessSurfaceManifest() []types.AccessSurface {
	surfaces := authz.AccessSurfaces()
	out := make([]types.AccessSurface, len(surfaces))
	for i, surface := range surfaces {
		out[i] = types.AccessSurface{
			ID:            surface.ID,
			Kind:          surface.Kind,
			URL:           surface.URL,
			Label:         surface.Label,
			AccessMode:    surface.AccessMode,
			MatchMode:     surface.MatchMode,
			ScopeMode:     surface.ScopeMode,
			Permissions:   append([]string(nil), surface.Permissions...),
			Children:      append([]string(nil), surface.Children...),
			FallbackOrder: surface.FallbackOrder,
		}
	}
	return out
}

func requiredPermissionsForPicker(permission string) []string {
	switch permission {
	case authz.PermSettingsWrite,
		authz.PermApiKeysList,
		authz.PermApiKeysRead,
		authz.PermApiKeysCreate,
		authz.PermApiKeysUpdate,
		authz.PermApiKeysDelete,
		authz.PermFederatedList,
		authz.PermFederatedRead,
		authz.PermFederatedCreate,
		authz.PermFederatedUpdate,
		authz.PermFederatedDelete,
		authz.PermUsersList,
		authz.PermUsersRead,
		authz.PermUsersCreate,
		authz.PermUsersUpdate,
		authz.PermUsersDelete,
		authz.PermRolesList,
		authz.PermRolesRead,
		authz.PermWebhooksList,
		authz.PermWebhooksCreate,
		authz.PermWebhooksUpdate,
		authz.PermWebhooksDelete,
		authz.PermNotificationsManage,
		authz.PermDiagnosticsRead:
		return []string{authz.PermSettingsRead}
	default:
		return nil
	}
}
