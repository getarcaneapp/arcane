package services

import (
	"reflect"
	"sort"
	"strings"
	"sync"

	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/internal/utils"
	"go.getarcane.app/types/category"
	"go.getarcane.app/types/meta"
	"go.getarcane.app/types/search"
)

type CustomizeSearchService struct {
	categories []category.Category
	once       sync.Once
}

func NewCustomizeSearchService() *CustomizeSearchService {
	s := &CustomizeSearchService{}
	s.initCategories()
	return s
}

func (s *CustomizeSearchService) initCategories() {
	s.once.Do(func() {
		s.categories = s.buildCategoriesFromModel()
	})
}

// GetCustomizeCategories returns all available customization categories with their metadata
func (s *CustomizeSearchService) GetCustomizeCategories() []category.Category {
	return s.categories
}

func (s *CustomizeSearchService) buildCategoriesFromModel() []category.Category {
	// Extract category metadata from struct tags (catmeta)
	catMetaMap := utils.ExtractCategoryMetadata(models.CustomizeItem{}, nil)

	// map category id -> list of customizations
	categories := map[string][]meta.Metadata{}
	categoryOrder := []string{} // Track order from first appearance in struct

	rt := reflect.TypeOf(models.CustomizeItem{})
	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)
		keyTag := field.Tag.Get("key")
		key, _, _ := strings.Cut(keyTag, ",")
		if key == "" {
			continue
		}

		metaTag := utils.ParseMetaTag(field.Tag.Get("meta"))
		label := metaTag["label"]
		if label == "" {
			label = key
		}
		typ := metaTag["type"]
		if typ == "" {
			typ = "text"
		}
		desc := metaTag["description"]
		keywords := utils.ParseKeywords(metaTag["keywords"])
		categoryID := metaTag["category"]
		if categoryID == "" {
			categoryID = "defaults"
		}

		// Track category order from first appearance
		if len(categories[categoryID]) == 0 && !utils.Contains(categoryOrder, categoryID) {
			categoryOrder = append(categoryOrder, categoryID)
		}

		cm := meta.Metadata{
			Key:         key,
			Label:       label,
			Type:        typ,
			Description: desc,
			Keywords:    keywords,
		}

		categories[categoryID] = append(categories[categoryID], cm)
	}

	// Build final category list in struct order
	result := []category.Category{}
	for _, catID := range categoryOrder {
		catMeta := catMetaMap[catID]
		if catMeta == nil {
			continue
		}

		// Parse keywords from catmeta
		keywords := utils.ParseKeywords(catMeta["keywords"])
		if keywords == nil {
			keywords = []string{}
		}

		result = append(result, category.Category{
			ID:          catMeta["id"],
			Title:       catMeta["title"],
			Description: catMeta["description"],
			Icon:        catMeta["icon"],
			URL:         catMeta["url"],
			Keywords:    keywords,
			Settings:    categories[catID],
		})
	}

	return result
}

// Search performs a relevance-scored search across all customization categories and items
func (s *CustomizeSearchService) Search(query string) search.Response {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return search.Response{
			Results: []category.Category{},
			Query:   query,
			Count:   0,
		}
	}

	categories := s.GetCustomizeCategories()
	results := []category.Category{}

	for _, cat := range categories {
		// Check if category matches
		categoryMatch := strings.Contains(strings.ToLower(cat.Title), query) ||
			strings.Contains(strings.ToLower(cat.Description), query) ||
			containsKeyword(cat.Keywords, query)

		// Check individual settings
		matchingSettings := []meta.Metadata{}
		for _, setting := range cat.Settings {
			if matchesSetting(setting, query) {
				matchingSettings = append(matchingSettings, setting)
			}
		}

		if categoryMatch || len(matchingSettings) > 0 {
			relevanceScore := calculateCustomizeRelevance(cat, matchingSettings, query)

			resultCategory := cat
			if len(matchingSettings) > 0 {
				resultCategory.MatchingSettings = matchingSettings
			}
			resultCategory.RelevanceScore = relevanceScore

			results = append(results, resultCategory)
		}
	}

	// Sort by relevance (highest first)
	sort.Slice(results, func(i, j int) bool {
		return results[i].RelevanceScore > results[j].RelevanceScore
	})

	return search.Response{
		Results: results,
		Query:   query,
		Count:   len(results),
	}
}

func matchesSetting(setting meta.Metadata, query string) bool {
	return strings.Contains(strings.ToLower(setting.Key), query) ||
		strings.Contains(strings.ToLower(setting.Label), query) ||
		strings.Contains(strings.ToLower(setting.Description), query) ||
		containsKeyword(setting.Keywords, query)
}

func calculateCustomizeRelevance(cat category.Category, matchingSettings []meta.Metadata, query string) int {
	score := 0

	// Category-level scoring
	if strings.ToLower(cat.Title) == query {
		score += 30
	} else if strings.Contains(strings.ToLower(cat.Title), query) {
		score += 20
	}

	if strings.Contains(strings.ToLower(cat.Description), query) {
		score += 15
	}

	// Exact keyword match
	for _, keyword := range cat.Keywords {
		if strings.ToLower(keyword) == query {
			score += 25
			break
		} else if strings.Contains(strings.ToLower(keyword), query) {
			score += 10
			break
		}
	}

	// Setting-level scoring
	for _, setting := range matchingSettings {
		if strings.ToLower(setting.Key) == query {
			score += 30
		} else if strings.Contains(strings.ToLower(setting.Key), query) {
			score += 15
		}

		if strings.Contains(strings.ToLower(setting.Label), query) {
			score += 12
		}

		if strings.Contains(strings.ToLower(setting.Description), query) {
			score += 8
		}

		// Exact keyword match
		for _, keyword := range setting.Keywords {
			if strings.ToLower(keyword) == query {
				score += 20
				break
			} else if strings.Contains(strings.ToLower(keyword), query) {
				score += 5
				break
			}
		}
	}

	return score
}

func containsKeyword(keywords []string, query string) bool {
	for _, keyword := range keywords {
		if strings.Contains(strings.ToLower(keyword), query) {
			return true
		}
	}
	return false
}
