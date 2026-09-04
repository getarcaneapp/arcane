package settings

import (
	"github.com/getarcaneapp/arcane/backend/v2/internal/search"
	"github.com/getarcaneapp/arcane/types/v2/category"
	searchtypes "github.com/getarcaneapp/arcane/types/v2/search"
)

type SettingsSearchService struct {
	categories []category.Category
}

func NewSettingsSearchService() *SettingsSearchService {
	return &SettingsSearchService{categories: search.BuildCategories[Settings](searchtypes.SettingsProfile)}
}

// GetSettingsCategories returns the category index initialized by the service.
func (s *SettingsSearchService) GetSettingsCategories() []category.Category {
	return s.categories
}
