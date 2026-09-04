package search

import (
	"github.com/getarcaneapp/arcane/types/v2/category"
	searchtypes "github.com/getarcaneapp/arcane/types/v2/search"
)

type CustomizeSearchService struct {
	categories []category.Category
}

func NewCustomizeSearchService() *CustomizeSearchService {
	return &CustomizeSearchService{categories: BuildCategories[CustomizeItem](searchtypes.CustomizeProfile)}
}

// GetCustomizeCategories returns the category index initialized by the service.
func (s *CustomizeSearchService) GetCustomizeCategories() []category.Category {
	return s.categories
}
