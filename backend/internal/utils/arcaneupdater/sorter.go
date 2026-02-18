package arcaneupdater

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

// ContainerWithDeps represents a container with its dependency information for sorting
type ContainerWithDeps struct {
	Container   container.Summary
	Inspect     container.InspectResponse
	Name        string
	Links       []string // Container names this one links to
	DependsOn   []string // Explicit dependencies from label
	NetworkDeps []string // Implicit dependencies from container network mode
}

// ContainerSorter handles topological sorting of containers based on dependencies
type ContainerSorter struct {
	containers  []ContainerWithDeps
	nameToIndex map[string]int
	visited     map[string]bool
	marked      map[string]bool // For cycle detection
	sorted      []ContainerWithDeps
}

// NewContainerSorter creates a new sorter for the given containers
func NewContainerSorter(containers []ContainerWithDeps) *ContainerSorter {
	nameToIndex := make(map[string]int, len(containers))
	for i, c := range containers {
		nameToIndex[c.Name] = i
	}
	return &ContainerSorter{
		containers:  containers,
		nameToIndex: nameToIndex,
		visited:     make(map[string]bool),
		marked:      make(map[string]bool),
		sorted:      make([]ContainerWithDeps, 0, len(containers)),
	}
}

// Sort performs topological sort and returns containers in dependency order
// Dependencies come first, dependents come later
func (s *ContainerSorter) Sort() ([]ContainerWithDeps, error) {
	for _, c := range s.containers {
		if !s.visited[c.Name] {
			if err := s.visit(c); err != nil {
				return nil, err
			}
		}
	}
	return s.sorted, nil
}

// SortReverse returns containers in reverse dependency order
// Dependents come first, dependencies come later (for stopping)
func (s *ContainerSorter) SortReverse() ([]ContainerWithDeps, error) {
	sorted, err := s.Sort()
	if err != nil {
		return nil, err
	}

	slices.Reverse(sorted)
	return sorted, nil
}

func (s *ContainerSorter) visit(c ContainerWithDeps) error {
	if s.marked[c.Name] {
		return fmt.Errorf("circular dependency detected: %s", c.Name)
	}
	if s.visited[c.Name] {
		return nil
	}

	s.marked[c.Name] = true
	defer delete(s.marked, c.Name)

	// Visit all dependencies first
	allDeps := s.getAllDependencies(c)
	for _, depName := range allDeps {
		if idx, ok := s.nameToIndex[depName]; ok {
			if err := s.visit(s.containers[idx]); err != nil {
				return err
			}
		}
	}

	s.visited[c.Name] = true
	s.sorted = append(s.sorted, c)
	return nil
}

func (s *ContainerSorter) getAllDependencies(c ContainerWithDeps) []string {
	seen := make(map[string]struct{})
	var deps []string

	for _, d := range c.Links {
		if _, ok := seen[d]; !ok {
			seen[d] = struct{}{}
			deps = append(deps, d)
		}
	}
	for _, d := range c.DependsOn {
		if _, ok := seen[d]; !ok {
			seen[d] = struct{}{}
			deps = append(deps, d)
		}
	}
	for _, d := range c.NetworkDeps {
		if _, ok := seen[d]; !ok {
			seen[d] = struct{}{}
			deps = append(deps, d)
		}
	}

	return deps
}

// ExtractContainerDeps extracts dependency information from a container
func ExtractContainerDeps(ctx context.Context, dcli *client.Client, cnt container.Summary, inspect container.InspectResponse) ContainerWithDeps {
	c := ContainerWithDeps{
		Container: cnt,
		Inspect:   inspect,
		Name:      ExtractContainerName(cnt),
	}

	// Extract Docker links
	if inspect.HostConfig != nil {
		for _, link := range inspect.HostConfig.Links {
			// Links are in format "container:/alias"
			parts := strings.SplitN(link, ":", 2)
			if len(parts) > 0 {
				linkName := strings.TrimPrefix(parts[0], "/")
				c.Links = append(c.Links, linkName)
			}
		}
	}

	// Extract explicit depends-on from label
	if inspect.Config != nil && inspect.Config.Labels != nil {
		if deps, ok := inspect.Config.Labels[LabelDependsOn]; ok {
			for dep := range strings.SplitSeq(deps, ",") {
				dep = strings.TrimSpace(dep)
				if dep != "" {
					c.DependsOn = append(c.DependsOn, dep)
				}
			}
		}
	}

	// Extract implicit dependencies from network mode (container:xxx)
	if inspect.HostConfig != nil {
		nm := inspect.HostConfig.NetworkMode
		if nm.IsContainer() {
			// NetworkMode is "container:<name_or_id>"
			containerRef := strings.TrimPrefix(string(nm), "container:")
			c.NetworkDeps = append(c.NetworkDeps, containerRef)
		}
	}

	slog.DebugContext(ctx, "ExtractContainerDeps: extracted dependencies",
		"container", c.Name,
		"links", c.Links,
		"dependsOn", c.DependsOn,
		"networkDeps", c.NetworkDeps)

	return c
}

// UpdateImplicitRestart marks containers that need to restart because their dependencies are restarting.
// Returns the names of containers that were marked for implicit restart.
// Note: This function mutates the containers slice by adding "_arcane_implicit_restart" labels.
func UpdateImplicitRestart(containers []ContainerWithDeps, markedForRestart map[string]bool) []string {
	var implicitRestarts []string

	for i, c := range containers {
		if markedForRestart[c.Name] {
			continue // Already marked
		}

		// Check if any dependency is marked for restart
		allDeps := make([]string, 0, len(c.Links)+len(c.DependsOn)+len(c.NetworkDeps))
		allDeps = append(allDeps, c.Links...)
		allDeps = append(allDeps, c.DependsOn...)
		allDeps = append(allDeps, c.NetworkDeps...)

		for _, dep := range allDeps {
			if markedForRestart[dep] {
				// This container's dependency is restarting, so it needs to restart too
				markedForRestart[c.Name] = true
				if containers[i].Container.Labels == nil {
					containers[i].Container.Labels = map[string]string{}
				}
				containers[i].Container.Labels["_arcane_implicit_restart"] = "true"
				implicitRestarts = append(implicitRestarts, c.Name)
				break
			}
		}
	}

	return implicitRestarts
}

// ExtractContainerName extracts a clean container name from the summary
func ExtractContainerName(cnt container.Summary) string {
	if len(cnt.Names) > 0 {
		n := cnt.Names[0]
		if strings.HasPrefix(n, "/") {
			return n[1:]
		}
		return n
	}
	return cnt.ID[:12]
}
