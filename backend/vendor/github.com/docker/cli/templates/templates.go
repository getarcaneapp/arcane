// FIXME(thaJeztah): remove once we are a module; the go:build directive prevents go from downgrading language version to go1.16:
//go:build go1.23

package templates

import (
	"bytes"
	"encoding/json"
	"strings"
	"text/template"
)

// basicFunctions are the set of initial
// functions provided to every template.
var basicFunctions = template.FuncMap{
	"json": func(v any) string {
		buf := &bytes.Buffer{}
		enc := json.NewEncoder(buf)
		enc.SetEscapeHTML(false)
		err := enc.Encode(v)
		if err != nil {
			panic(err)
		}

		// Remove the trailing new line added by the encoder
		return strings.TrimSpace(buf.String())
	},
	"split":    strings.Split,
	"join":     strings.Join,
	"title":    strings.Title, //nolint:nolintlint,staticcheck // strings.Title is deprecated, but we only use it for ASCII, so replacing with golang.org/x/text is out of scope
	"lower":    strings.ToLower,
	"upper":    strings.ToUpper,
	"pad":      padWithSpace,
	"truncate": truncateWithLength,
}

// HeaderFunctions are used to created headers of a table.
// This is a replacement of basicFunctions for header generation
// because we want the header to remain intact.
// Some functions like `pad` are not overridden (to preserve alignment
// with the columns).
var HeaderFunctions = template.FuncMap{
	"json": func(v string) string {
		return v
	},
	"split": func(v string, _ string) string {
		// we want the table header to show the name of the column, and not
		// split the table header itself. Using a different signature
		// here, and return a string instead of []string
		return v
	},
	"join": func(v string, _ string) string {
		// table headers are always a string, so use a different signature
		// for the "join" function (string instead of []string)
		return v
	},
	"title": func(v string) string {
		return v
	},
	"lower": func(v string) string {
		return v
	},
	"upper": func(v string) string {
		return v
	},
	"truncate": func(v string, _ int) string {
		return v
	},
}

// Parse creates a new anonymous template with the basic functions
// and parses the given format.
func Parse(format string) (*template.Template, error) {
	return template.New("").Funcs(basicFunctions).Parse(format)
}

// New creates a new empty template with the provided tag and built-in
// template functions.
func New(tag string) *template.Template {
	return template.New(tag).Funcs(basicFunctions)
}

// NewParse creates a new tagged template with the basic functions
// and parses the given format.
//
// Deprecated: this function is unused and will be removed in the next release. Use [New] if you need to set a tag, or [Parse] instead.
func NewParse(tag, format string) (*template.Template, error) {
	return template.New(tag).Funcs(basicFunctions).Parse(format)
}

// padWithSpace adds whitespace to the input if the input is non-empty
func padWithSpace(source string, prefix, suffix int) string {
	if source == "" {
		return source
	}
	return strings.Repeat(" ", prefix) + source + strings.Repeat(" ", suffix)
}

// truncateWithLength truncates the source string up to the length provided by the input
func truncateWithLength(source string, length int) string {
	if len(source) < length {
		return source
	}
	return source[:length]
}
