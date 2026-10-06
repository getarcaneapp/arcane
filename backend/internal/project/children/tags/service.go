// Package tags owns project tag rules: UI and Compose sources, colors,
// per-project limits and the effective tag view.
package tags

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/getarcaneapp/arcane/types/v2/project"
	"go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/projects"
)

// ErrComposeTagReadOnly rejects UI edits of a tag the Compose file owns.
var ErrComposeTagReadOnly = errors.New("tag is defined in Compose and can only be changed through x-arcane metadata")

// Store reads and writes one project's tag assignments inside the caller's
// transaction.
type Store interface {
	CountTag(projectID, name string, source project.TagSource) (int64, error)
	CountSource(projectID string, source project.TagSource) (int64, error)
	StoredColor(name string) (project.TagColor, bool, error)
	Insert(rows []project.TagAssignment) error
	InsertIgnoringConflict(row project.TagAssignment) error
	DeleteTag(projectID, name string, source project.TagSource) error
	DeleteSource(projectID string, source project.TagSource) error
}

// NormalizeUpdate validates a UI tag change; detached tags keep the neutral color.
func NormalizeUpdate(name string, color project.TagColor, attached bool) (string, project.TagColor, error) {
	normalized, err := projects.NormalizeProjectTag(name)
	if err != nil {
		return "", "", err
	}
	if !attached {
		return normalized, project.TagColorGray, nil
	}
	normalizedColor, err := projects.NormalizeProjectTagColor(color)
	if err != nil {
		return "", "", err
	}
	return normalized, normalizedColor, nil
}

// ApplyUpdate attaches or detaches a normalized UI tag. Compose-owned names are
// read-only, attaching is idempotent and UI tags are capped per project.
func ApplyUpdate(store Store, projectID, name string, color project.TagColor, attached bool) error {
	composeCount, err := store.CountTag(projectID, name, project.TagSourceCompose)
	if err != nil {
		return fmt.Errorf("check Compose tag source: %w", err)
	}
	if composeCount > 0 {
		return ErrComposeTagReadOnly
	}

	if !attached {
		if detachErr := store.DeleteTag(projectID, name, project.TagSourceUI); detachErr != nil {
			return fmt.Errorf("detach UI project tag: %w", detachErr)
		}
		return nil
	}

	existing, err := store.CountTag(projectID, name, project.TagSourceUI)
	if err != nil {
		return fmt.Errorf("check UI tag source: %w", err)
	}
	if existing > 0 {
		return nil
	}
	count, err := store.CountSource(projectID, project.TagSourceUI)
	if err != nil {
		return fmt.Errorf("count UI project tags: %w", err)
	}
	if count >= projects.ProjectTagsPerSourceLimit {
		return fmt.Errorf("a project cannot have more than %d UI tags", projects.ProjectTagsPerSourceLimit)
	}
	// One color per tag name across projects; the normalized color only seeds new names.
	storedColor, found, err := store.StoredColor(name)
	if err != nil {
		return fmt.Errorf("resolve project tag color: %w", err)
	}
	resolvedColor := kit.Ternary(found, storedColor, color)
	if attachErr := store.InsertIgnoringConflict(project.TagAssignment{ProjectID: projectID, Name: name, Source: project.TagSourceUI, Color: resolvedColor}); attachErr != nil {
		return fmt.Errorf("attach UI project tag: %w", attachErr)
	}
	return nil
}

// ReplaceCompose replaces a project's Compose-owned tags with normalized ones.
func ReplaceCompose(store Store, projectID string, normalized []project.TagOption) error {
	if err := store.DeleteSource(projectID, project.TagSourceCompose); err != nil {
		return fmt.Errorf("clear Compose project tags: %w", err)
	}
	if len(normalized) == 0 {
		return nil
	}
	rows := make([]project.TagAssignment, 0, len(normalized))
	for _, tag := range normalized {
		rows = append(rows, project.TagAssignment{ProjectID: projectID, Name: tag.Name, Source: project.TagSourceCompose, Color: tag.Color})
	}
	if err := store.Insert(rows); err != nil {
		return fmt.Errorf("replace Compose project tags: %w", err)
	}
	return nil
}

// AttachInitial attaches the UI tags chosen when a project is created.
func AttachInitial(store Store, projectID string, uiTags []string, colors map[string]project.TagColor) error {
	if len(uiTags) == 0 {
		return nil
	}
	rows := make([]project.TagAssignment, 0, len(uiTags))
	for _, tag := range uiTags {
		color, found, err := store.StoredColor(tag)
		if err != nil {
			return fmt.Errorf("resolve project tag color: %w", err)
		}
		if !found {
			if color, err = projects.NormalizeProjectTagColor(colors[tag]); err != nil {
				return err
			}
		}
		rows = append(rows, project.TagAssignment{ProjectID: projectID, Name: tag, Source: project.TagSourceUI, Color: color})
	}
	if err := store.Insert(rows); err != nil {
		return fmt.Errorf("attach initial project tags: %w", err)
	}
	return nil
}

// Group merges stored assignments into effective tags per project, sorted by
// name with the UI source first.
func Group(rows []project.TagAssignment) map[string][]project.Tag {
	result := make(map[string][]project.Tag)
	for _, row := range rows {
		tags := result[row.ProjectID]
		if len(tags) == 0 || tags[len(tags)-1].Name != row.Name {
			tags = append(tags, project.Tag{Name: row.Name, Color: row.Color})
		}
		tags[len(tags)-1].Sources = append(tags[len(tags)-1].Sources, row.Source)
		result[row.ProjectID] = tags
	}
	for projectID := range result {
		sort.SliceStable(result[projectID], func(i, j int) bool { return result[projectID][i].Name < result[projectID][j].Name })
		for index := range result[projectID] {
			sort.SliceStable(result[projectID][index].Sources, func(i, j int) bool {
				return result[projectID][index].Sources[i] == project.TagSourceUI && result[projectID][index].Sources[j] != project.TagSourceUI
			})
		}
	}
	return result
}

func NormalizeProjectTagColors(colors map[string]project.TagColor) (map[string]project.TagColor, error) {
	result := make(map[string]project.TagColor, len(colors))
	for name, color := range colors {
		normalizedName, err := projects.NormalizeProjectTag(name)
		if err != nil {
			return nil, err
		}
		normalizedColor, err := projects.NormalizeProjectTagColor(color)
		if err != nil {
			return nil, err
		}
		result[normalizedName] = normalizedColor
	}
	return result, nil
}

func NormalizeComposeProjectTags(tags []project.TagOption) ([]project.TagOption, error) {
	result := make([]project.TagOption, 0, min(len(tags), projects.ProjectTagsPerSourceLimit))
	seen := make(map[string]struct{}, len(tags))
	for _, tag := range tags {
		name, err := projects.NormalizeProjectTag(tag.Name)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(string(tag.Color)) == "" {
			return nil, errors.New("compose tag color is required")
		}
		color, err := projects.NormalizeProjectTagColor(tag.Color)
		if err != nil {
			return nil, err
		}
		if _, exists := seen[name]; exists {
			continue
		}
		if len(result) >= projects.ProjectTagsPerSourceLimit {
			return nil, fmt.Errorf("a project cannot have more than %d compose tags", projects.ProjectTagsPerSourceLimit)
		}
		seen[name] = struct{}{}
		result = append(result, project.TagOption{Name: name, Color: color})
	}
	return result, nil
}
