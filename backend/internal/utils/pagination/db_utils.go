package pagination

import (
	"strings"

	"gorm.io/gorm"
)

// ApplyFilter adds a WHERE clause to the GORM query.
// It detects comma-separated values and uses IN (?) for multiple values,
// or = ? for single values.
func ApplyFilter(q *gorm.DB, column string, value string) *gorm.DB {
	if value == "" {
		return q
	}
	if strings.Contains(value, ",") {
		return q.Where(column+" IN ?", strings.Split(value, ","))
	}
	return q.Where(column+" = ?", value)
}
