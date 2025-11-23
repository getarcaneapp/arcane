package pagination

type Response struct {
	TotalPages      int64 `json:"totalPages"`
	TotalItems      int64 `json:"totalItems"`
	CurrentPage     int   `json:"currentPage"`
	ItemsPerPage    int   `json:"itemsPerPage"`
	GrandTotalItems int64 `json:"grandTotalItems,omitempty"`
}
