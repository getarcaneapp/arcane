package pagination

import (
	"reflect"
	"strconv"

	stringutils "github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func PaginateAndSortDB(params QueryParams, query *gorm.DB, result any) (Response, error) {
	sortColumn := params.Sort
	sortDirection := string(params.Order)

	if sortDirection == "" {
		sortDirection = "asc"
	}

	capitalizedSortColumn := stringutils.CapitalizeFirstLetter(sortColumn)
	sortField, sortFieldFound := reflect.TypeOf(result).Elem().Elem().FieldByName(capitalizedSortColumn)
	isSortable, _ := strconv.ParseBool(sortField.Tag.Get("sortable"))

	sortDirection = normalizeSortDirection(sortDirection)

	if sortFieldFound && isSortable {
		columnName := stringutils.CamelCaseToSnakeCase(sortColumn)
		query = query.Clauses(clause.OrderBy{
			Columns: []clause.OrderByColumn{
				{Column: clause.Column{Name: columnName}, Desc: sortDirection == "desc"},
			},
		})
	}

	limit := params.Limit
	// limit = -1 means "show all" - skip pagination
	if limit == -1 {
		return paginateDBAll(query, result, params.SkipCount)
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

// paginateDBAll returns all results without pagination limits.
// When skipCount is true the COUNT(*) is elided and TotalItems/TotalPages are returned as UnknownTotal.
// Count runs before Find so it sees a clean session (Find sets Statement.Dest in GORM v2).
func paginateDBAll(query *gorm.DB, result any, skipCount bool) (Response, error) {
	var totalItems int64
	if !skipCount {
		if err := query.Count(&totalItems).Error; err != nil {
			return Response{}, err
		}
	}

	if err := query.Find(result).Error; err != nil {
		return Response{}, err
	}

	if skipCount {
		return Response{
			TotalPages:   UnknownTotal,
			TotalItems:   UnknownTotal,
			CurrentPage:  1,
			ItemsPerPage: 0,
		}, nil
	}

	return Response{
		TotalPages:   1,
		TotalItems:   totalItems,
		CurrentPage:  1,
		ItemsPerPage: int(totalItems),
	}, nil
}

// paginateDB applies offset/limit pagination. When skipCount is true the COUNT(*) is elided
// and TotalItems/TotalPages are returned as UnknownTotal.
// Count runs before Find so it sees a clean session (Find sets Statement.Dest in GORM v2).
func paginateDB(offset int, pageSize int, query *gorm.DB, result any, skipCount bool) (Response, error) {
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

func normalizeSortDirection(direction string) string {
	if direction != "asc" && direction != "desc" {
		return "asc"
	}
	return direction
}
