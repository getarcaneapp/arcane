package group

import (
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/getarcaneapp/arcane/backend/v3/internal/common"
	"github.com/getarcaneapp/arcane/backend/v3/internal/role"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/authz"
)

type envGroupMapping struct {
	claimValue string
	groupName  string
}

func parseEnvGroupMappings(raw string) ([]envGroupMapping, error) {
	var specs []envGroupMapping
	for i, entry := range strings.Split(raw, ",") {
		if strings.TrimSpace(entry) == "" {
			continue
		}
		claimValue, groupName, ok := strings.Cut(entry, "=")
		if !ok || strings.TrimSpace(claimValue) == "" || strings.TrimSpace(groupName) == "" {
			return nil, fmt.Errorf("ARCANE_OIDC_GROUP_MAPPINGS[%d]: expected claim=GroupName, got %q", i, strings.TrimSpace(entry))
		}
		specs = append(specs, envGroupMapping{
			claimValue: strings.TrimSpace(claimValue),
			groupName:  strings.TrimSpace(groupName),
		})
	}
	return specs, nil
}

func lockGroup(tx *gorm.DB, id string) (Group, error) {
	var existing Group
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", id).First(&existing).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Group{}, common.ErrGroupNotFound
		}
		return Group{}, fmt.Errorf("failed to load group: %w", err)
	}
	return existing, nil
}

func ensureGroupNameAvailable(tx *gorm.DB, name, excludeID string) error {
	var conflicts int64
	if err := tx.Model(&Group{}).Where("name = ? AND id <> ?", name, excludeID).Count(&conflicts).Error; err != nil {
		return fmt.Errorf("failed to check group name uniqueness: %w", err)
	}
	if conflicts > 0 {
		return common.ErrGroupNameTaken
	}
	return nil
}

func ensureMappingAvailable(tx *gorm.DB, claimValue, groupID, excludeID string) error {
	var conflicts int64
	if err := tx.Model(&OidcGroupMapping{}).Where("claim_value = ? AND group_id = ? AND id <> ?", claimValue, groupID, excludeID).Count(&conflicts).Error; err != nil {
		return fmt.Errorf("failed to check OIDC group mapping uniqueness: %w", err)
	}
	if conflicts > 0 {
		return common.Classify(common.ErrConflict, errors.New("this claim value is already mapped to the group"))
	}
	return nil
}

func memberIDs(tx *gorm.DB, groupID string) ([]string, error) {
	var ids []string
	if err := tx.Model(&GroupMember{}).Where("group_id = ?", groupID).Distinct("user_id").Pluck("user_id", &ids).Error; err != nil {
		return nil, fmt.Errorf("failed to list group members: %w", err)
	}
	return ids, nil
}

func grantsAdmin(tx *gorm.DB, groupIDs ...string) (bool, error) {
	var count int64
	if err := tx.Model(&role.RoleBinding{}).
		Where("group_id IN ? AND role_id = ? AND environment_id IS NULL AND selector_hash = ''", groupIDs, authz.BuiltInRoleAdmin).
		Count(&count).Error; err != nil {
		return false, fmt.Errorf("failed to check group admin bindings: %w", err)
	}
	return count > 0, nil
}

func countByGroup(query *gorm.DB, aggregate string, groupIDs []string) (map[string]int, error) {
	out := make(map[string]int, len(groupIDs))
	if len(groupIDs) == 0 {
		return out, nil
	}
	var rows []struct {
		GroupID string
		Total   int
	}
	if err := query.Select("group_id, "+aggregate+" AS total").Where("group_id IN ?", groupIDs).Group("group_id").Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		out[row.GroupID] = row.Total
	}
	return out, nil
}

func groupSnapshot(group Group) map[string]any {
	return map[string]any{
		"name":        group.Name,
		"description": group.Description,
	}
}
