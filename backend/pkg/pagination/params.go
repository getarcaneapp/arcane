package pagination

type (
	QueryParams struct {
		Filters map[string]string
		SortParams
		SearchQuery
		Params
	}
)
