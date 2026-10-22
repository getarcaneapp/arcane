package authz

import (
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/getarcaneapp/arcane/types/v3"
	"go.getarcane.app/docker"
	"go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/backend/v3/internal/common"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/libarcane"
)

var ErrInvalidSelector = common.Classify(common.ErrValidation, errors.New("invalid resource selector"))

type ResourceAttributes struct {
	Names   []string
	Labels  map[string]string
	Project string
	OwnerID string
}

type ResourceScope struct {
	unrestricted bool
	subjectID    string
	selectors    []types.ResourceSelector
}

func UnrestrictedScope() ResourceScope {
	return ResourceScope{unrestricted: true}
}

func (s ResourceScope) IsUnrestricted() bool {
	return s.unrestricted
}

func (s ResourceScope) Matches(a ResourceAttributes) bool {
	if s.unrestricted {
		return true
	}
	return slices.ContainsFunc(s.selectors, func(sel types.ResourceSelector) bool {
		return s.selectorMatches(sel, a)
	})
}

func (s ResourceScope) selectorMatches(sel types.ResourceSelector, a ResourceAttributes) bool {
	if len(selectorKinds(sel)) == 0 {
		return false
	}
	for key, value := range sel.Labels {
		if got, ok := a.Labels[key]; !ok || got != value {
			return false
		}
	}
	if len(sel.Projects) > 0 && (a.Project == "" || !slices.Contains(sel.Projects, a.Project)) {
		return false
	}
	if sel.OwnerOnly && (s.subjectID == "" || a.OwnerID != s.subjectID) {
		return false
	}
	return len(sel.Names) == 0 || slices.ContainsFunc(a.Names, func(name string) bool {
		return slices.ContainsFunc(sel.Names, func(glob string) bool {
			matched, err := path.Match(glob, name)
			return err == nil && matched
		})
	})
}

func (s ResourceScope) Covers(sel *types.ResourceSelector) bool {
	if s.unrestricted {
		return true
	}
	if sel == nil {
		return false
	}
	return slices.ContainsFunc(s.selectors, func(broad types.ResourceSelector) bool {
		return SelectorIsSubset(*sel, broad)
	})
}

func (s ResourceScope) CoversScope(other ResourceScope) bool {
	if s.unrestricted {
		return true
	}
	if other.unrestricted {
		return false
	}
	return !slices.ContainsFunc(other.selectors, func(sel types.ResourceSelector) bool {
		return !s.Covers(&sel)
	})
}

func ContainerAttributes(names []string, labels map[string]string) ResourceAttributes {
	trimmed := make([]string, len(names))
	for i, name := range names {
		trimmed[i] = strings.TrimPrefix(name, "/")
	}
	return ResourceAttributes{
		Names:   trimmed,
		Labels:  labels,
		Project: docker.ComposeProjectLabel(labels),
		OwnerID: labels[libarcane.LabelOwner],
	}
}

func VolumeAttributes(name string, labels map[string]string) ResourceAttributes {
	return ResourceAttributes{
		Names:   []string{name},
		Labels:  labels,
		Project: docker.ComposeProjectLabel(labels),
		OwnerID: labels[libarcane.LabelOwner],
	}
}

func NetworkAttributes(name string, labels map[string]string) ResourceAttributes {
	return ResourceAttributes{
		Names:   []string{name},
		Labels:  labels,
		Project: docker.ComposeProjectLabel(labels),
		OwnerID: labels[libarcane.LabelOwner],
	}
}

func ImageAttributes(repoTags []string, labels map[string]string) ResourceAttributes {
	return ResourceAttributes{
		Names: slices.DeleteFunc(slices.Clone(repoTags), func(tag string) bool {
			return tag == "" || tag == "<none>:<none>"
		}),
		Labels: labels,
	}
}

func ProjectAttributes(name string, ownerID *string) ResourceAttributes {
	return ResourceAttributes{
		Names:   []string{name},
		Project: name,
		OwnerID: kit.FromPtr(ownerID),
	}
}

func ValidateSelector(sel types.ResourceSelector) error {
	if len(selectorKinds(sel)) == 0 {
		return fmt.Errorf("%w: set at least one of labels, projects, ownerOnly, or names", ErrInvalidSelector)
	}
	for key := range sel.Labels {
		if strings.TrimSpace(key) == "" {
			return fmt.Errorf("%w: label keys must not be empty", ErrInvalidSelector)
		}
	}
	if slices.ContainsFunc(sel.Projects, func(project string) bool {
		return strings.TrimSpace(project) == ""
	}) {
		return fmt.Errorf("%w: project names must not be empty", ErrInvalidSelector)
	}
	for _, glob := range sel.Names {
		if strings.TrimSpace(glob) == "" {
			return fmt.Errorf("%w: name globs must not be empty", ErrInvalidSelector)
		}
		if _, err := path.Match(glob, ""); err != nil {
			return fmt.Errorf("%w: invalid name glob %q", ErrInvalidSelector, glob)
		}
	}
	return nil
}

func selectorKinds(sel types.ResourceSelector) []types.SelectorKind {
	var kinds []types.SelectorKind
	if len(sel.Labels) > 0 {
		kinds = append(kinds, types.SelectorKindLabels)
	}
	if len(sel.Projects) > 0 {
		kinds = append(kinds, types.SelectorKindProjects)
	}
	if sel.OwnerOnly {
		kinds = append(kinds, types.SelectorKindOwner)
	}
	if len(sel.Names) > 0 {
		kinds = append(kinds, types.SelectorKindNames)
	}
	return kinds
}
