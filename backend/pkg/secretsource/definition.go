package secretsource

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"text/template"

	"go.yaml.in/yaml/v4"

	"getarcane.dev/arcane/v3/pkg/projects"
	"getarcane.dev/types/v3"
)

const (
	APIVersion         = "arcane.secretsource/v1"
	KindHTTP           = "http"
	KindContainer      = "container"
	formatJSON         = "json"
	formatDotenv       = "dotenv"
	BaseURLInput       = "baseUrl"
	maxDefinitionBytes = 256 << 10
)

var allowedMethods = []string{http.MethodGet, http.MethodPost, http.MethodPut}

func ParseDefinition(source []byte) (types.SecretSourceDefinition, error) {
	if len(source) > maxDefinitionBytes {
		return types.SecretSourceDefinition{}, fmt.Errorf("definition exceeds %d bytes", maxDefinitionBytes)
	}

	var root yaml.Node
	if err := yaml.Unmarshal(source, &root); err != nil {
		return types.SecretSourceDefinition{}, fmt.Errorf("invalid definition YAML: %w", err)
	}
	if len(root.Content) == 0 || root.Content[0].Kind != yaml.MappingNode {
		return types.SecretSourceDefinition{}, errors.New("definition must be a YAML mapping")
	}

	document := root.Content[0]
	for i := 0; i+1 < len(document.Content); i += 2 {
		if name := document.Content[i].Value; name == "inputs" || name == "credentials" {
			fields, err := fieldListNode(name, document.Content[i+1])
			if err != nil {
				return types.SecretSourceDefinition{}, err
			}
			document.Content[i+1] = fields
		}
	}

	var definition types.SecretSourceDefinition
	if err := document.Load(&definition, yaml.WithKnownFields()); err != nil {
		return types.SecretSourceDefinition{}, fmt.Errorf("invalid definition: %w", err)
	}
	if err := validateDefinition(definition); err != nil {
		return types.SecretSourceDefinition{}, err
	}
	return definition, nil
}

func fieldListNode(name string, node *yaml.Node) (*yaml.Node, error) {
	switch {
	case node.Kind == yaml.SequenceNode:
		return node, nil
	case node.Kind == yaml.ScalarNode && node.Tag == "!!null":
		return &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}, nil
	case node.Kind != yaml.MappingNode:
		return nil, fmt.Errorf("%s must be a mapping of field keys", name)
	}

	items := make([]*yaml.Node, 0, len(node.Content)/2)
	for i := 0; i+1 < len(node.Content); i += 2 {
		key, value := node.Content[i], node.Content[i+1]
		content := []*yaml.Node{
			{Kind: yaml.ScalarNode, Tag: "!!str", Value: "key"},
			{Kind: yaml.ScalarNode, Tag: "!!str", Value: key.Value},
		}
		switch {
		case value.Kind == yaml.MappingNode:
			content = append(content, value.Content...)
		case value.Kind == yaml.ScalarNode && value.Tag != "!!null":
			content = append(content,
				&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "label"},
				&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value.Value},
			)
		case value.Kind != yaml.ScalarNode:
			return nil, fmt.Errorf("%s.%s must be a mapping", name, key.Value)
		}
		items = append(items, &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: content})
	}
	return &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Content: items}, nil
}

func validateDefinition(definition types.SecretSourceDefinition) error {
	var problems []error
	if definition.APIVersion != APIVersion {
		problems = append(problems, fmt.Errorf("apiVersion must be %q", APIVersion))
	}
	if strings.TrimSpace(definition.ID) == "" {
		problems = append(problems, errors.New("id is required"))
	}
	if strings.TrimSpace(definition.Name) == "" {
		problems = append(problems, errors.New("name is required"))
	}

	problems = append(problems, validateFields("inputs", definition.Inputs)...)
	problems = append(problems, validateFields("credentials", definition.Credentials)...)
	for _, input := range definition.Inputs {
		if slices.ContainsFunc(definition.Credentials, func(credential types.SecretSourceField) bool { return credential.Key == input.Key }) {
			problems = append(problems, fmt.Errorf("%q is declared as both an input and a credential", input.Key))
		}
	}

	switch definition.Kind {
	case KindHTTP:
		problems = append(problems, validateHTTPDefinition(definition)...)
	case KindContainer:
		problems = append(problems, validateContainerDefinition(definition)...)
	default:
		problems = append(problems, fmt.Errorf("kind must be %q or %q", KindHTTP, KindContainer))
	}

	for i, rule := range definition.Extract {
		if strings.TrimSpace(rule.From) == "" {
			problems = append(problems, fmt.Errorf("extract[%d].from is required", i))
		}
		if rule.Object == (rule.List != nil) {
			problems = append(problems, fmt.Errorf("extract[%d] needs exactly one of object or list", i))
		}
		if rule.List != nil && (strings.TrimSpace(rule.List.Key) == "" || strings.TrimSpace(rule.List.Value) == "") {
			problems = append(problems, fmt.Errorf("extract[%d].list needs key and value paths", i))
		}
	}

	for _, text := range definitionTemplates(definition) {
		if _, err := template.New("definition").Parse(text); err != nil {
			problems = append(problems, fmt.Errorf("invalid template %q: %w", text, err))
		}
	}

	return errors.Join(problems...)
}

func validateFields(section string, fields []types.SecretSourceField) []error {
	var problems []error
	seen := make(map[string]bool, len(fields))
	for _, field := range fields {
		switch {
		case !projects.EnvKeyPattern.MatchString(field.Key):
			problems = append(problems, fmt.Errorf("%s key %q must use letters, numbers, and underscores", section, field.Key))
		case seen[field.Key]:
			problems = append(problems, fmt.Errorf("%s key %q is declared twice", section, field.Key))
		}
		seen[field.Key] = true
	}
	return problems
}

func validateHTTPDefinition(definition types.SecretSourceDefinition) []error {
	var problems []error
	if !slices.ContainsFunc(definition.Inputs, func(field types.SecretSourceField) bool { return field.Key == BaseURLInput }) {
		problems = append(problems, fmt.Errorf("http definitions need a %q input", BaseURLInput))
	}
	if definition.Fetch == nil {
		problems = append(problems, errors.New("http definitions need a fetch step"))
	}
	if definition.Image != "" || len(definition.Env) > 0 || len(definition.Args) > 0 || definition.Format != "" {
		problems = append(problems, errors.New("image, env, args, and format are only valid for the container kind"))
	}
	for _, step := range []struct {
		step *types.SecretSourceHTTPStep
		name string
	}{
		{name: "login", step: definition.Login},
		{name: "fetch", step: definition.Fetch},
	} {
		if step.step == nil {
			continue
		}
		if !strings.HasPrefix(step.step.Path, "{{") && (!strings.HasPrefix(step.step.Path, "/") || strings.HasPrefix(step.step.Path, "//")) {
			problems = append(problems, fmt.Errorf("%s.path must start with a single /", step.name))
		}
		if method := strings.ToUpper(step.step.Method); method != "" && !slices.Contains(allowedMethods, method) {
			problems = append(problems, fmt.Errorf("%s.method must be one of %s", step.name, strings.Join(allowedMethods, ", ")))
		}
		if step.step.JSON != nil && step.step.Form != nil {
			problems = append(problems, fmt.Errorf("%s cannot have both json and form bodies", step.name))
		}
	}
	if definition.Fetch != nil && len(definition.Fetch.Capture) > 0 {
		problems = append(problems, errors.New("capture is only valid on the login step"))
	}
	if definition.Login != nil && len(definition.Login.Capture) == 0 {
		problems = append(problems, errors.New("login needs at least one capture"))
	}
	return problems
}

func validateContainerDefinition(definition types.SecretSourceDefinition) []error {
	var problems []error
	if strings.TrimSpace(definition.Image) == "" {
		problems = append(problems, errors.New("container definitions need an image"))
	}
	if definition.Login != nil || definition.Fetch != nil {
		problems = append(problems, errors.New("login and fetch are only valid for the http kind"))
	}
	return problems
}

func definitionTemplates(definition types.SecretSourceDefinition) []string {
	texts := []string{definition.Image, definition.Format}
	texts = append(texts, definition.Args...)
	for _, value := range definition.Env {
		texts = append(texts, value)
	}
	for _, step := range []*types.SecretSourceHTTPStep{definition.Login, definition.Fetch} {
		if step == nil {
			continue
		}
		texts = append(texts, step.Path)
		for _, values := range []map[string]string{step.Query, step.Headers, step.Form} {
			for _, value := range values {
				texts = append(texts, value)
			}
		}
		texts = append(texts, templateStrings(step.JSON)...)
	}
	return texts
}

func templateStrings(value any) []string {
	var texts []string
	switch typed := value.(type) {
	case string:
		texts = append(texts, typed)
	case map[string]any:
		for _, item := range typed {
			texts = append(texts, templateStrings(item)...)
		}
	case []any:
		for _, item := range typed {
			texts = append(texts, templateStrings(item)...)
		}
	}
	return texts
}
