// Package tags owns project tag rules: UI and Compose sources, colors,
// per-project limits and the effective tag view.
package tags

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/getarcaneapp/arcane/types/v3"
	kit "go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/backend/v3/pkg/projects"
)

// ErrComposeTagReadOnly rejects UI edits of a tag the Compose file owns.
var ErrComposeTagReadOnly = errors.New("tag is defined in Compose and can only be changed through x-arcane metadata")

// Store reads and writes one project's tag assignments inside the caller's
// transaction.
type Store interface {
	CountTag(projectID, name string, source types.ProjectTagSource) (int64, error)
	CountSource(projectID string, source types.ProjectTagSource) (int64, error)
	StoredColor(name string) (types.ProjectTagColor, bool, error)
	Insert(rows []types.ProjectTagAssignment) error
	InsertIgnoringConflict(row types.ProjectTagAssignment) error
	DeleteTag(projectID, name string, source types.ProjectTagSource) error
	DeleteSource(projectID string, source types.ProjectTagSource) error
}

// NormalizeUpdate validates a UI tag change; detached tags keep the neutral color.
func NormalizeUpdate(name string, color types.ProjectTagColor, attached bool) (string, types.ProjectTagColor, error) {
	normalized, err := projects.NormalizeProjectTag(name)
	if err != nil {
		return "", "", err
	}
	if !attached {
		return normalized, types.ProjectTagColorGray, nil
	}
	normalizedColor, err := projects.NormalizeProjectTagColor(color)
	if err != nil {
		return "", "", err
	}
	return normalized, normalizedColor, nil
}

// ApplyUpdate attaches or detaches a normalized UI tag. Compose-owned names are
// read-only, attaching is idempotent and UI tags are capped per project.
func ApplyUpdate(store Store, projectID, name string, color types.ProjectTagColor, attached bool) error {
	composeCount, err := store.CountTag(projectID, name, types.ProjectTagSourceCompose)
	if err != nil {
		return fmt.Errorf("check Compose tag source: %w", err)
	}
	if composeCount > 0 {
		return ErrComposeTagReadOnly
	}

	if !attached {
		if detachErr := store.DeleteTag(projectID, name, types.ProjectTagSourceUI); detachErr != nil {
			return fmt.Errorf("detach UI project tag: %w", detachErr)
		}
		return nil
	}

	existing, err := store.CountTag(projectID, name, types.ProjectTagSourceUI)
	if err != nil {
		return fmt.Errorf("check UI tag source: %w", err)
	}
	if existing > 0 {
		return nil
	}
	count, err := store.CountSource(projectID, types.ProjectTagSourceUI)
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
	if attachErr := store.InsertIgnoringConflict(types.ProjectTagAssignment{
		ProjectID: projectID,
		Name:      name,
		Source:    types.ProjectTagSourceUI,
		Color:     kit.Ternary(found, storedColor, color),
	}); attachErr != nil {
		return fmt.Errorf("attach UI project tag: %w", attachErr)
	}
	return nil
}

// ReplaceCompose replaces a project's Compose-owned tags with normalized ones.
func ReplaceCompose(store Store, projectID string, normalized []types.ProjectTagOption) error {
	if err := store.DeleteSource(projectID, types.ProjectTagSourceCompose); err != nil {
		return fmt.Errorf("clear Compose project tags: %w", err)
	}
	if len(normalized) == 0 {
		return nil
	}
	rows := make([]types.ProjectTagAssignment, 0, len(normalized))
	for _, tag := range normalized {
		rows = append(rows, types.ProjectTagAssignment{
			ProjectID: projectID,
			Name:      tag.Name,
			Source:    types.ProjectTagSourceCompose,
			Color:     tag.Color,
		})
	}
	if err := store.Insert(rows); err != nil {
		return fmt.Errorf("replace Compose project tags: %w", err)
	}
	return nil
}

// AttachInitial attaches the UI tags chosen when a project is created.
func AttachInitial(store Store, projectID string, uiTags []string, colors map[string]types.ProjectTagColor) error {
	if len(uiTags) == 0 {
		return nil
	}
	rows := make([]types.ProjectTagAssignment, 0, len(uiTags))
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
		rows = append(rows, types.ProjectTagAssignment{
			ProjectID: projectID,
			Name:      tag,
			Source:    types.ProjectTagSourceUI,
			Color:     color,
		})
	}
	if err := store.Insert(rows); err != nil {
		return fmt.Errorf("attach initial project tags: %w", err)
	}
	return nil
}

// Group merges stored assignments into effective tags per project, sorted by
// name with the UI source first.
func Group(rows []types.ProjectTagAssignment) map[string][]types.ProjectTag {
	result := make(map[string][]types.ProjectTag)
	for _, row := range rows {
		tags := result[row.ProjectID]
		if len(tags) == 0 || tags[len(tags)-1].Name != row.Name {
			result[row.ProjectID] = append(tags, types.ProjectTag{
				Name:    row.Name,
				Color:   row.Color,
				Sources: []types.ProjectTagSource{row.Source},
			})
			continue
		}
		tags[len(tags)-1] = types.ProjectTag{ //exhaustruct:enforce
			Name:    tags[len(tags)-1].Name,
			Color:   tags[len(tags)-1].Color,
			Sources: append(tags[len(tags)-1].Sources, row.Source),
		}
	}
	for projectID := range result {
		slices.SortStableFunc(result[projectID], func(a, b types.ProjectTag) int {
			return strings.Compare(a.Name, b.Name)
		})
		for index := range result[projectID] {
			slices.SortStableFunc(result[projectID][index].Sources, func(a, b types.ProjectTagSource) int {
				return cmp.Compare(kit.Ternary(a == types.ProjectTagSourceUI, 0, 1), kit.Ternary(b == types.ProjectTagSourceUI, 0, 1))
			})
		}
	}
	return result
}

func NormalizeProjectTagColors(colors map[string]types.ProjectTagColor) (map[string]types.ProjectTagColor, error) {
	result := make(map[string]types.ProjectTagColor, len(colors))
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

func NormalizeComposeProjectTags(tags []types.ProjectTagOption) ([]types.ProjectTagOption, error) {
	result := make([]types.ProjectTagOption, 0, min(len(tags), projects.ProjectTagsPerSourceLimit))
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
		result = append(result, types.ProjectTagOption{Name: name, Color: color})
	}
	return result, nil
}
