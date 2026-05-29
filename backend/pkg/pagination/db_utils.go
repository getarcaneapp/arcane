package pagination

import (
	"fmt"
	"strings"

	"github.com/getarcaneapp/arcane/backend/pkg/utils/mapper"
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

// ApplyLikeSearch adds a LIKE search condition using the same wildcard pattern
// for every placeholder in condition.
func ApplyLikeSearch(q *gorm.DB, search string, condition string) *gorm.DB {
	term := strings.TrimSpace(search)
	if term == "" {
		return q
	}

	pattern := "%" + term + "%"
	args := make([]any, strings.Count(condition, "?"))
	for i := range args {
		args[i] = pattern
	}

	return q.Where(condition, args...)
}

// PaginateSortAndMapDB paginates DB records and maps them to API DTOs.
func PaginateSortAndMapDB[M any, D any](params QueryParams, query *gorm.DB, records *[]M) ([]D, Response, error) {
	paginationResp, err := PaginateAndSortDB(params, query, records)
	if err != nil {
		return nil, Response{}, fmt.Errorf("paginate db records: %w", err)
	}

	out, err := mapper.MapSlice[M, D](*records)
	if err != nil {
		return nil, Response{}, fmt.Errorf("map db records: %w", err)
	}

	return out, paginationResp, nil
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
