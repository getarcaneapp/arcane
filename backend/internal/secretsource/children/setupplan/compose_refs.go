package setupplan

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"go.yaml.in/yaml/v4"
)

type (
	// Compose files are edited as text, not re-encoded, so comments, quoting, and
	// layout stay as written. The YAML parser only locates the lines to change.

	// ComposeService describes one service for the reference picker.
	ComposeService struct {
		Name   string `json:"name"`
		Reason string `json:"reason,omitempty"`
		// Available are keys the service already gets: entries of its environment
		// block, and variables it interpolates anywhere, such as ${DB_PASSWORD}
		// in a URL.
		Available []string `json:"available"`
		// Editable is false when the service's environment cannot be edited as
		// text (flow style such as environment: {A: b}); Reason says why.
		Editable bool `json:"editable"`
	}

	// SkippedRef is a key that was not added to a service.
	SkippedRef struct {
		Service string `json:"service"`
		Key     string `json:"key"`
		Reason  string `json:"reason"`
	}

	serviceNodeInternal struct {
		key   *yaml.Node
		value *yaml.Node
		name  string
	}
)

// ComposeServices lists the services of one compose file, in file order.
func ComposeServices(content string) ([]ComposeService, error) {
	services, err := parseServicesInternal(content)
	if err != nil {
		return nil, err
	}
	result := make([]ComposeService, 0, len(services))
	for _, service := range services {
		info := ComposeService{Name: service.name, Available: availableKeysInternal(service.value), Editable: true}
		if reason := uneditableReasonInternal(service); reason != "" {
			info.Editable, info.Reason = false, reason
		}
		result = append(result, info)
	}
	return result, nil
}

// InjectEnvironmentRefs adds KEY: ${KEY} (or - KEY=${KEY} in list form) to the
// environment of each service in assignments, so compose passes the secret
// of the same name to that service. Keys a service already gets are skipped.
// It returns the new content, what it added per service, and what it skipped.
func InjectEnvironmentRefs(content string, assignments map[string][]string) (string, map[string][]string, []SkippedRef, error) {
	services, err := parseServicesInternal(content)
	if err != nil {
		return "", nil, nil, err
	}
	byName := make(map[string]serviceNodeInternal, len(services))
	for _, service := range services {
		byName[service.name] = service
	}

	type insertion struct {
		lines     []string
		afterLine int // 1-based; lines are inserted after it
	}
	insertions := []insertion{}
	added := map[string][]string{}
	skipped := []SkippedRef{}
	lines := strings.Split(content, "\n")

	names := make([]string, 0, len(assignments))
	for name := range assignments {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		service, ok := byName[name]
		if !ok {
			for _, key := range assignments[name] {
				skipped = append(skipped, SkippedRef{Service: name, Key: key, Reason: "no such service"})
			}
			continue
		}
		if reason := uneditableReasonInternal(service); reason != "" {
			for _, key := range assignments[name] {
				skipped = append(skipped, SkippedRef{Service: name, Key: key, Reason: reason})
			}
			continue
		}

		available := availableKeysInternal(service.value)
		keys := []string{}
		for _, key := range assignments[name] {
			key = strings.TrimSpace(key)
			switch {
			case !envKeyPattern.MatchString(key):
				skipped = append(skipped, SkippedRef{Service: name, Key: key, Reason: "not a valid variable name"})
			case slices.Contains(available, key) || slices.Contains(keys, key):
				skipped = append(skipped, SkippedRef{Service: name, Key: key, Reason: "already available to the service"})
			default:
				keys = append(keys, key)
			}
		}
		if len(keys) == 0 {
			continue
		}
		slices.Sort(keys)

		after, entries, err := environmentInsertionInternal(lines, service, keys)
		if err != nil {
			for _, key := range keys {
				skipped = append(skipped, SkippedRef{Service: name, Key: key, Reason: err.Error()})
			}
			continue
		}
		insertions = append(insertions, insertion{afterLine: after, lines: entries})
		added[name] = keys
	}

	// Insert bottom-up so earlier line numbers stay valid.
	sort.Slice(insertions, func(i, j int) bool { return insertions[i].afterLine > insertions[j].afterLine })
	for _, ins := range insertions {
		lines = slices.Insert(lines, ins.afterLine, ins.lines...)
	}
	updated := strings.Join(lines, "\n")

	// The result must still parse and must give each service its keys.
	check, err := ComposeServices(updated)
	if err != nil {
		return "", nil, nil, fmt.Errorf("adding references produced invalid YAML: %w", err)
	}
	for _, service := range check {
		for _, key := range added[service.Name] {
			if !slices.Contains(service.Available, key) {
				return "", nil, nil, fmt.Errorf("could not add %s to %s", key, service.Name)
			}
		}
	}
	return updated, added, skipped, nil
}

func parseServicesInternal(content string) ([]serviceNodeInternal, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(content), &doc); err != nil {
		return nil, fmt.Errorf("parse compose file: %w", err)
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, errors.New("the compose file has no top-level mapping")
	}
	root := doc.Content[0]
	servicesNode := mappingValueInternal(root, "services")
	if servicesNode == nil {
		return nil, errors.New("the compose file has no services")
	}
	if servicesNode.Kind != yaml.MappingNode {
		return nil, errors.New("services must be a mapping")
	}
	services := make([]serviceNodeInternal, 0, len(servicesNode.Content)/2)
	for i := 0; i+1 < len(servicesNode.Content); i += 2 {
		key, value := servicesNode.Content[i], servicesNode.Content[i+1]
		services = append(services, serviceNodeInternal{name: key.Value, key: key, value: value})
	}
	if servicesNode.Style&yaml.FlowStyle != 0 {
		for i := range services {
			services[i].value = &yaml.Node{Kind: yaml.ScalarNode, Style: yaml.FlowStyle}
		}
	}
	return services, nil
}

func mappingValueInternal(mapping *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i+1]
		}
	}
	return nil
}

func mappingKeyInternal(mapping *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i]
		}
	}
	return nil
}

func uneditableReasonInternal(service serviceNodeInternal) string {
	value := service.value
	if value.Kind != yaml.MappingNode || value.Style&yaml.FlowStyle != 0 || len(value.Content) == 0 {
		return "the service is not a block mapping; add the references by hand"
	}
	env := mappingValueInternal(value, "environment")
	if env == nil {
		return ""
	}
	switch {
	case env.Kind == yaml.AliasNode:
		return "environment is a YAML alias; add the references by hand"
	case env.Style&yaml.FlowStyle != 0:
		return "environment is written inline; add the references by hand"
	case env.Kind == yaml.ScalarNode && env.Tag == "!!null" && env.Line == mappingKeyInternal(value, "environment").Line && env.Value != "":
		return "environment is set to null inline; add the references by hand"
	case env.Kind == yaml.ScalarNode && env.Tag != "!!null":
		return "environment is not a mapping or a list"
	}
	return ""
}

// availableKeysInternal lists the environment entries of a service and the
// variables it interpolates anywhere.
func availableKeysInternal(service *yaml.Node) []string {
	set := map[string]struct{}{}
	if service.Kind == yaml.MappingNode {
		if env := mappingValueInternal(service, "environment"); env != nil {
			switch env.Kind {
			case yaml.MappingNode:
				for i := 0; i+1 < len(env.Content); i += 2 {
					set[env.Content[i].Value] = struct{}{}
				}
			case yaml.SequenceNode:
				for _, item := range env.Content {
					name, _, _ := strings.Cut(item.Value, "=")
					set[strings.TrimSpace(name)] = struct{}{}
				}
			}
		}
	}
	var walk func(node *yaml.Node)
	walk = func(node *yaml.Node) {
		if node.Kind == yaml.ScalarNode {
			for _, ref := range scanReferencesInternal(node.Value) {
				set[ref.name] = struct{}{}
			}
		}
		for _, child := range node.Content {
			walk(child)
		}
	}
	walk(service)

	keys := make([]string, 0, len(set))
	for key := range set {
		if key != "" {
			keys = append(keys, key)
		}
	}
	slices.Sort(keys)
	return keys
}

// environmentInsertionInternal returns where to insert the new entries of a
// service and the lines to insert.
func environmentInsertionInternal(lines []string, service serviceNodeInternal, keys []string) (int, []string, error) {
	value := service.value
	serviceIndent := service.key.Column - 1
	childIndent := value.Content[0].Column - 1
	step := childIndent - serviceIndent
	if step <= 0 {
		step = 2
	}

	envKey := mappingKeyInternal(value, "environment")
	env := mappingValueInternal(value, "environment")
	switch {
	case env == nil:
		// No environment yet: add one as the service's first key.
		entries := []string{strings.Repeat(" ", childIndent) + "environment:"}
		for _, key := range keys {
			entries = append(entries, mapEntryInternal(childIndent+step, key))
		}
		return service.key.Line, entries, nil

	case env.Kind == yaml.ScalarNode:
		// "environment:" with nothing after it.
		entries := make([]string, 0, len(keys))
		for _, key := range keys {
			entries = append(entries, mapEntryInternal(childIndent+step, key))
		}
		return blockEndInternal(lines, envKey.Line, envKey.Column-1), entries, nil

	case env.Kind == yaml.MappingNode && len(env.Content) > 0:
		indent := env.Content[0].Column - 1
		entries := make([]string, 0, len(keys))
		for _, key := range keys {
			entries = append(entries, mapEntryInternal(indent, key))
		}
		return blockEndInternal(lines, envKey.Line, envKey.Column-1), entries, nil

	case env.Kind == yaml.SequenceNode && len(env.Content) > 0:
		first := lines[env.Content[0].Line-1]
		indent := len(first) - len(strings.TrimLeft(first, " "))
		entries := make([]string, 0, len(keys))
		for _, key := range keys {
			entries = append(entries, strings.Repeat(" ", indent)+"- "+key+"=${"+key+"}")
		}
		return blockEndInternal(lines, envKey.Line, envKey.Column-1), entries, nil

	default:
		return 0, nil, errors.New("environment has an unexpected shape; add the references by hand")
	}
}

func mapEntryInternal(indent int, key string) string {
	return strings.Repeat(" ", indent) + key + ": ${" + key + "}"
}

// blockEndInternal returns the last line (1-based) of the block that starts
// with a key on keyLine at keyIndent: the last following line indented
// deeper than the key that is not blank or a comment.
func blockEndInternal(lines []string, keyLine, keyIndent int) int {
	end := keyLine
	for i := keyLine; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if len(line)-len(strings.TrimLeft(line, " ")) <= keyIndent {
			break
		}
		end = i + 1
	}
	return end
}
