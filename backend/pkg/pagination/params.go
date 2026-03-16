package pagination

type QueryParams struct {
	SearchQuery
	SortParams
	PaginationParams
	Filters map[string]string
}
