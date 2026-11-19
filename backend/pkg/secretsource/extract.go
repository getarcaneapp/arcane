package secretsource

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"strconv"
	"strings"

	"getarcane.dev/types/v3"
)

type (
	jsonMember struct {
		name  string
		value jsontext.Value
	}
)

var rootObjectRules = []types.SecretSourceExtract{{From: ".", Object: true}}

func extract(document jsontext.Value, rules []types.SecretSourceExtract) ([]types.EnvVariable, error) {
	if len(rules) == 0 {
		rules = rootObjectRules
	}

	var entries []types.EnvVariable
	positions := make(map[string]int)
	matched := false
	for i, rule := range rules {
		nodes, err := lookup(document, rule.From)
		if err != nil {
			return nil, fmt.Errorf("extract[%d]: %w", i, err)
		}
		matched = matched || len(nodes) > 0
		for _, node := range nodes {
			pairs, pairsErr := extractPairs(node, rule)
			if pairsErr != nil {
				return nil, fmt.Errorf("extract[%d] (%s): %w", i, rule.From, pairsErr)
			}
			for _, pair := range pairs {
				if index, ok := positions[pair.Key]; ok {
					entries[index] = pair
					continue
				}
				positions[pair.Key] = len(entries)
				entries = append(entries, pair)
			}
		}
	}
	if !matched {
		paths := make([]string, 0, len(rules))
		for _, rule := range rules {
			paths = append(paths, rule.From)
		}
		return nil, fmt.Errorf("no values found at %s", strings.Join(paths, ", "))
	}
	return entries, nil
}

func extractPairs(node jsontext.Value, rule types.SecretSourceExtract) ([]types.EnvVariable, error) {
	if rule.List == nil {
		if node.Kind() != '{' {
			return nil, fmt.Errorf("expected an object, found %s", kindName(node.Kind()))
		}
		members, err := readObject(node)
		if err != nil {
			return nil, err
		}
		pairs := make([]types.EnvVariable, 0, len(members))
		for _, member := range members {
			if value, ok := scalarText(member.value); ok {
				pairs = append(pairs, types.EnvVariable{Key: member.name, Value: value})
			}
		}
		return pairs, nil
	}

	if node.Kind() != '[' {
		return nil, fmt.Errorf("expected a list, found %s", kindName(node.Kind()))
	}
	items, err := readArray(node)
	if err != nil {
		return nil, err
	}
	pairs := make([]types.EnvVariable, 0, len(items))
	for _, item := range items {
		key, keyOK, keyErr := firstScalar(item, rule.List.Key)
		if keyErr != nil {
			return nil, keyErr
		}
		value, valueOK, valueErr := firstScalar(item, rule.List.Value)
		if valueErr != nil {
			return nil, valueErr
		}
		if keyOK && valueOK && key != "" {
			pairs = append(pairs, types.EnvVariable{Key: key, Value: value})
		}
	}
	return pairs, nil
}

func firstScalar(document jsontext.Value, path string) (string, bool, error) {
	nodes, err := lookup(document, path)
	if err != nil || len(nodes) == 0 {
		return "", false, err
	}
	value, ok := scalarText(nodes[0])
	return value, ok, nil
}

func lookup(document jsontext.Value, path string) ([]jsontext.Value, error) {
	nodes := []jsontext.Value{document}
	trimmed := strings.Trim(strings.TrimSpace(path), ".")
	if trimmed == "" {
		return nodes, nil
	}

	for segment := range strings.SplitSeq(trimmed, ".") {
		var next []jsontext.Value
		for _, node := range nodes {
			switch node.Kind() {
			case '{':
				members, err := readObject(node)
				if err != nil {
					return nil, err
				}
				for _, member := range members {
					if segment == "*" || member.name == segment {
						next = append(next, member.value)
					}
				}
			case '[':
				items, err := readArray(node)
				if err != nil {
					return nil, err
				}
				if segment == "*" {
					next = append(next, items...)
				} else if index, convErr := strconv.Atoi(segment); convErr == nil && index >= 0 && index < len(items) {
					next = append(next, items[index])
				}
			}
		}
		nodes = next
	}
	return nodes, nil
}

func readObject(value jsontext.Value) ([]jsonMember, error) {
	decoder := jsontext.NewDecoder(bytes.NewReader(value))
	if _, err := decoder.ReadToken(); err != nil {
		return nil, fmt.Errorf("invalid JSON object: %w", err)
	}
	var members []jsonMember
	for decoder.PeekKind() != '}' {
		token, err := decoder.ReadToken()
		if err != nil {
			return nil, fmt.Errorf("invalid JSON object: %w", err)
		}
		name := token.String()
		member, err := decoder.ReadValue()
		if err != nil {
			return nil, fmt.Errorf("invalid JSON object: %w", err)
		}
		members = append(members, jsonMember{name: name, value: member.Clone()})
	}
	return members, nil
}

func readArray(value jsontext.Value) ([]jsontext.Value, error) {
	decoder := jsontext.NewDecoder(bytes.NewReader(value))
	if _, err := decoder.ReadToken(); err != nil {
		return nil, fmt.Errorf("invalid JSON list: %w", err)
	}
	var items []jsontext.Value
	for decoder.PeekKind() != ']' {
		item, err := decoder.ReadValue()
		if err != nil {
			return nil, fmt.Errorf("invalid JSON list: %w", err)
		}
		items = append(items, item.Clone())
	}
	return items, nil
}

func scalarText(value jsontext.Value) (string, bool) {
	switch value.Kind() {
	case '"':
		var text string
		if err := json.Unmarshal(value, &text); err != nil {
			return "", false
		}
		return text, true
	case '0', 't', 'f':
		return strings.TrimSpace(string(value)), true
	default:
		return "", false
	}
}

func kindName(kind jsontext.Kind) string {
	switch kind {
	case '{':
		return "an object"
	case '[':
		return "a list"
	case 'n':
		return "null"
	default:
		return "a scalar"
	}
}
