package pagination

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBuildResponseFromFilterResult(t *testing.T) {
	tests := []struct {
		name     string
		result   FilterResult[int]
		params   QueryParams
		expected Response
	}{
		{
			name: "show all mode (limit = -1)",
			result: FilterResult[int]{
				Items:          []int{1, 2, 3, 4, 5},
				TotalCount:     5,
				TotalAvailable: 10,
			},
			params: QueryParams{
				PaginationParams: PaginationParams{
					Start: 0,
					Limit: -1,
				},
			},
			expected: Response{
				TotalPages:      1,
				TotalItems:      5,
				CurrentPage:     1,
				ItemsPerPage:    5,
				GrandTotalItems: 10,
			},
		},
		{
			name: "show all mode with zero items",
			result: FilterResult[int]{
				Items:          []int{},
				TotalCount:     0,
				TotalAvailable: 0,
			},
			params: QueryParams{
				PaginationParams: PaginationParams{
					Start: 0,
					Limit: -1,
				},
			},
			expected: Response{
				TotalPages:      1,
				TotalItems:      0,
				CurrentPage:     1,
				ItemsPerPage:    0,
				GrandTotalItems: 0,
			},
		},
		{
			name: "normal pagination (limit > 0)",
			result: FilterResult[int]{
				Items:          []int{1, 2, 3},
				TotalCount:     25,
				TotalAvailable: 100,
			},
			params: QueryParams{
				PaginationParams: PaginationParams{
					Start: 0,
					Limit: 10,
				},
			},
			expected: Response{
				TotalPages:      3,
				TotalItems:      25,
				CurrentPage:     1,
				ItemsPerPage:    10,
				GrandTotalItems: 100,
			},
		},
		{
			name: "normal pagination second page",
			result: FilterResult[int]{
				Items:          []int{11, 12, 13},
				TotalCount:     25,
				TotalAvailable: 100,
			},
			params: QueryParams{
				PaginationParams: PaginationParams{
					Start: 10,
					Limit: 10,
				},
			},
			expected: Response{
				TotalPages:      3,
				TotalItems:      25,
				CurrentPage:     2,
				ItemsPerPage:    10,
				GrandTotalItems: 100,
			},
		},
		{
			name: "invalid limit (zero) falls back to show all",
			result: FilterResult[int]{
				Items:          []int{1, 2, 3},
				TotalCount:     3,
				TotalAvailable: 3,
			},
			params: QueryParams{
				PaginationParams: PaginationParams{
					Start: 0,
					Limit: 0,
				},
			},
			expected: Response{
				TotalPages:      1,
				TotalItems:      3,
				CurrentPage:     1,
				ItemsPerPage:    3,
				GrandTotalItems: 3,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := BuildResponseFromFilterResult(tt.result, tt.params)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestPaginateItemsFunction(t *testing.T) {
	items := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}

	tests := []struct {
		name     string
		params   PaginationParams
		expected []int
	}{
		{
			name:     "show all mode (limit = -1)",
			params:   PaginationParams{Start: 0, Limit: -1},
			expected: []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
		},
		{
			name:     "show all mode (limit = 0)",
			params:   PaginationParams{Start: 0, Limit: 0},
			expected: []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
		},
		{
			name:     "normal pagination first page",
			params:   PaginationParams{Start: 0, Limit: 3},
			expected: []int{1, 2, 3},
		},
		{
			name:     "normal pagination second page",
			params:   PaginationParams{Start: 3, Limit: 3},
			expected: []int{4, 5, 6},
		},
		{
			name:     "pagination at end of list",
			params:   PaginationParams{Start: 8, Limit: 5},
			expected: []int{9, 10},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := paginateItemsFunction(items, tt.params)
			assert.Equal(t, tt.expected, result)
		})
	}
}
