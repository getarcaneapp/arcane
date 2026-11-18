package secretsource

import (
	"cmp"
	"errors"
	"fmt"
	"strings"
	"text/template"

	"getarcane.dev/types/v3"
)

func render(name, text string, data map[string]any) (string, error) {
	tmpl, err := template.New(name).Option("missingkey=error").Parse(text)
	if err != nil {
		return "", fmt.Errorf("invalid %s template: %w", name, err)
	}
	var out strings.Builder
	if execErr := tmpl.Execute(&out, data); execErr != nil {
		return "", fmt.Errorf("failed to render %s: %w", name, execErr)
	}
	return out.String(), nil
}

func renderMap(name string, values map[string]string, data map[string]any) (map[string]string, error) {
	rendered := make(map[string]string, len(values))
	for key, text := range values {
		value, err := render(name+"."+key, text, data)
		if err != nil {
			return nil, err
		}
		rendered[key] = value
	}
	return rendered, nil
}

func renderValue(name string, value any, data map[string]any) (any, error) {
	switch typed := value.(type) {
	case string:
		return render(name, typed, data)
	case map[string]any:
		rendered := make(map[string]any, len(typed))
		for key, item := range typed {
			renderedItem, err := renderValue(name+"."+key, item, data)
			if err != nil {
				return nil, err
			}
			rendered[key] = renderedItem
		}
		return rendered, nil
	case []any:
		rendered := make([]any, 0, len(typed))
		for i, item := range typed {
			renderedItem, err := renderValue(fmt.Sprintf("%s[%d]", name, i), item, data)
			if err != nil {
				return nil, err
			}
			rendered = append(rendered, renderedItem)
		}
		return rendered, nil
	default:
		return value, nil
	}
}

func resolveFields(section string, fields []types.SecretSourceField, values map[string]string) (map[string]string, error) {
	resolved := make(map[string]string, len(fields))
	for _, field := range fields {
		value := cmp.Or(values[field.Key], field.Default)
		if field.Required && strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("%s %q is required", section, cmp.Or(field.Label, field.Key))
		}
		resolved[field.Key] = value
	}
	return resolved, nil
}

func templateData(inputs, credentials map[string]string, path string, login map[string]string) map[string]any {
	return map[string]any{
		"inputs":      inputs,
		"credentials": credentials,
		"path":        path,
		"login":       login,
	}
}

func ValidateValues(req Request) error {
	if req.Definition.PathRequired && strings.TrimSpace(req.Path) == "" {
		return &types.FieldError{Field: "path", Err: errors.New("path is required")}
	}
	if _, err := resolveFields("input", req.Definition.Inputs, req.Inputs); err != nil {
		return err
	}
	_, err := resolveFields("credential", req.Definition.Credentials, req.Credentials)
	return err
}
