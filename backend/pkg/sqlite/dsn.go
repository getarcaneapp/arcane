package sqlite

import (
	"net/url"
	"slices"
	"strings"
)

// NormalizeQuery converts mattn-style SQLite DSN options to _pragma values and
// drops every foreign_keys setting so callers control enforcement.
func NormalizeQuery(query url.Values) url.Values {
	qs := make(url.Values, len(query))
	for k, v := range query {
		switch k {
		case "_auto_vacuum", "_vacuum":
			qs.Add("_pragma", "auto_vacuum("+v[0]+")")
		case "_busy_timeout", "_timeout":
			qs.Add("_pragma", "busy_timeout("+v[0]+")")
		case "_case_sensitive_like", "_cslike":
			qs.Add("_pragma", "case_sensitive_like("+v[0]+")")
		case "_foreign_keys", "_fk":
			continue
		case "_locking_mode", "_locking":
			qs.Add("_pragma", "locking_mode("+v[0]+")")
		case "_secure_delete":
			qs.Add("_pragma", "secure_delete("+v[0]+")")
		case "_synchronous", "_sync":
			qs.Add("_pragma", "synchronous("+v[0]+")")
		case "_journal_mode":
			qs.Add("_pragma", "journal_mode("+v[0]+")")
		case "_txlock":
			qs.Add("_txlock", v[0])
		case "_pragma":
			for _, pragma := range v {
				if strings.EqualFold(pragmaName(pragma), "foreign_keys") {
					continue
				}
				qs.Add("_pragma", pragma)
			}
		default:
			qs[k] = v
		}
	}
	return qs
}

// HasPragma reports whether query sets the named _pragma.
func HasPragma(query url.Values, name string) bool {
	return slices.ContainsFunc(query["_pragma"], func(pragma string) bool {
		return strings.EqualFold(pragmaName(pragma), name)
	})
}

// pragmaName strips arguments, schema qualifiers, and quotes from a _pragma value.
func pragmaName(pragma string) string {
	name := strings.TrimSpace(pragma)
	if end := strings.IndexAny(name, "=( \t\r\n"); end >= 0 {
		name = name[:end]
	}
	if _, unqualified, found := strings.Cut(name, "."); found {
		name = unqualified
	}
	return strings.Trim(name, "\"'`[]")
}
