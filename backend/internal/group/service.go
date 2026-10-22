package group

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/getarcaneapp/arcane/types/v3"
	"go.getarcane.app/kit/normalization"
	"go.getarcane.app/kit/pkg"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/getarcaneapp/arcane/backend/v3/internal/audit"
	"github.com/getarcaneapp/arcane/backend/v3/internal/common"
	"github.com/getarcaneapp/arcane/backend/v3/internal/database"
	"github.com/getarcaneapp/arcane/backend/v3/internal/role"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/dbutil"
)

type GroupService struct {
	db            *database.DB
	roles         *role.RoleService
	defaultRoleID func(context.Context) string
}

func NewGroupService(db *database.DB, roles *role.RoleService, defaultRoleID func(context.Context) string) *GroupService {
	return &GroupService{
		db:            db,
		roles:         roles,
		defaultRoleID: defaultRoleID,
	}
}

func (s *GroupService) List(ctx context.Context, params pagination.QueryParams) ([]types.Group, pagination.Response, error) {
	query := s.db.WithContext(ctx).Model(&Group{})
	if term := strings.TrimSpace(params.Search); term != "" {
		pattern := "%" + term + "%"
		query = query.Where("name LIKE ? OR COALESCE(description, '') LIKE ?", pattern, pattern)
	}
	var groups []Group
	resp, err := pagination.PaginateAndSortDB(params, query, &groups)
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to paginate groups: %w", err)
	}
	dtos, err := s.toDTOs(ctx, groups)
	if err != nil {
		return nil, pagination.Response{}, err
	}
	return dtos, resp, nil
}

func (s *GroupService) Get(ctx context.Context, id string) (types.Group, error) {
	found, err := dbutil.FirstWhere[Group](ctx, s.db.DB, common.ErrGroupNotFound, "id = ?", id)
	if err != nil {
		return types.Group{}, err
	}
	dtos, err := s.toDTOs(ctx, []Group{*found})
	if err != nil {
		return types.Group{}, err
	}
	return dtos[0], nil
}

func (s *GroupService) toDTOs(ctx context.Context, groups []Group) ([]types.Group, error) {
	ids := make([]string, len(groups))
	for i, g := range groups {
		ids[i] = g.ID
	}
	memberCounts, err := countByGroup(s.db.WithContext(ctx).Model(&GroupMember{}), "COUNT(DISTINCT user_id)", ids)
	if err != nil {
		return nil, fmt.Errorf("failed to count group members: %w", err)
	}
	bindingCounts, err := countByGroup(s.db.WithContext(ctx).Model(&role.RoleBinding{}), "COUNT(*)", ids)
	if err != nil {
		return nil, fmt.Errorf("failed to count group bindings: %w", err)
	}
	out := make([]types.Group, len(groups))
	for i, g := range groups {
		out[i] = types.Group{
			ID:           g.ID,
			Name:         g.Name,
			Description:  g.Description,
			MemberCount:  memberCounts[g.ID],
			BindingCount: bindingCounts[g.ID],
			CreatedAt:    g.CreatedAt,
			UpdatedAt:    g.UpdatedAt,
		}
	}
	return out, nil
}

func (s *GroupService) Create(ctx context.Context, input types.CreateGroup) (types.Group, error) {
	if err := normalization.Normalize(&input); err != nil {
		return types.Group{}, common.Classify(common.ErrValidation, err)
	}
	if input.Name == "" {
		return types.Group{}, common.Classify(common.ErrValidation, errors.New("group name is required"))
	}
	created := Group{
		Name:        input.Name,
		Description: input.Description,
	}
	err := dbutil.WithTx(ctx, s.db.DB, func(tx *gorm.DB) error {
		if err := ensureGroupNameAvailable(tx, input.Name, ""); err != nil {
			return err
		}
		if err := tx.Create(&created).Error; err != nil {
			return err
		}
		return audit.RecordInTx(ctx, tx, types.AuditEntry{
			Action:     types.AuditActionGroupCreate,
			TargetType: types.AuditTargetGroup,
			TargetID:   created.ID,
			TargetName: created.Name,
			After:      input,
		})
	})
	if err != nil {
		return types.Group{}, err
	}
	return s.Get(ctx, created.ID)
}

func (s *GroupService) Update(ctx context.Context, id string, input types.UpdateGroup, submitted map[string]bool) (types.Group, error) {
	if err := normalization.Normalize(&input); err != nil {
		return types.Group{}, common.Classify(common.ErrValidation, err)
	}
	err := dbutil.WithTx(ctx, s.db.DB, func(tx *gorm.DB) error {
		if lockErr := s.roles.LockGlobalAdminGuard(tx); lockErr != nil {
			return lockErr
		}
		existing, err := lockGroup(tx, id)
		if err != nil {
			return err
		}
		if input.Name != nil && *input.Name != existing.Name {
			if nameErr := ensureGroupNameAvailable(tx, *input.Name, id); nameErr != nil {
				return nameErr
			}
		}
		updated := Group{
			BaseModel:   existing.BaseModel,
			Name:        cmp.Or(kit.FromPtr(input.Name), existing.Name),
			Description: kit.Ternary(submitted["description"], input.Description, existing.Description),
		}
		if saveErr := tx.Save(&updated).Error; saveErr != nil {
			return saveErr
		}
		return audit.RecordInTx(ctx, tx, types.AuditEntry{
			Action:     types.AuditActionGroupUpdate,
			TargetType: types.AuditTargetGroup,
			TargetID:   id,
			TargetName: updated.Name,
			Before:     groupSnapshot(existing),
			After:      groupSnapshot(updated),
		})
	})
	if err != nil {
		return types.Group{}, err
	}
	return s.Get(ctx, id)
}

func (s *GroupService) Delete(ctx context.Context, caller *authz.PermissionSet, id string) error {
	var members []string
	err := dbutil.WithTx(ctx, s.db.DB, func(tx *gorm.DB) error {
		if err := s.roles.LockGlobalAdminGuard(tx); err != nil {
			return err
		}
		existing, err := s.lockGroupChange(ctx, tx, caller, id)
		if err != nil {
			return err
		}
		admin, err := grantsAdmin(tx, id)
		if err != nil {
			return err
		}
		if members, err = memberIDs(tx, id); err != nil {
			return err
		}
		for _, model := range []any{&GroupMember{}, &role.RoleBinding{}, &OidcGroupMapping{}} {
			if deleteErr := tx.Where("group_id = ?", id).Delete(model).Error; deleteErr != nil {
				return fmt.Errorf("failed to delete group references: %w", deleteErr)
			}
		}
		if deleteErr := tx.Delete(&Group{}, "id = ?", id).Error; deleteErr != nil {
			return fmt.Errorf("failed to delete group: %w", deleteErr)
		}
		if recordErr := audit.RecordInTx(ctx, tx, types.AuditEntry{
			Action:     types.AuditActionGroupDelete,
			TargetType: types.AuditTargetGroup,
			TargetID:   id,
			TargetName: existing.Name,
			Before:     groupSnapshot(existing),
			Metadata:   map[string]any{"memberIds": members},
		}); recordErr != nil {
			return recordErr
		}
		if !admin {
			return nil
		}
		return s.roles.AssertGlobalAdminExists(ctx, tx)
	})
	if err != nil {
		return err
	}
	s.roles.InvalidateUsers(members...)
	return nil
}

func (s *GroupService) ListMembers(ctx context.Context, groupID string) ([]types.GroupMember, error) {
	if _, err := dbutil.FirstWhere[Group](ctx, s.db.DB, common.ErrGroupNotFound, "id = ?", groupID); err != nil {
		return nil, err
	}
	members := []types.GroupMember{}
	if err := s.db.WithContext(ctx).
		Table("group_members AS gm").
		Select("gm.user_id AS user_id, u.username AS username, u.display_name AS display_name, gm.source AS source, gm.created_at AS created_at").
		Joins("INNER JOIN users u ON u.id = gm.user_id").
		Where("gm.group_id = ?", groupID).
		Order("u.username, gm.source").
		Scan(&members).Error; err != nil {
		return nil, fmt.Errorf("failed to list group members: %w", err)
	}
	return members, nil
}

func (s *GroupService) AddMembers(ctx context.Context, caller *authz.PermissionSet, groupID string, userIDs []string) error {
	ids := slices.Compact(slices.Sorted(slices.Values(userIDs)))
	err := dbutil.WithTx(ctx, s.db.DB, func(tx *gorm.DB) error {
		if lockErr := s.roles.LockGlobalAdminGuard(tx); lockErr != nil {
			return lockErr
		}
		group, err := s.lockGroupChange(ctx, tx, caller, groupID)
		if err != nil {
			return err
		}
		var found []string
		if verifyErr := tx.Table("users").Where("id IN ?", ids).Pluck("id", &found).Error; verifyErr != nil {
			return fmt.Errorf("failed to verify users: %w", verifyErr)
		}
		if missing := slices.DeleteFunc(slices.Clone(ids), func(id string) bool {
			return slices.Contains(found, id)
		}); len(missing) > 0 {
			return common.Classify(common.ErrBadRequest, fmt.Errorf("user %q does not exist", missing[0]))
		}
		var federatedUsers []string
		if federatedErr := tx.Table("federated_credentials").Where("identity_user_id IN ?", ids).Pluck("identity_user_id", &federatedUsers).Error; federatedErr != nil {
			return fmt.Errorf("failed to check federated service users: %w", federatedErr)
		}
		if len(federatedUsers) > 0 {
			return common.Classify(common.ErrBadRequest, fmt.Errorf("user %q is a federated credential service user and cannot join groups", federatedUsers[0]))
		}
		rows := make([]GroupMember, len(ids))
		for i, userID := range ids {
			rows[i] = GroupMember{
				GroupID: groupID,
				UserID:  userID,
				Source:  MemberSourceManual,
			}
		}
		if createErr := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&rows).Error; createErr != nil {
			return createErr
		}
		return audit.RecordInTx(ctx, tx, types.AuditEntry{
			Action:     types.AuditActionGroupMemberAdd,
			TargetType: types.AuditTargetGroup,
			TargetID:   groupID,
			TargetName: group.Name,
			Metadata:   map[string]any{"userIds": ids},
		})
	})
	if err != nil {
		return err
	}
	s.roles.InvalidateUsers(ids...)
	return nil
}

func (s *GroupService) RemoveMember(ctx context.Context, caller *authz.PermissionSet, groupID, userID string) error {
	err := dbutil.WithTx(ctx, s.db.DB, func(tx *gorm.DB) error {
		if err := s.roles.LockGlobalAdminGuard(tx); err != nil {
			return err
		}
		if _, err := s.lockGroupChange(ctx, tx, caller, groupID); err != nil {
			return err
		}
		admin, err := grantsAdmin(tx, groupID)
		if err != nil {
			return err
		}
		result := tx.Where("group_id = ? AND user_id = ?", groupID, userID).Delete(&GroupMember{})
		if result.Error != nil {
			return fmt.Errorf("failed to remove group member: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			return common.Classify(common.ErrNotFound, errors.New("user is not a member of this group"))
		}
		if recordErr := audit.RecordInTx(ctx, tx, types.AuditEntry{
			Action:     types.AuditActionGroupMemberRemove,
			TargetType: types.AuditTargetGroup,
			TargetID:   groupID,
			Metadata:   map[string]any{"userId": userID},
		}); recordErr != nil {
			return recordErr
		}
		if !admin {
			return nil
		}
		return s.roles.AssertGlobalAdminExists(ctx, tx)
	})
	if err != nil {
		return err
	}
	s.roles.InvalidateUsers(userID)
	return nil
}

func (s *GroupService) lockGroupChange(ctx context.Context, tx *gorm.DB, caller *authz.PermissionSet, groupID string) (Group, error) {
	group, err := lockGroup(tx, groupID)
	if err != nil {
		return Group{}, err
	}
	var bindings []role.RoleBinding
	if findErr := tx.Where("group_id = ?", groupID).Order("role_id").Find(&bindings).Error; findErr != nil {
		return Group{}, fmt.Errorf("failed to list group bindings: %w", findErr)
	}
	for _, b := range bindings {
		if validateErr := s.roles.ValidateBindingAgainstCaller(ctx, tx, caller, b.RoleID, b.EnvironmentID, b.Selector); validateErr != nil {
			return Group{}, validateErr
		}
	}
	return group, nil
}

func (s *GroupService) SyncOidcMemberships(ctx context.Context, userID string, claimValues []string) error {
	desired := []string{}
	if len(claimValues) > 0 {
		if err := s.db.WithContext(ctx).Model(&OidcGroupMapping{}).Where("claim_value IN ?", claimValues).Distinct("group_id").Pluck("group_id", &desired).Error; err != nil {
			return fmt.Errorf("failed to resolve OIDC group mappings: %w", err)
		}
	}
	err := dbutil.WithTx(ctx, s.db.DB, func(tx *gorm.DB) error {
		return s.replaceOidcMemberships(ctx, tx, userID, desired, true)
	})
	if errors.Is(err, common.ErrNoGlobalAdminRemains) {
		slog.WarnContext(ctx, "OIDC group sync would remove the last global admin; keeping existing memberships", "userId", userID)
		err = dbutil.WithTx(ctx, s.db.DB, func(tx *gorm.DB) error {
			if replaceErr := s.replaceOidcMemberships(ctx, tx, userID, desired, false); replaceErr != nil {
				return replaceErr
			}
			return audit.RecordInTx(ctx, tx, types.AuditEntry{
				Action:     types.AuditActionGroupOidcAdminKept,
				ActorType:  types.AuditActorOidc,
				TargetType: types.AuditTargetUser,
				TargetID:   userID,
				Metadata:   map[string]any{"claimValues": claimValues},
			})
		})
	}
	if err != nil {
		return err
	}
	s.roles.InvalidateUsers(userID)
	var memberships, bindings int64
	if countErr := s.db.WithContext(ctx).Model(&GroupMember{}).Where("user_id = ?", userID).Count(&memberships).Error; countErr != nil {
		return fmt.Errorf("failed to count group memberships: %w", countErr)
	}
	if countErr := s.db.WithContext(ctx).Model(&role.RoleBinding{}).Where("user_id = ?", userID).Count(&bindings).Error; countErr != nil {
		return fmt.Errorf("failed to count role bindings: %w", countErr)
	}
	if memberships > 0 || bindings > 0 {
		return nil
	}
	return s.ApplyDefaultRole(ctx, userID)
}

func (s *GroupService) replaceOidcMemberships(ctx context.Context, tx *gorm.DB, userID string, desired []string, removeStale bool) error {
	if err := s.roles.LockGlobalAdminGuard(tx); err != nil {
		return err
	}
	var current []string
	if err := tx.Model(&GroupMember{}).Where("user_id = ? AND source = ?", userID, MemberSourceOidc).Pluck("group_id", &current).Error; err != nil {
		return fmt.Errorf("failed to load OIDC memberships: %w", err)
	}
	stale := slices.DeleteFunc(slices.Clone(current), func(groupID string) bool {
		return slices.Contains(desired, groupID)
	})
	if removeStale && len(stale) > 0 {
		admin, err := grantsAdmin(tx, stale...)
		if err != nil {
			return err
		}
		if deleteErr := tx.Where("user_id = ? AND source = ? AND group_id IN ?", userID, MemberSourceOidc, stale).Delete(&GroupMember{}).Error; deleteErr != nil {
			return fmt.Errorf("failed to remove stale OIDC memberships: %w", deleteErr)
		}
		if admin {
			if assertErr := s.roles.AssertGlobalAdminExists(ctx, tx); assertErr != nil {
				return assertErr
			}
		}
	}
	if removeStale {
		var legacyRoles []string
		if err := tx.Model(&role.RoleBinding{}).Where("user_id = ? AND source = ?", userID, role.BindingSourceOidc).Pluck("role_id", &legacyRoles).Error; err != nil {
			return fmt.Errorf("failed to load legacy OIDC bindings: %w", err)
		}
		if len(legacyRoles) > 0 {
			if deleteErr := tx.Where("user_id = ? AND source = ?", userID, role.BindingSourceOidc).Delete(&role.RoleBinding{}).Error; deleteErr != nil {
				return fmt.Errorf("failed to remove legacy OIDC bindings: %w", deleteErr)
			}
			if slices.Contains(legacyRoles, authz.BuiltInRoleAdmin) {
				if assertErr := s.roles.AssertGlobalAdminExists(ctx, tx); assertErr != nil {
					return assertErr
				}
			}
		}
	}
	missing := slices.DeleteFunc(slices.Clone(desired), func(groupID string) bool {
		return slices.Contains(current, groupID)
	})
	if len(missing) == 0 {
		return nil
	}
	rows := make([]GroupMember, len(missing))
	for i, groupID := range missing {
		rows[i] = GroupMember{
			GroupID: groupID,
			UserID:  userID,
			Source:  MemberSourceOidc,
		}
	}
	return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&rows).Error
}

func (s *GroupService) ApplyDefaultRole(ctx context.Context, userID string) error {
	roleID := strings.TrimSpace(s.defaultRoleID(ctx))
	if roleID == "" {
		return nil
	}
	if roleID == authz.BuiltInRoleAdmin {
		slog.WarnContext(ctx, "Ignoring defaultUserRoleId because the Admin role cannot be granted by default", "userId", userID)
		return nil
	}
	if _, err := s.roles.GetRole(ctx, roleID); err != nil {
		if errors.Is(err, common.ErrRoleNotFound) {
			slog.WarnContext(ctx, "Ignoring defaultUserRoleId because the role does not exist", "roleId", roleID, "userId", userID)
			return nil
		}
		return err
	}
	return s.roles.BindUser(ctx, userID, roleID)
}

func (s *GroupService) mappings(ctx context.Context) *gorm.DB {
	return s.db.WithContext(ctx).
		Table("oidc_group_mappings AS m").
		Select("m.id AS id, m.claim_value AS claim_value, m.group_id AS group_id, g.name AS group_name, m.source AS source, m.created_at AS created_at, m.updated_at AS updated_at").
		Joins("INNER JOIN groups g ON g.id = m.group_id")
}

func (s *GroupService) ListOidcMappings(ctx context.Context) ([]types.OidcGroupMapping, error) {
	out := []types.OidcGroupMapping{}
	if err := s.mappings(ctx).Order("m.claim_value, g.name").Scan(&out).Error; err != nil {
		return nil, fmt.Errorf("failed to list OIDC group mappings: %w", err)
	}
	return out, nil
}

func (s *GroupService) getOidcMapping(ctx context.Context, id string) (types.OidcGroupMapping, error) {
	var out types.OidcGroupMapping
	if err := s.mappings(ctx).Where("m.id = ?", id).Take(&out).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return types.OidcGroupMapping{}, common.ErrOidcMappingNotFound
		}
		return types.OidcGroupMapping{}, fmt.Errorf("failed to load OIDC group mapping: %w", err)
	}
	return out, nil
}

func (s *GroupService) CreateOidcMapping(ctx context.Context, caller *authz.PermissionSet, spec types.OidcGroupMappingSpec) (types.OidcGroupMapping, error) {
	claimValue := strings.TrimSpace(spec.ClaimValue)
	if claimValue == "" {
		return types.OidcGroupMapping{}, common.Classify(common.ErrValidation, errors.New("claim value is required"))
	}
	created := OidcGroupMapping{
		ClaimValue: claimValue,
		GroupID:    spec.GroupID,
		Source:     MappingSourceManual,
	}
	err := dbutil.WithTx(ctx, s.db.DB, func(tx *gorm.DB) error {
		if lockErr := s.roles.LockGlobalAdminGuard(tx); lockErr != nil {
			return lockErr
		}
		if _, err := s.lockGroupChange(ctx, tx, caller, spec.GroupID); err != nil {
			return err
		}
		if err := ensureMappingAvailable(tx, claimValue, spec.GroupID, ""); err != nil {
			return err
		}
		if err := tx.Create(&created).Error; err != nil {
			return err
		}
		return audit.RecordInTx(ctx, tx, types.AuditEntry{
			Action:     types.AuditActionOidcMappingCreate,
			TargetType: types.AuditTargetOidcMapping,
			TargetID:   created.ID,
			TargetName: claimValue,
			After:      spec,
		})
	})
	if err != nil {
		return types.OidcGroupMapping{}, err
	}
	return s.getOidcMapping(ctx, created.ID)
}

func (s *GroupService) loadManualMapping(ctx context.Context, tx *gorm.DB, caller *authz.PermissionSet, id string) (OidcGroupMapping, error) {
	existing, err := dbutil.FirstWhere[OidcGroupMapping](ctx, tx, common.ErrOidcMappingNotFound, "id = ?", id)
	if err != nil {
		return OidcGroupMapping{}, err
	}
	if existing.Source == MappingSourceEnv {
		return OidcGroupMapping{}, common.ErrOidcMappingEnvManaged
	}
	if _, lockErr := s.lockGroupChange(ctx, tx, caller, existing.GroupID); lockErr != nil {
		return OidcGroupMapping{}, lockErr
	}
	return *existing, nil
}

func (s *GroupService) UpdateOidcMapping(ctx context.Context, caller *authz.PermissionSet, id string, spec types.OidcGroupMappingSpec) (types.OidcGroupMapping, error) {
	claimValue := strings.TrimSpace(spec.ClaimValue)
	if claimValue == "" {
		return types.OidcGroupMapping{}, common.Classify(common.ErrValidation, errors.New("claim value is required"))
	}
	err := dbutil.WithTx(ctx, s.db.DB, func(tx *gorm.DB) error {
		if lockErr := s.roles.LockGlobalAdminGuard(tx); lockErr != nil {
			return lockErr
		}
		existing, loadErr := s.loadManualMapping(ctx, tx, caller, id)
		if loadErr != nil {
			return loadErr
		}
		if spec.GroupID != existing.GroupID {
			if _, lockErr := s.lockGroupChange(ctx, tx, caller, spec.GroupID); lockErr != nil {
				return lockErr
			}
		}
		if availableErr := ensureMappingAvailable(tx, claimValue, spec.GroupID, id); availableErr != nil {
			return availableErr
		}
		if saveErr := tx.Save(&OidcGroupMapping{
			BaseModel:  existing.BaseModel,
			ClaimValue: claimValue,
			GroupID:    spec.GroupID,
			Source:     existing.Source,
		}).Error; saveErr != nil {
			return saveErr
		}
		return audit.RecordInTx(ctx, tx, types.AuditEntry{
			Action:     types.AuditActionOidcMappingUpdate,
			TargetType: types.AuditTargetOidcMapping,
			TargetID:   id,
			TargetName: claimValue,
			Before: types.OidcGroupMappingSpec{
				ClaimValue: existing.ClaimValue,
				GroupID:    existing.GroupID,
			},
			After: spec,
		})
	})
	if err != nil {
		return types.OidcGroupMapping{}, err
	}
	return s.getOidcMapping(ctx, id)
}

func (s *GroupService) DeleteOidcMapping(ctx context.Context, caller *authz.PermissionSet, id string) error {
	return dbutil.WithTx(ctx, s.db.DB, func(tx *gorm.DB) error {
		if lockErr := s.roles.LockGlobalAdminGuard(tx); lockErr != nil {
			return lockErr
		}
		existing, loadErr := s.loadManualMapping(ctx, tx, caller, id)
		if loadErr != nil {
			return loadErr
		}
		if deleteErr := tx.Delete(&OidcGroupMapping{}, "id = ?", id).Error; deleteErr != nil {
			return fmt.Errorf("failed to delete OIDC group mapping: %w", deleteErr)
		}
		return audit.RecordInTx(ctx, tx, types.AuditEntry{
			Action:     types.AuditActionOidcMappingDelete,
			TargetType: types.AuditTargetOidcMapping,
			TargetID:   id,
			TargetName: existing.ClaimValue,
			Before: types.OidcGroupMappingSpec{
				ClaimValue: existing.ClaimValue,
				GroupID:    existing.GroupID,
			},
		})
	})
}

func (s *GroupService) ReconcileEnvOidcMappings(ctx context.Context, raw string) error {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	specs, err := parseEnvGroupMappings(raw)
	if err != nil {
		return err
	}
	return dbutil.WithTx(ctx, s.db.DB, func(tx *gorm.DB) error {
		if lockErr := s.roles.LockGlobalAdminGuard(tx); lockErr != nil {
			return lockErr
		}
		if deleteErr := tx.Where("source = ?", MappingSourceEnv).Delete(&OidcGroupMapping{}).Error; deleteErr != nil {
			return fmt.Errorf("failed to clear env-managed OIDC group mappings: %w", deleteErr)
		}
		for _, spec := range specs {
			target := Group{Name: spec.groupName}
			if findErr := tx.Where(Group{Name: spec.groupName}).FirstOrCreate(&target).Error; findErr != nil {
				return fmt.Errorf("ARCANE_OIDC_GROUP_MAPPINGS: failed to ensure group %q: %w", spec.groupName, findErr)
			}
			if createErr := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&OidcGroupMapping{
				ClaimValue: spec.claimValue,
				GroupID:    target.ID,
				Source:     MappingSourceEnv,
			}).Error; createErr != nil {
				return fmt.Errorf("ARCANE_OIDC_GROUP_MAPPINGS: failed to map %q: %w", spec.claimValue, createErr)
			}
		}
		slog.InfoContext(ctx, "ARCANE_OIDC_GROUP_MAPPINGS reconciled", "envManagedCount", len(specs))
		return nil
	})
}
