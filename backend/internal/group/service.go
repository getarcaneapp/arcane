package group

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"kit.getarcane.app"
	"kit.getarcane.app/normalization"

	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/arcane/v3/internal/database"
	"getarcane.dev/arcane/v3/internal/role"
	"getarcane.dev/arcane/v3/pkg/pagination"
	"getarcane.dev/arcane/v3/pkg/sqlite"
	"getarcane.dev/types/v3"
)

type (
	GroupService struct {
		db          *database.DB
		roleService *role.RoleService
	}

	GroupMemberDetail struct {
		CreatedAt   time.Time `gorm:"column:created_at"`
		DisplayName *string   `gorm:"column:display_name"`
		Email       *string   `gorm:"column:email"`
		ID          string    `gorm:"column:id"`
		GroupID     string    `gorm:"column:group_id"`
		UserID      string    `gorm:"column:user_id"`
		Username    string    `gorm:"column:username"`
		Source      string    `gorm:"column:source"`
	}
)

func NewGroupService(db *database.DB, roleService *role.RoleService) *GroupService {
	return &GroupService{
		db:          db,
		roleService: roleService,
	}
}

func (s *GroupService) ListGroups(ctx context.Context, params pagination.QueryParams) ([]Group, pagination.Response, error) {
	var groups []Group
	query := s.db.WithContext(ctx).Model(&Group{})

	if term := strings.TrimSpace(params.Search); term != "" {
		pattern := "%" + term + "%"
		query = query.Where("name LIKE ? OR COALESCE(description, '') LIKE ?", pattern, pattern)
	}

	resp, err := pagination.PaginateAndSortDB(params, query, &groups)
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to paginate groups: %w", err)
	}
	return groups, resp, nil
}

func (s *GroupService) GetGroup(ctx context.Context, id string) (*Group, error) {
	return sqlite.FirstWhere[Group](ctx, s.db.DB, common.Classify(common.ErrGroupNotFound, errors.New("Group not found")), "id = ?", id)
}

func (s *GroupService) CreateGroup(ctx context.Context, name string, description *string) (*Group, error) {
	input := types.Group{Name: name, Description: description}
	if err := normalization.Normalize(&input); err != nil {
		return nil, common.Classify(common.ErrValidation, err)
	}
	if input.Name == "" {
		return nil, common.Classify(common.ErrValidation, errors.New("group name is required"))
	}

	group := &Group{
		Name:        input.Name,
		Description: input.Description,
	}
	err := s.db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var conflict int64
		if err := tx.Model(&Group{}).Where("name = ?", input.Name).Count(&conflict).Error; err != nil {
			return fmt.Errorf("failed to check group name uniqueness: %w", err)
		}
		if conflict > 0 {
			return common.Classify(common.ErrGroupNameTaken, errors.New("Group name already in use"))
		}
		return tx.Create(group).Error
	})
	if err != nil {
		return nil, err
	}
	return group, nil
}

func (s *GroupService) UpdateGroup(ctx context.Context, id, name string, description *string) (*Group, error) {
	input := types.Group{Name: name, Description: description}
	if err := normalization.Normalize(&input); err != nil {
		return nil, common.Classify(common.ErrValidation, err)
	}
	if input.Name == "" {
		return nil, common.Classify(common.ErrValidation, errors.New("group name is required"))
	}

	var out Group
	err := s.db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing Group
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", id).First(&existing).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return common.Classify(common.ErrGroupNotFound, errors.New("Group not found"))
			}
			return fmt.Errorf("failed to load group: %w", err)
		}
		if input.Name != existing.Name {
			var conflict int64
			if err := tx.Model(&Group{}).Where("name = ? AND id <> ?", input.Name, id).Count(&conflict).Error; err != nil {
				return fmt.Errorf("failed to check group name uniqueness: %w", err)
			}
			if conflict > 0 {
				return common.Classify(common.ErrGroupNameTaken, errors.New("Group name already in use"))
			}
		}
		existing.Name = input.Name
		existing.Description = input.Description
		if err := tx.Save(&existing).Error; err != nil {
			return fmt.Errorf("failed to update group: %w", err)
		}
		out = existing
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (s *GroupService) DeleteGroup(ctx context.Context, id string) error {
	var affected []string
	err := s.db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := s.roleService.LockGlobalAdminGuard(tx); err != nil {
			return err
		}
		var existing Group
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", id).First(&existing).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return common.Classify(common.ErrGroupNotFound, errors.New("Group not found"))
			}
			return fmt.Errorf("failed to load group: %w", err)
		}
		ids, err := lockGroupMemberUserRows(tx, id)
		if err != nil {
			return err
		}
		affected = ids
		if deleteErr := tx.Delete(&Group{}, "id = ?", id).Error; deleteErr != nil {
			return fmt.Errorf("failed to delete group: %w", deleteErr)
		}
		if len(ids) == 0 {
			return nil
		}
		return s.roleService.AssertGlobalAdminExists(ctx, tx)
	})
	if err != nil {
		return err
	}
	for _, userID := range affected {
		s.roleService.InvalidateUser(userID)
	}
	return nil
}

func (s *GroupService) CountMembers(ctx context.Context, groupID string) (int, error) {
	var count int64
	if err := s.db.WithContext(ctx).
		Model(&GroupMember{}).
		Distinct("user_id").
		Where("group_id = ?", groupID).
		Count(&count).Error; err != nil {
		return 0, fmt.Errorf("failed to count group members: %w", err)
	}
	return int(count), nil
}

func (s *GroupService) CountRoleAssignments(ctx context.Context, groupID string) (int, error) {
	var count int64
	if err := s.db.WithContext(ctx).
		Model(&GroupRoleAssignment{}).
		Where("group_id = ?", groupID).
		Count(&count).Error; err != nil {
		return 0, fmt.Errorf("failed to count group role assignments: %w", err)
	}
	return int(count), nil
}

func (s *GroupService) ListMembers(ctx context.Context, groupID string) ([]GroupMemberDetail, error) {
	if _, err := s.GetGroup(ctx, groupID); err != nil {
		return nil, err
	}
	var out []GroupMemberDetail
	if err := s.db.WithContext(ctx).
		Table("group_members AS gm").
		Select("gm.id, gm.group_id, gm.user_id, gm.source, gm.created_at, u.username, u.display_name, u.email").
		Joins("INNER JOIN users u ON u.id = gm.user_id").
		Where("gm.group_id = ?", groupID).
		Order("u.username ASC, gm.source ASC").
		Scan(&out).Error; err != nil {
		return nil, fmt.Errorf("failed to list group members: %w", err)
	}
	return out, nil
}

func (s *GroupService) ReplaceMembers(ctx context.Context, groupID, source string, userIDs []string) error {
	seen := make(map[string]struct{}, len(userIDs))
	deduped := make([]string, 0, len(userIDs))
	for _, id := range userIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		deduped = append(deduped, id)
	}

	var affected []string
	err := s.db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := s.roleService.LockGlobalAdminGuard(tx); err != nil {
			return err
		}
		var existing Group
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", groupID).First(&existing).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return common.Classify(common.ErrGroupNotFound, errors.New("Group not found"))
			}
			return fmt.Errorf("failed to load group: %w", err)
		}
		if err := verifyIDsExist(tx.Table("users"), seen, "user"); err != nil {
			return err
		}

		currentIDs, err := lockGroupMemberUserRows(tx, groupID)
		if err != nil {
			return err
		}
		affectedSet := make(map[string]struct{}, len(currentIDs)+len(deduped))
		for _, id := range currentIDs {
			affectedSet[id] = struct{}{}
		}
		for _, id := range deduped {
			affectedSet[id] = struct{}{}
		}
		affected = slices.Collect(maps.Keys(affectedSet))

		if clearErr := tx.Where("group_id = ? AND source = ?", groupID, source).Delete(&GroupMember{}).Error; clearErr != nil {
			return fmt.Errorf("failed to clear %s group members: %w", source, clearErr)
		}
		if len(deduped) > 0 {
			rows := make([]GroupMember, len(deduped))
			for i, uid := range deduped {
				rows[i] = GroupMember{
					GroupID: groupID,
					UserID:  uid,
					Source:  source,
				}
			}
			if insertErr := tx.Create(&rows).Error; insertErr != nil {
				return fmt.Errorf("failed to insert %s group members: %w", source, insertErr)
			}
		}
		return s.roleService.AssertGlobalAdminExists(ctx, tx)
	})
	if err != nil {
		return err
	}
	for _, userID := range affected {
		s.roleService.InvalidateUser(userID)
	}
	return nil
}

func (s *GroupService) ListRoleAssignments(ctx context.Context, groupID string) ([]GroupRoleAssignment, error) {
	if _, err := s.GetGroup(ctx, groupID); err != nil {
		return nil, err
	}
	var out []GroupRoleAssignment
	if err := s.db.WithContext(ctx).
		Where("group_id = ?", groupID).
		Order("source ASC, role_id ASC").
		Find(&out).Error; err != nil {
		return nil, fmt.Errorf("failed to list group role assignments: %w", err)
	}
	return out, nil
}

func (s *GroupService) ReplaceRoleAssignments(ctx context.Context, groupID, source string, desired []GroupRoleAssignment) error {
	seen := make(map[string]struct{}, len(desired))
	desired = slices.DeleteFunc(slices.Clone(desired), func(a GroupRoleAssignment) bool {
		key := a.RoleID + "\x00" + kit.FromPtr(a.EnvironmentID)
		if _, dup := seen[key]; dup {
			return true
		}
		seen[key] = struct{}{}
		return false
	})
	for i := range desired {
		desired[i].GroupID = groupID
		desired[i].Source = source
	}

	var affected []string
	err := s.db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := s.roleService.LockGlobalAdminGuard(tx); err != nil {
			return err
		}
		var existing Group
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", groupID).First(&existing).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return common.Classify(common.ErrGroupNotFound, errors.New("Group not found"))
			}
			return fmt.Errorf("failed to load group: %w", err)
		}
		if err := validateGroupAssignmentsExist(tx, desired); err != nil {
			return err
		}
		ids, err := lockGroupMemberUserRows(tx, groupID)
		if err != nil {
			return err
		}
		affected = ids

		if clearErr := tx.Where("group_id = ? AND source = ?", groupID, source).Delete(&GroupRoleAssignment{}).Error; clearErr != nil {
			return fmt.Errorf("failed to clear %s group role assignments: %w", source, clearErr)
		}
		if len(desired) > 0 {
			if insertErr := tx.Create(&desired).Error; insertErr != nil {
				return fmt.Errorf("failed to insert %s group role assignments: %w", source, insertErr)
			}
		}
		return s.roleService.AssertGlobalAdminExists(ctx, tx)
	})
	if err != nil {
		return err
	}
	for _, userID := range affected {
		s.roleService.InvalidateUser(userID)
	}
	return nil
}

func lockGroupMemberUserRows(tx *gorm.DB, groupID string) ([]string, error) {
	var ids []string
	if err := tx.Model(&GroupMember{}).
		Where("group_id = ?", groupID).
		Distinct("user_id").
		Pluck("user_id", &ids).Error; err != nil {
		return nil, fmt.Errorf("failed to list group members: %w", err)
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
		return nil, fmt.Errorf("failed to lock group member users: %w", err)
	}
	return ids, nil
}

func validateGroupAssignmentsExist(tx *gorm.DB, desired []GroupRoleAssignment) error {
	roleIDSet := make(map[string]struct{}, len(desired))
	envIDSet := make(map[string]struct{}, len(desired))
	for _, a := range desired {
		roleIDSet[a.RoleID] = struct{}{}
		if a.EnvironmentID != nil {
			envIDSet[*a.EnvironmentID] = struct{}{}
		}
	}
	if err := verifyIDsExist(tx.Table("roles"), roleIDSet, "role"); err != nil {
		return err
	}
	return verifyIDsExist(tx.Table("environments"), envIDSet, "environment")
}

func verifyIDsExist(query *gorm.DB, idSet map[string]struct{}, kind string) error {
	if len(idSet) == 0 {
		return nil
	}
	var found []string
	if err := query.Where("id IN ?", slices.Collect(maps.Keys(idSet))).Pluck("id", &found).Error; err != nil {
		return fmt.Errorf("failed to verify %s ids: %w", kind, err)
	}
	foundSet := make(map[string]struct{}, len(found))
	for _, id := range found {
		foundSet[id] = struct{}{}
	}
	for id := range idSet {
		if _, ok := foundSet[id]; !ok {
			return common.Classify(common.ErrInvalidRoleAssignment, fmt.Errorf("invalid assignment: %s %q does not exist", kind, id))
		}
	}
	return nil
}
