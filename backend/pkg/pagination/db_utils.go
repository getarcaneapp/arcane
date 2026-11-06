package pagination

import (
	"fmt"
	"strings"

	"gorm.io/gorm"

	"kit.getarcane.app"
	"kit.getarcane.app/mapping"
)

func ApplyFilter(q *gorm.DB, column, value string) *gorm.DB {
	if value == "" {
		return q
	}
	if strings.Contains(value, ",") {
		return q.Where(column+" IN ?", kit.TrimNonEmpty(strings.Split(value, ",")))
	}
	return q.Where(column+" = ?", value)
}

func ApplyLikeSearch(q *gorm.DB, search, condition string) *gorm.DB {
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

func (params QueryParams) PaginateSortAndMapDB[M, D any](query *gorm.DB, records *[]M) ([]D, Response, error) {
	paginationResp, err := PaginateAndSortDB(params, query, records)
	if err != nil {
		return nil, Response{}, fmt.Errorf("paginate db records: %w", err)
	}

	out, err := mapping.MapSlice[M, D](*records)
	if err != nil {
		return nil, Response{}, fmt.Errorf("map db records: %w", err)
	}

	return out, paginationResp, nil
}

func ApplyBooleanFilter(q *gorm.DB, column, value string) *gorm.DB {
	if value == "" {
		return q
	}

	parts := strings.Split(value, ",")
	var boolValues []bool

	for _, part := range parts {
		if value, valid := kit.ParseBool(part); valid {
			boolValues = append(boolValues, value)
		}
	}

	if len(boolValues) == 1 {
		return q.Where(column+" = ?", boolValues[0])
	} else if len(boolValues) > 1 {
		return q.Where(column+" IN ?", boolValues)
	}

	return q
}
