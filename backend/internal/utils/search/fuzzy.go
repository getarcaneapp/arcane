package search

import (
	"strings"
)

// SearchQuery holds the parsed search query with SQL-ready clauses
type SearchQuery struct {
	// SQL WHERE clause (e.g., "LOWER(name) LIKE ? OR LOWER(api_url) LIKE ?")
	Clause string
	// Arguments for the clause
	Args []any
}

// BuildSearch parses a search query and returns a database-specific SQL clause.
// - Unquoted: fuzzy matching with wildcards between characters (case-insensitive)
// - Quoted ("..." or '...'): exact substring match (case-sensitive)
//
// dialect should be "sqlite" or "postgres"
func BuildSearch(query string, dialect string, columns ...string) SearchQuery {
	query = strings.TrimSpace(query)
	if query == "" || len(columns) == 0 {
		return SearchQuery{Clause: "1=1", Args: nil}
	}

	// Check for quoted exact match
	isExact := false
	rawQuery := query
	if len(query) >= 2 &&
		((query[0] == '"' && query[len(query)-1] == '"') ||
			(query[0] == '\'' && query[len(query)-1] == '\'')) {
		rawQuery = query[1 : len(query)-1]
		isExact = true
	}

	if isExact {
		return buildExactSearch(rawQuery, dialect, columns)
	}
	return buildFuzzySearch(rawQuery, columns)
}

// buildFuzzySearch creates a case-insensitive fuzzy search clause
func buildFuzzySearch(query string, columns []string) SearchQuery {
	pattern := buildFuzzyPattern(query)

	var clauses []string
	var args []any
	for _, col := range columns {
		clauses = append(clauses, "LOWER("+col+") LIKE ?")
		args = append(args, pattern)
	}

	return SearchQuery{
		Clause: strings.Join(clauses, " OR "),
		Args:   args,
	}
}

// buildExactSearch creates a case-sensitive exact substring search clause
func buildExactSearch(query string, dialect string, columns []string) SearchQuery {
	var clauses []string
	var args []any

	if dialect == "sqlite" {
		// SQLite LIKE is case-insensitive, use GLOB for case-sensitivity
		pattern := buildGlobPattern(query)
		for _, col := range columns {
			clauses = append(clauses, col+" GLOB ?")
			args = append(args, pattern)
		}
	} else {
		// PostgreSQL LIKE is case-sensitive by default
		pattern := buildLikePattern(query)
		for _, col := range columns {
			clauses = append(clauses, col+" LIKE ?")
			args = append(args, pattern)
		}
	}

	return SearchQuery{
		Clause: strings.Join(clauses, " OR "),
		Args:   args,
	}
}

// buildFuzzyPattern creates a LIKE pattern with wildcards between each character.
// Example: "dck" -> "%d%c%k%"
func buildFuzzyPattern(query string) string {
	query = strings.ToLower(query)
	var b strings.Builder
	b.WriteString("%")
	for _, r := range query {
		switch r {
		case '%':
			b.WriteString("\\%")
		case '_':
			b.WriteString("\\_")
		case '\\':
			b.WriteString("\\\\")
		default:
			b.WriteRune(r)
		}
		b.WriteString("%")
	}
	return b.String()
}

// buildLikePattern creates a LIKE pattern for exact substring matching
func buildLikePattern(query string) string {
	query = strings.ReplaceAll(query, "\\", "\\\\")
	query = strings.ReplaceAll(query, "%", "\\%")
	query = strings.ReplaceAll(query, "_", "\\_")
	return "%" + query + "%"
}

// buildGlobPattern creates a case-sensitive GLOB pattern for SQLite
func buildGlobPattern(query string) string {
	query = strings.ReplaceAll(query, "[", "[[]")
	query = strings.ReplaceAll(query, "*", "[*]")
	query = strings.ReplaceAll(query, "?", "[?]")
	return "*" + query + "*"
}
