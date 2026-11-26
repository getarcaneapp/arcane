package middleware

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"kit.getarcane.app"

	"getarcane.dev/arcane/v3/internal/database"
	"getarcane.dev/arcane/v3/pkg/authz"
)

const composeProjectLabel = "com.docker.compose.project"

// AllowedProjectIDs returns nil when the caller has unrestricted env/global access for perm
// (or when no request PermissionSet is present, such as internal background jobs).
func AllowedProjectIDs(ctx context.Context, envID, perm string) []string {
	ps, ok := PermissionsFromContext(ctx)
	if !ok {
		return nil
	}
	return ps.ResourceIDs(perm, envID, authz.ResourceTypeProject)
}

// AllowedComposeProjects returns (nil, true, nil) when the caller has unrestricted access for perm.
// Otherwise it returns the lowercase set of compose project names owned by granted projects.
func AllowedComposeProjects(ctx context.Context, db *database.DB, envID, perm string) (map[string]struct{}, bool, error) {
	ps, ok := PermissionsFromContext(ctx)
	if !ok || ps.Allows(perm, envID) {
		return nil, true, nil
	}
	projectIDs := ps.ResourceIDs(perm, envID, authz.ResourceTypeProject)
	if len(projectIDs) == 0 || db == nil {
		return map[string]struct{}{}, false, nil
	}
	type projRow struct {
		ComposeProjectName *string `gorm:"column:compose_project_name"`
		DirName            *string `gorm:"column:dir_name"`
		Name               string  `gorm:"column:name"`
	}
	var rows []projRow
	if err := db.WithContext(ctx).
		Table("projects").
		Select("name, dir_name, compose_project_name").
		Where("id IN ?", projectIDs).
		Scan(&rows).Error; err != nil {
		return nil, false, fmt.Errorf("failed to resolve allowed compose projects: %w", err)
	}
	allowed := make(map[string]struct{}, len(rows)*2)
	for _, r := range rows {
		for _, candidate := range []string{kit.FromPtr(r.ComposeProjectName), kit.FromPtr(r.DirName), r.Name} {
			if name := strings.ToLower(strings.TrimSpace(candidate)); name != "" {
				allowed[name] = struct{}{}
			}
		}
	}
	return allowed, false, nil
}

// AllowedProjectImageRefs returns (nil, true, nil) when the caller has unrestricted access for perm.
// Otherwise it returns the set of image refs referenced by projects granted perm on envID.
func AllowedProjectImageRefs(ctx context.Context, db *database.DB, envID, perm string) (map[string]struct{}, bool, error) {
	ps, ok := PermissionsFromContext(ctx)
	if !ok || ps.Allows(perm, envID) {
		return nil, true, nil
	}
	if perm != authz.PermImagesList && perm != authz.PermImagesRead {
		return map[string]struct{}{}, false, nil
	}
	projectIDs := ps.ResourceIDs(perm, envID, authz.ResourceTypeProject)
	if len(projectIDs) == 0 || db == nil {
		return map[string]struct{}{}, false, nil
	}
	var rawRefs []string
	if err := db.WithContext(ctx).
		Table("projects").
		Where("id IN ?", projectIDs).
		Pluck("image_refs_json", &rawRefs).Error; err != nil {
		return nil, false, fmt.Errorf("failed to resolve allowed project image refs: %w", err)
	}
	allowed := make(map[string]struct{})
	for _, raw := range rawRefs {
		if raw == "" || raw == "[]" {
			continue
		}
		var refs []string
		if err := json.Unmarshal([]byte(raw), &refs); err != nil {
			continue
		}
		for _, ref := range refs {
			if norm := strings.ToLower(strings.TrimSpace(ref)); norm != "" {
				allowed[norm] = struct{}{}
			}
		}
	}
	return allowed, false, nil
}

func ContainerPermitted(ps *authz.PermissionSet, envID, perm, containerID string, names []string, labels map[string]string, allowedCompose map[string]struct{}) bool {
	if ps.AllowsResource(perm, envID, authz.ResourceTypeContainer, containerID) {
		return true
	}
	for _, name := range names {
		trimmed := strings.TrimPrefix(strings.TrimSpace(name), "/")
		if trimmed != "" && ps.AllowsResource(perm, envID, authz.ResourceTypeContainer, trimmed) {
			return true
		}
	}
	if len(allowedCompose) > 0 && labels != nil {
		if projectName := strings.ToLower(strings.TrimSpace(labels[composeProjectLabel])); projectName != "" {
			if _, ok := allowedCompose[projectName]; ok {
				return true
			}
		}
	}
	return false
}

func ComposeResourcePermitted(ps *authz.PermissionSet, envID, perm, resourceType, resourceID, resourceName string, labels map[string]string, allowedCompose map[string]struct{}) bool {
	if ps.AllowsResource(perm, envID, resourceType, resourceID) {
		return true
	}
	if resourceName != "" && ps.AllowsResource(perm, envID, resourceType, resourceName) {
		return true
	}
	if len(allowedCompose) > 0 && labels != nil {
		if projectName := strings.ToLower(strings.TrimSpace(labels[composeProjectLabel])); projectName != "" {
			if _, ok := allowedCompose[projectName]; ok {
				return true
			}
		}
	}
	return false
}

func ImagePermitted(ps *authz.PermissionSet, envID, perm, imageID string, repoTags []string, allowedRefs map[string]struct{}) bool {
	if ps.AllowsResource(perm, envID, authz.ResourceTypeImage, imageID) {
		return true
	}
	for _, tag := range repoTags {
		if tag != "" && ps.AllowsResource(perm, envID, authz.ResourceTypeImage, tag) {
			return true
		}
		if len(allowedRefs) > 0 {
			if _, ok := allowedRefs[strings.ToLower(strings.TrimSpace(tag))]; ok {
				return true
			}
		}
	}
	return false
}

func GitOpsSyncPermitted(ps *authz.PermissionSet, envID, perm, syncID string, projectID *string) bool {
	if ps.AllowsResource(perm, envID, authz.ResourceTypeGitOps, syncID) {
		return true
	}
	if pid := strings.TrimSpace(kit.FromPtr(projectID)); pid != "" {
		return ps.AllowsResource(perm, envID, authz.ResourceTypeProject, pid)
	}
	return false
}

func RequireContainerPermission(ctx context.Context, db *database.DB, envID, perm, containerID string, names []string, labels map[string]string) error {
	ps, _ := PermissionsFromContext(ctx)
	if ps.Allows(perm, envID) {
		return nil
	}
	allowedCompose, _, err := AllowedComposeProjects(ctx, db, envID, perm)
	if err != nil {
		return huma.Error500InternalServerError(err.Error())
	}
	if !ContainerPermitted(ps, envID, perm, containerID, names, labels, allowedCompose) {
		return huma.Error403Forbidden("permission denied: " + perm)
	}
	return nil
}

func RequireComposeResourcePermission(ctx context.Context, db *database.DB, envID, perm, resourceType, resourceID, resourceName string, labels map[string]string) error {
	ps, _ := PermissionsFromContext(ctx)
	if ps.Allows(perm, envID) {
		return nil
	}
	allowedCompose, _, err := AllowedComposeProjects(ctx, db, envID, perm)
	if err != nil {
		return huma.Error500InternalServerError(err.Error())
	}
	if !ComposeResourcePermitted(ps, envID, perm, resourceType, resourceID, resourceName, labels, allowedCompose) {
		return huma.Error403Forbidden("permission denied: " + perm)
	}
	return nil
}

func RequireImagePermission(ctx context.Context, db *database.DB, envID, perm, imageID string, repoTags []string) error {
	ps, _ := PermissionsFromContext(ctx)
	if ps.Allows(perm, envID) {
		return nil
	}
	allowedRefs, _, err := AllowedProjectImageRefs(ctx, db, envID, perm)
	if err != nil {
		return huma.Error500InternalServerError(err.Error())
	}
	if !ImagePermitted(ps, envID, perm, imageID, repoTags, allowedRefs) {
		return huma.Error403Forbidden("permission denied: " + perm)
	}
	return nil
}

func RequireGitOpsSyncPermission(ctx context.Context, envID, perm, syncID string, projectID *string) error {
	ps, _ := PermissionsFromContext(ctx)
	if !GitOpsSyncPermitted(ps, envID, perm, syncID, projectID) {
		return huma.Error403Forbidden("permission denied: " + perm)
	}
	return nil
}
