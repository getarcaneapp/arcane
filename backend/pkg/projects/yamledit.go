package projects

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"go.yaml.in/yaml/v4"
)

// ScalarEdit replaces the source span of one YAML scalar.
type ScalarEdit struct {
	Text       []byte
	Start, End int
}

// NewScalarEdit locates node's single-line scalar text in source so it can be
// replaced without re-encoding, which would drop comments and formatting.
func NewScalarEdit(source []byte, node *yaml.Node, value string) (ScalarEdit, error) {
	const lineBreaks = "\r\n\u0085\u2028\u2029"
	start := len(source) - len(bytes.TrimPrefix(source, []byte("\xEF\xBB\xBF")))
	for line, column := 1, 1; line < node.Line || (line == node.Line && column < node.Column); column++ {
		if start >= len(source) {
			return ScalarEdit{}, errors.New("scalar position is outside the YAML source")
		}
		r, size := utf8.DecodeRune(source[start:])
		start += size
		if r == '\r' && bytes.HasPrefix(source[start:], []byte("\n")) {
			start++
		}
		if strings.ContainsRune(lineBreaks, r) {
			line, column = line+1, 0
		}
	}
	// The shortest same-line span that decodes to the parsed value is the scalar's source text, including tags and escapes.
	found := false
	end := start
	for !found && end < len(source) {
		r, size := utf8.DecodeRune(source[end:])
		if strings.ContainsRune(lineBreaks, r) {
			break
		}
		end += size
		var decoded string
		found = yaml.Unmarshal(source[start:end], &decoded) == nil && decoded == node.Value
	}
	if !found {
		return ScalarEdit{}, fmt.Errorf("scalar on line %d must be a single-line value", node.Line)
	}
	text, err := yaml.Marshal(&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Style: node.Style, Value: value})
	if err != nil || bytes.ContainsAny(bytes.TrimSuffix(text, []byte("\n")), "\r\n") {
		return ScalarEdit{}, fmt.Errorf("%s cannot be written as a single-line scalar", value)
	}
	return ScalarEdit{Start: start, End: end, Text: bytes.TrimSuffix(text, []byte("\n"))}, nil
}

// ApplyScalarEdits splices non-overlapping edits into a copy of source.
func ApplyScalarEdits(source []byte, edits []ScalarEdit) []byte {
	updated := slices.Clone(source)
	// Splice from the end so earlier spans keep their offsets.
	for _, edit := range slices.SortedFunc(slices.Values(edits), func(a, b ScalarEdit) int { return b.Start - a.Start }) {
		updated = slices.Concat(updated[:edit.Start], edit.Text, updated[edit.End:])
	}
	return updated
}
