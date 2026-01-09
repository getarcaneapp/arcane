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
		values := strings.Split(value, ",")
		for i := range values {
			values[i] = strings.TrimSpace(values[i])
		}
		return q.Where(column+" IN ?", values)
	}
	return q.Where(column+" = ?", value)
}

// ApplyBooleanFilter adds a WHERE clause for boolean columns.
// It detects comma-separated values and maps "true"/"1" to true and "false"/"0" to false.
func ApplyBooleanFilter(q *gorm.DB, column string, value string) *gorm.DB {
	if value == "" {
		return q
	}

	parts := strings.Split(value, ",")
	var boolValues []bool

	for _, part := range parts {
		switch strings.TrimSpace(part) {
		case "true", "1":
			boolValues = append(boolValues, true)
		case "false", "0":
			boolValues = append(boolValues, false)
		}
	}

	if len(boolValues) == 1 {
		return q.Where(column+" = ?", boolValues[0])
	} else if len(boolValues) > 1 {
		return q.Where(column+" IN ?", boolValues)
	}

	return q
}
