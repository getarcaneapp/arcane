package middleware

import (
	"cmp"
	"context"
	"encoding/json/v2"
	"fmt"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	kit "kit.getarcane.app"
	"kit.getarcane.app/updater/digest"
	"kit.getarcane.app/updater/refs"

	"getarcane.dev/arcane/v3/internal/database"
	"getarcane.dev/arcane/v3/pkg/authz"
	"getarcane.dev/arcane/v3/pkg/projects"
)

type (
	ContainerAccessInput struct {
		Labels         map[string]string
		AllowedCompose map[string]struct{}
		EnvID          string
		Permission     string
		ContainerID    string
		Names          []string
	}

	ComposeResourceAccessInput struct {
		Labels         map[string]string
		AllowedCompose map[string]struct{}
		EnvID          string
		Permission     string
		ResourceType   string
		ResourceID     string
		ResourceName   string
	}

	ImageAccessInput struct {
		AllowedRefs map[string]struct{}
		EnvID       string
		Permission  string
		ImageID     string
		RepoTags    []string
		RepoDigests []string
	}

	GitOpsSyncAccessInput struct {
		ProjectID  *string
		EnvID      string
		Permission string
		SyncID     string
	}
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
// Otherwise it returns the lowercase set of canonical compose project names owned by granted projects.
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
	allowed := make(map[string]struct{}, len(rows))
	for _, r := range rows {
		canonical := cmp.Or(
			strings.TrimSpace(kit.FromPtr(r.ComposeProjectName)),
			projects.NormalizeProjectName(strings.TrimSpace(kit.FromPtr(r.DirName))),
			strings.TrimSpace(r.Name),
		)
		if name := strings.ToLower(canonical); name != "" {
			allowed[name] = struct{}{}
		}
	}
	return allowed, false, nil
}

// AllowedProjectImageRefs returns (nil, true, nil) when the caller has unrestricted access for perm.
// Otherwise it returns the canonical set of image references used by projects granted perm on envID.
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
		var imageRefs []string
		if err := json.Unmarshal([]byte(raw), &imageRefs); err != nil {
			continue
		}
		for _, imageRef := range imageRefs {
			for _, key := range canonicalImageRefKeys(imageRef) {
				allowed[key] = struct{}{}
			}
		}
	}
	return allowed, false, nil
}

// canonicalImageRefKeys normalizes registry/repository casing and default tag while preserving
// Docker's case-sensitive tag spelling and supporting digest-pinned references.
func canonicalImageRefKeys(raw string) []string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed == "<none>:<none>" || trimmed == "<none>@<none>" {
		return nil
	}
	parsed, err := refs.NormalizeReference(trimmed)
	if err != nil {
		return []string{trimmed}
	}
	repoKey := strings.ToLower(parsed.RegistryHost) + "/" + strings.ToLower(parsed.Repository)
	if dig, ok := digest.FromReferenceSuffix(trimmed); ok {
		return []string{repoKey + "@" + strings.ToLower(dig)}
	}
	tag := cmp.Or(strings.TrimSpace(parsed.Tag), "latest")
	return []string{repoKey + ":" + tag}
}

func ContainerPermitted(ps *authz.PermissionSet, in ContainerAccessInput) bool {
	if ps.AllowsResource(in.Permission, in.EnvID, authz.ResourceTypeContainer, in.ContainerID) {
		return true
	}
	for _, name := range in.Names {
		trimmed := strings.TrimPrefix(strings.TrimSpace(name), "/")
		if trimmed != "" && ps.AllowsResource(in.Permission, in.EnvID, authz.ResourceTypeContainer, trimmed) {
			return true
		}
	}
	if len(in.AllowedCompose) > 0 && in.Labels != nil {
		if projectName := strings.ToLower(strings.TrimSpace(in.Labels[composeProjectLabel])); projectName != "" {
			if _, ok := in.AllowedCompose[projectName]; ok {
				return true
			}
		}
	}
	return false
}

func ComposeResourcePermitted(ps *authz.PermissionSet, in ComposeResourceAccessInput) bool {
	if ps.AllowsResource(in.Permission, in.EnvID, in.ResourceType, in.ResourceID) {
		return true
	}
	if in.ResourceName != "" && ps.AllowsResource(in.Permission, in.EnvID, in.ResourceType, in.ResourceName) {
		return true
	}
	if len(in.AllowedCompose) > 0 && in.Labels != nil {
		if projectName := strings.ToLower(strings.TrimSpace(in.Labels[composeProjectLabel])); projectName != "" {
			if _, ok := in.AllowedCompose[projectName]; ok {
				return true
			}
		}
	}
	return false
}

func ImagePermitted(ps *authz.PermissionSet, in ImageAccessInput) bool {
	if ps.AllowsResource(in.Permission, in.EnvID, authz.ResourceTypeImage, in.ImageID) {
		return true
	}
	for _, tag := range in.RepoTags {
		if tag != "" && ps.AllowsResource(in.Permission, in.EnvID, authz.ResourceTypeImage, tag) {
			return true
		}
		if len(in.AllowedRefs) > 0 {
			for _, key := range canonicalImageRefKeys(tag) {
				if _, ok := in.AllowedRefs[key]; ok {
					return true
				}
			}
		}
	}
	for _, repoDigest := range in.RepoDigests {
		if repoDigest != "" && ps.AllowsResource(in.Permission, in.EnvID, authz.ResourceTypeImage, repoDigest) {
			return true
		}
		if len(in.AllowedRefs) > 0 {
			for _, key := range canonicalImageRefKeys(repoDigest) {
				if _, ok := in.AllowedRefs[key]; ok {
					return true
				}
			}
		}
	}
	return false
}

func GitOpsSyncPermitted(ps *authz.PermissionSet, in GitOpsSyncAccessInput) bool {
	if ps.AllowsResource(in.Permission, in.EnvID, authz.ResourceTypeGitOps, in.SyncID) {
		return true
	}
	if pid := strings.TrimSpace(kit.FromPtr(in.ProjectID)); pid != "" {
		return ps.AllowsResource(in.Permission, in.EnvID, authz.ResourceTypeProject, pid)
	}
	return false
}

func RequireComposeResourcePermission(ctx context.Context, db *database.DB, in ComposeResourceAccessInput) error {
	ps, _ := PermissionsFromContext(ctx)
	if ps.Allows(in.Permission, in.EnvID) {
		return nil
	}
	allowedCompose, _, err := AllowedComposeProjects(ctx, db, in.EnvID, in.Permission)
	if err != nil {
		return huma.Error500InternalServerError(err.Error())
	}
	if !ComposeResourcePermitted(ps, ComposeResourceAccessInput{ //exhaustruct:enforce
		Labels:         in.Labels,
		AllowedCompose: allowedCompose,
		EnvID:          in.EnvID,
		Permission:     in.Permission,
		ResourceType:   in.ResourceType,
		ResourceID:     in.ResourceID,
		ResourceName:   in.ResourceName,
	}) {
		return huma.Error403Forbidden("permission denied: " + in.Permission)
	}
	return nil
}

func RequireImagePermission(ctx context.Context, db *database.DB, in ImageAccessInput) error {
	ps, _ := PermissionsFromContext(ctx)
	if ps.Allows(in.Permission, in.EnvID) {
		return nil
	}
	allowedRefs, _, err := AllowedProjectImageRefs(ctx, db, in.EnvID, in.Permission)
	if err != nil {
		return huma.Error500InternalServerError(err.Error())
	}
	if !ImagePermitted(ps, ImageAccessInput{ //exhaustruct:enforce
		AllowedRefs: allowedRefs,
		EnvID:       in.EnvID,
		Permission:  in.Permission,
		ImageID:     in.ImageID,
		RepoTags:    in.RepoTags,
		RepoDigests: in.RepoDigests,
	}) {
		return huma.Error403Forbidden("permission denied: " + in.Permission)
	}
	return nil
}

func RequireGitOpsSyncPermission(ctx context.Context, in GitOpsSyncAccessInput) error {
	ps, _ := PermissionsFromContext(ctx)
	if !GitOpsSyncPermitted(ps, in) {
		return huma.Error403Forbidden("permission denied: " + in.Permission)
	}
	return nil
}
