package binding

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"
	"uuid"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/samber/mo"
	"go.getarcane.app/kit/pkg"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/getarcaneapp/arcane/backend/v3/internal/audit"
	"github.com/getarcaneapp/arcane/backend/v3/internal/common"
	"github.com/getarcaneapp/arcane/backend/v3/internal/database"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/dbutil"
)

type BindingService struct {
	db     *database.DB
	lookup RoleLookup
	guard  Guard
}

func NewBindingService(db *database.DB, lookup RoleLookup, guard Guard) *BindingService {
	return &BindingService{
		db:     db,
		lookup: lookup,
		guard:  guard,
	}
}

func (s *BindingService) rows(ctx context.Context) *gorm.DB {
	return s.db.WithContext(ctx).
		Table("role_bindings AS rb").
		Select("rb.id, rb.user_id, rb.group_id, rb.role_id, rb.environment_id, rb.selector, rb.source, rb.created_by, rb.created_at, " +
			"u.username AS username, g.name AS group_name, r.name AS role_name, e.name AS environment_name").
		Joins("LEFT JOIN users u ON u.id = rb.user_id").
		Joins("LEFT JOIN groups g ON g.id = rb.group_id").
		Joins("LEFT JOIN roles r ON r.id = rb.role_id").
		Joins("LEFT JOIN environments e ON e.id = rb.environment_id")
}

func (s *BindingService) List(ctx context.Context, params pagination.QueryParams, filter types.RoleBindingFilter) ([]types.RoleBinding, pagination.Response, error) {
	query := s.rows(ctx)
	switch filter.SubjectType {
	case SubjectUser:
		query = query.Where("rb.user_id IS NOT NULL")
	case SubjectGroup:
		query = query.Where("rb.group_id IS NOT NULL")
	}
	if filter.SubjectID != "" {
		query = query.Where("(rb.user_id = ? OR rb.group_id = ?)", filter.SubjectID, filter.SubjectID)
	}
	if filter.RoleID != "" {
		query = query.Where("rb.role_id = ?", filter.RoleID)
	}
	if filter.EnvironmentID != "" {
		query = query.Where("rb.environment_id = ?", filter.EnvironmentID)
	}
	var rows []bindingRow
	if err := query.Order("rb.created_at, rb.id").Scan(&rows).Error; err != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to list role bindings: %w", err)
	}
	bindings := make([]types.RoleBinding, 0, len(rows))
	for _, row := range rows {
		binding, err := row.toDTO()
		if err != nil {
			return nil, pagination.Response{}, fmt.Errorf("failed to decode role binding %s: %w", row.ID, err)
		}
		bindings = append(bindings, binding)
	}
	result := pagination.Config[types.RoleBinding]{
		SearchAccessors: []pagination.SearchAccessor[types.RoleBinding]{
			func(b types.RoleBinding) (string, error) {
				return b.SubjectName, nil
			},
			func(b types.RoleBinding) (string, error) {
				return b.RoleName, nil
			},
			func(b types.RoleBinding) (string, error) {
				return b.EnvironmentName, nil
			},
		},
		SortBindings: []pagination.SortBinding[types.RoleBinding]{
			{
				Key: "createdAt",
				Fn: func(a, b types.RoleBinding) int {
					return a.CreatedAt.Compare(b.CreatedAt)
				},
			},
			{
				Key: "subjectName",
				Fn: func(a, b types.RoleBinding) int {
					return strings.Compare(a.SubjectName, b.SubjectName)
				},
			},
			{
				Key: "roleName",
				Fn: func(a, b types.RoleBinding) int {
					return strings.Compare(a.RoleName, b.RoleName)
				},
			},
			{
				Key: "environmentName",
				Fn: func(a, b types.RoleBinding) int {
					return strings.Compare(a.EnvironmentName, b.EnvironmentName)
				},
			},
		},
	}.SearchOrderAndPaginate(bindings, params)
	return result.Items, pagination.BuildResponse(result.TotalCount, result.TotalAvailable, params), nil
}

func (s *BindingService) Get(ctx context.Context, id string) (types.RoleBinding, error) {
	var row bindingRow
	if err := s.rows(ctx).Where("rb.id = ?", id).Take(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return types.RoleBinding{}, common.ErrRoleBindingNotFound
		}
		return types.RoleBinding{}, fmt.Errorf("failed to load role binding: %w", err)
	}
	return row.toDTO()
}

func (s *BindingService) Create(ctx context.Context, caller *authz.PermissionSet, actorID string, input types.CreateRoleBinding) (types.RoleBinding, []string, error) {
	if input.Selector != nil {
		if err := authz.ValidateSelector(*input.Selector); err != nil {
			return types.RoleBinding{}, nil, err
		}
	}
	if input.SubjectType != SubjectUser && input.SubjectType != SubjectGroup {
		return types.RoleBinding{}, nil, common.Classify(common.ErrInvalidRoleBinding, fmt.Errorf("invalid subject type %q", input.SubjectType))
	}
	hash, err := selectorHash(input.Selector)
	if err != nil {
		return types.RoleBinding{}, nil, fmt.Errorf("failed to hash selector: %w", err)
	}
	selector, err := encodeSelector(input.Selector)
	if err != nil {
		return types.RoleBinding{}, nil, fmt.Errorf("failed to encode selector: %w", err)
	}
	userID := kit.Ternary(input.SubjectType == SubjectUser, new(input.SubjectID), nil)
	groupID := kit.Ternary(input.SubjectType == SubjectGroup, new(input.SubjectID), nil)
	id := uuid.New().String()
	var dropped []string
	err = dbutil.WithTx(ctx, s.db.DB, func(tx *gorm.DB) error {
		if lockErr := s.guard.Lock(tx); lockErr != nil {
			return lockErr
		}
		if subjectErr := ensureBindingTargetsExist(tx, input); subjectErr != nil {
			return subjectErr
		}
		role, roleErr := s.lookup.LockRole(ctx, tx, input.RoleID)
		if errors.Is(roleErr, common.ErrRoleNotFound) {
			return common.Classify(common.ErrInvalidRoleBinding, fmt.Errorf("role %q does not exist", input.RoleID))
		}
		if roleErr != nil {
			return roleErr
		}
		if bindErr := authz.CanBind(caller, role.Permissions, kit.FromPtr(input.EnvironmentID), input.Selector); bindErr != nil {
			return bindErr
		}
		_, dropped = authz.EffectiveBindingPermissions(role.Permissions, input.Selector)
		var duplicates int64
		if countErr := tx.Table("role_bindings").
			Where("COALESCE(user_id, '') = ? AND COALESCE(group_id, '') = ?", kit.FromPtr(userID), kit.FromPtr(groupID)).
			Where("role_id = ? AND COALESCE(environment_id, '') = ?", input.RoleID, kit.FromPtr(input.EnvironmentID)).
			Where("selector_hash = ? AND source = ?", hash, sourceManual).
			Count(&duplicates).Error; countErr != nil {
			return fmt.Errorf("failed to check for duplicate role binding: %w", countErr)
		}
		if duplicates > 0 {
			return common.ErrRoleBindingExists
		}
		if createErr := tx.Table("role_bindings").Create(map[string]any{
			"id":             id,
			"user_id":        userID,
			"group_id":       groupID,
			"role_id":        input.RoleID,
			"environment_id": input.EnvironmentID,
			"selector":       selector,
			"selector_hash":  hash,
			"source":         sourceManual,
			"created_by":     mo.EmptyableToOption(actorID).ToPointer(),
			"created_at":     time.Now(),
		}).Error; createErr != nil {
			return createErr
		}
		return audit.RecordInTx(ctx, tx, types.AuditEntry{
			Action:        types.AuditActionBindingCreate,
			TargetType:    types.AuditTargetBinding,
			TargetID:      id,
			TargetName:    role.Name,
			EnvironmentID: input.EnvironmentID,
			After:         input,
			Metadata:      map[string]any{"droppedPermissions": dropped},
		})
	})
	if err != nil {
		return types.RoleBinding{}, nil, err
	}
	s.guard.Invalidate(ctx, userID, groupID)
	binding, err := s.Get(ctx, id)
	return binding, dropped, err
}

func ensureBindingTargetsExist(tx *gorm.DB, input types.CreateRoleBinding) error {
	var subjects []string
	if err := tx.Table(kit.Ternary(input.SubjectType == SubjectUser, "users", "groups")).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ?", input.SubjectID).
		Pluck("id", &subjects).Error; err != nil {
		return fmt.Errorf("failed to verify binding subject: %w", err)
	}
	if len(subjects) == 0 {
		return common.Classify(common.ErrInvalidRoleBinding, fmt.Errorf("%s %q does not exist", input.SubjectType, input.SubjectID))
	}
	if input.SubjectType == SubjectUser {
		var federatedCredentials int64
		if err := tx.Table("federated_credentials").Where("identity_user_id = ?", input.SubjectID).Count(&federatedCredentials).Error; err != nil {
			return fmt.Errorf("failed to check federated service user: %w", err)
		}
		if federatedCredentials > 0 {
			return common.Classify(common.ErrInvalidRoleBinding, fmt.Errorf("user %q is a federated credential service user; its access comes from the credential role", input.SubjectID))
		}
	}
	if input.EnvironmentID == nil {
		return nil
	}
	var environments int64
	if err := tx.Table("environments").Where("id = ?", *input.EnvironmentID).Count(&environments).Error; err != nil {
		return fmt.Errorf("failed to verify binding environment: %w", err)
	}
	if environments == 0 {
		return common.Classify(common.ErrInvalidRoleBinding, fmt.Errorf("environment %q does not exist", *input.EnvironmentID))
	}
	return nil
}

func (s *BindingService) Delete(ctx context.Context, caller *authz.PermissionSet, id string) error {
	existing, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	err = dbutil.WithTx(ctx, s.db.DB, func(tx *gorm.DB) error {
		if lockErr := s.guard.Lock(tx); lockErr != nil {
			return lockErr
		}
		if existing.SubjectType == SubjectGroup {
			if lockGroupErr := tx.Table("groups").
				Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("id = ?", existing.SubjectID).
				Pluck("id", new([]string)).Error; lockGroupErr != nil {
				return fmt.Errorf("failed to lock role binding group: %w", lockGroupErr)
			}
		}
		var locked []string
		if lockRowErr := tx.Table("role_bindings").
			Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", id).
			Pluck("id", &locked).Error; lockRowErr != nil {
			return fmt.Errorf("failed to lock role binding: %w", lockRowErr)
		}
		if len(locked) == 0 {
			return common.ErrRoleBindingNotFound
		}
		if authorizeErr := s.guard.Authorize(ctx, tx, caller, existing.RoleID, existing.EnvironmentID, existing.Selector); authorizeErr != nil {
			return authorizeErr
		}
		if deleteErr := tx.Exec("DELETE FROM role_bindings WHERE id = ?", id).Error; deleteErr != nil {
			return fmt.Errorf("failed to delete role binding: %w", deleteErr)
		}
		if recordErr := audit.RecordInTx(ctx, tx, types.AuditEntry{
			Action:        types.AuditActionBindingDelete,
			TargetType:    types.AuditTargetBinding,
			TargetID:      existing.ID,
			TargetName:    existing.RoleName,
			EnvironmentID: existing.EnvironmentID,
			Before:        existing,
		}); recordErr != nil {
			return recordErr
		}
		if existing.RoleID != authz.BuiltInRoleAdmin || existing.EnvironmentID != nil || existing.Selector != nil {
			return nil
		}
		return s.guard.AssertAdmin(ctx, tx)
	})
	if err != nil {
		return err
	}
	s.guard.Invalidate(
		ctx,
		kit.Ternary(existing.SubjectType == SubjectUser, new(existing.SubjectID), nil),
		kit.Ternary(existing.SubjectType == SubjectGroup, new(existing.SubjectID), nil),
	)
	return nil
}

func (s *BindingService) EffectiveAccess(ctx context.Context, userID string) (types.EffectiveAccess, error) {
	bindings, err := s.lookup.UserBindings(ctx, userID)
	if err != nil {
		return types.EffectiveAccess{}, err
	}
	scopes := map[string]map[string]*types.EffectivePermission{}
	roles := map[string]types.Role{}
	for _, binding := range bindings {
		role, cached := roles[binding.RoleID]
		if !cached {
			if role, err = s.lookup.Role(ctx, binding.RoleID); err != nil {
				return types.EffectiveAccess{}, err
			}
			roles[binding.RoleID] = role
		}
		granted, _ := authz.EffectiveBindingPermissions(role.Permissions, binding.Selector)
		accumulateEffectivePermissions(scopes, binding, granted)
	}
	global, isAdmin := scopes[""][authz.PermAll]
	return types.EffectiveAccess{
		UserID:        userID,
		IsGlobalAdmin: isAdmin && global.Unrestricted,
		Scopes:        effectiveScopes(scopes),
	}, nil
}

func accumulateEffectivePermissions(scopes map[string]map[string]*types.EffectivePermission, binding types.RoleBindingSummary, granted []string) {
	envID := kit.FromPtr(binding.EnvironmentID)
	if scopes[envID] == nil {
		scopes[envID] = map[string]*types.EffectivePermission{}
	}
	for _, permission := range granted {
		entry := scopes[envID][permission]
		if entry == nil {
			entry = &types.EffectivePermission{
				Permission: permission,
				Via:        []types.BindingRef{},
			}
			scopes[envID][permission] = entry
		}
		entry.Unrestricted = entry.Unrestricted || binding.Selector == nil
		if binding.Selector != nil {
			entry.Selectors = append(entry.Selectors, *binding.Selector)
		}
		entry.Via = append(entry.Via, types.BindingRef{
			ID:           binding.ID,
			RoleID:       binding.RoleID,
			RoleName:     binding.RoleName,
			GroupID:      binding.GroupID,
			GroupName:    binding.GroupName,
			Unrestricted: binding.Selector == nil,
		})
	}
}

func effectiveScopes(scopes map[string]map[string]*types.EffectivePermission) []types.EffectiveScope {
	out := make([]types.EffectiveScope, 0, len(scopes))
	for _, envID := range slices.Sorted(maps.Keys(scopes)) {
		permissions := make([]types.EffectivePermission, 0, len(scopes[envID]))
		for _, permission := range slices.Sorted(maps.Keys(scopes[envID])) {
			entry := *scopes[envID][permission]
			entry.Selectors = kit.Ternary(entry.Unrestricted, nil, entry.Selectors)
			permissions = append(permissions, entry)
		}
		out = append(out, types.EffectiveScope{
			EnvironmentID: mo.EmptyableToOption(envID).ToPointer(),
			Permissions:   permissions,
		})
	}
	return out
}
