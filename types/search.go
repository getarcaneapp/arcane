package types

type (
	SearchRequest struct {
		Query string `json:"query" binding:"required,min=1"`
	}

	SearchResponse struct {
		Query   string     `json:"query"`
		Results []Category `json:"results"`
		Count   int        `json:"count"`
	}
)
