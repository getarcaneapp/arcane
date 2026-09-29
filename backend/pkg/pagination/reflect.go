package pagination

import (
	"cmp"
	"reflect"
	"strconv"
	"strings"

	kit "go.getarcane.app/kit/pkg"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func PaginateAndSortDB[M any](params QueryParams, query *gorm.DB, result *[]M) (Response, error) {
	sortColumn := params.Sort
	sortDirection := cmp.Or(string(params.Order), "asc")

	modelType := reflect.TypeFor[M]()
	capitalizedSortColumn := kit.Capitalize(sortColumn)
	sortField, sortFieldFound := modelType.FieldByName(capitalizedSortColumn)
	isSortable, _ := strconv.ParseBool(sortField.Tag.Get("sortable"))

	sortDirection = kit.Ternary(sortDirection != "asc" && sortDirection != "desc", "asc", sortDirection)

	var orderColumns []clause.OrderByColumn
	columnName := ""
	if sortFieldFound && isSortable {
		columnName = kit.SnakeCase(sortColumn)
		orderColumns = append(orderColumns, clause.OrderByColumn{
			Column: clause.Column{Name: columnName},
			Desc:   sortDirection == "desc",
		})
	}
	// Without a total order the page cut is undefined: rows can repeat or go
	// missing across pages when the requested column has ties (or no sort was
	// requested at all) and the database reorders a plain scan. Tie-break on the
	// primary key whenever the model has one and the caller did not already.
	if _, hasID := modelType.FieldByName("ID"); hasID && columnName != "id" && !orderedByIDInternal(query) {
		orderColumns = append(orderColumns, clause.OrderByColumn{
			Column: clause.Column{Table: clause.CurrentTable, Name: "id"},
		})
	}
	if len(orderColumns) > 0 {
		query = query.Clauses(clause.OrderBy{Columns: orderColumns})
	}

	limit := params.Limit
	// limit = -1 means "show all" - skip pagination
	if limit == -1 {
		return paginateDBAll(query, result)
	}
	if limit <= 0 {
		limit = 20
	} else if limit > 100 {
		limit = 100
	}

	// The caller's offset is passed through as-is. Converting it to a page number
	// and back rounded it down to a page boundary, so ?start=25&limit=20 returned
	// rows 20-39 instead of 25-44 — and clamping limit above skewed it further,
	// since the clamped limit divided a start computed from the requested one.
	return paginateDB(params.Start, limit, query, result, params.SkipCount)
}

// orderedByIDInternal reports whether the query's existing ORDER BY already includes the id column.
func orderedByIDInternal(query *gorm.DB) bool {
	existing, ok := query.Statement.Clauses["ORDER BY"]
	if !ok {
		return false
	}
	orderBy, ok := existing.Expression.(clause.OrderBy)
	if !ok {
		return false
	}
	for _, column := range orderBy.Columns {
		name := column.Column.Name
		if column.Column.Raw {
			fields := strings.Fields(name)
			if len(fields) == 0 {
				continue
			}
			name = fields[0]
		}
		name = strings.Trim(name[strings.LastIndex(name, ".")+1:], "`\"")
		if strings.EqualFold(name, "id") || name == clause.PrimaryKey {
			return true
		}
	}
	return false
}

// paginateDBAll returns all results without pagination limits or a COUNT(*).
func paginateDBAll[M any](query *gorm.DB, result *[]M) (Response, error) {
	if err := query.Find(result).Error; err != nil {
		return Response{}, err
	}

	totalItems := len(*result)
	return Response{
		TotalPages:   1,
		TotalItems:   int64(totalItems),
		CurrentPage:  1,
		ItemsPerPage: totalItems,
	}, nil
}

// paginateDB applies offset/limit pagination. When skipCount is true the COUNT(*) is elided
// and TotalItems/TotalPages are returned as UnknownTotal.
// Count runs before Find so it sees a clean session (Find sets Statement.Dest in GORM v2).
func paginateDB[M any](offset, pageSize int, query *gorm.DB, result *[]M, skipCount bool) (Response, error) {
	if offset < 0 {
		offset = 0
	}
	if pageSize < 1 {
		pageSize = 1
	}

	// CurrentPage is a display value derived from the offset; the query itself
	// uses the exact offset, so an offset that does not land on a page boundary
	// is honoured rather than rounded.
	page := (offset / pageSize) + 1

	var totalItems int64
	if !skipCount {
		if err := query.Count(&totalItems).Error; err != nil {
			return Response{}, err
		}
	}

	if err := query.Offset(offset).Limit(pageSize).Find(result).Error; err != nil {
		return Response{}, err
	}

	if skipCount {
		return Response{
			TotalPages:   UnknownTotal,
			TotalItems:   UnknownTotal,
			CurrentPage:  page,
			ItemsPerPage: pageSize,
		}, nil
	}

	totalPages := (totalItems + int64(pageSize) - 1) / int64(pageSize)
	if totalItems == 0 {
		totalPages = 1
	}

	return Response{
		TotalPages:   totalPages,
		TotalItems:   totalItems,
		CurrentPage:  page,
		ItemsPerPage: pageSize,
	}, nil
}
