package pagination

type QueryParams struct {
	SearchQuery
	SortParams
	Params

	Filters map[string]string
}
