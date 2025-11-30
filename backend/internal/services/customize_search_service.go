package services

import (
	"reflect"
	"sort"
	"strings"
	"sync"

	"github.com/getarcaneapp/arcane/backend/internal/dto"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/internal/utils"
)

type CustomizeSearchService struct {
	categories []dto.CustomizeCategory
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
func (s *CustomizeSearchService) GetCustomizeCategories() []dto.CustomizeCategory {
	return s.categories
}

func (s *CustomizeSearchService) buildCategoriesFromModel() []dto.CustomizeCategory {
	// Extract category metadata from struct tags (catmeta)
	catMetaMap := utils.ExtractCategoryMetadata(models.CustomizeItem{}, nil)

	// map category id -> list of customizations
	categories := map[string][]dto.CustomizationMeta{}
	categoryOrder := []string{} // Track order from first appearance in struct

	rt := reflect.TypeOf(models.CustomizeItem{})
	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)
		keyTag := field.Tag.Get("key")
		key, _, _ := strings.Cut(keyTag, ",")
		if key == "" {
			continue
		}

		meta := utils.ParseMetaTag(field.Tag.Get("meta"))
		label := meta["label"]
		if label == "" {
			label = key
		}
		typ := meta["type"]
		if typ == "" {
			typ = "text"
		}
		desc := meta["description"]
		keywords := utils.ParseKeywords(meta["keywords"])
		categoryID := meta["category"]
		if categoryID == "" {
			categoryID = "defaults"
		}

		// Track category order from first appearance
		if len(categories[categoryID]) == 0 && !utils.Contains(categoryOrder, categoryID) {
			categoryOrder = append(categoryOrder, categoryID)
		}

		cm := dto.CustomizationMeta{
			Key:         key,
			Label:       label,
			Type:        typ,
			Description: desc,
			Keywords:    keywords,
		}

		categories[categoryID] = append(categories[categoryID], cm)
	}

	// Build final category list in struct order
	result := []dto.CustomizeCategory{}
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

		result = append(result, dto.CustomizeCategory{
			ID:             catMeta["id"],
			Title:          catMeta["title"],
			Description:    catMeta["description"],
			Icon:           catMeta["icon"],
			URL:            catMeta["url"],
			Keywords:       keywords,
			Customizations: categories[catID],
		})
	}

	return result
}

// Search performs a relevance-scored search across all customization categories and items
func (s *CustomizeSearchService) Search(query string) dto.CustomizeSearchResponse {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return dto.CustomizeSearchResponse{
			Results: []dto.CustomizeCategory{},
			Query:   query,
			Count:   0,
		}
	}

	categories := s.GetCustomizeCategories()
	results := []dto.CustomizeCategory{}

	for _, category := range categories {
		// Check if category matches
		categoryMatch := strings.Contains(strings.ToLower(category.Title), query) ||
			strings.Contains(strings.ToLower(category.Description), query) ||
			containsKeyword(category.Keywords, query)

		// Check individual customizations
		matchingCustomizations := []dto.CustomizationMeta{}
		for _, customization := range category.Customizations {
			if matchesCustomization(customization, query) {
				matchingCustomizations = append(matchingCustomizations, customization)
			}
		}

		if categoryMatch || len(matchingCustomizations) > 0 {
			relevanceScore := calculateCustomizeRelevance(category, matchingCustomizations, query)

			resultCategory := category
			if len(matchingCustomizations) > 0 {
				resultCategory.MatchingCustomizations = matchingCustomizations
			}
			resultCategory.RelevanceScore = relevanceScore

			results = append(results, resultCategory)
		}
	}

	// Sort by relevance (highest first)
	sort.Slice(results, func(i, j int) bool {
		return results[i].RelevanceScore > results[j].RelevanceScore
	})

	return dto.CustomizeSearchResponse{
		Results: results,
		Query:   query,
		Count:   len(results),
	}
}

func matchesCustomization(customization dto.CustomizationMeta, query string) bool {
	return strings.Contains(strings.ToLower(customization.Key), query) ||
		strings.Contains(strings.ToLower(customization.Label), query) ||
		strings.Contains(strings.ToLower(customization.Description), query) ||
		containsKeyword(customization.Keywords, query)
}

func calculateCustomizeRelevance(category dto.CustomizeCategory, matchingCustomizations []dto.CustomizationMeta, query string) int {
	score := 0

	// Category-level scoring
	if strings.ToLower(category.Title) == query {
		score += 30
	} else if strings.Contains(strings.ToLower(category.Title), query) {
		score += 20
	}

	if strings.Contains(strings.ToLower(category.Description), query) {
		score += 15
	}

	// Exact keyword match
	for _, keyword := range category.Keywords {
		if strings.ToLower(keyword) == query {
			score += 25
			break
		} else if strings.Contains(strings.ToLower(keyword), query) {
			score += 10
			break
		}
	}

	// Customization-level scoring
	for _, customization := range matchingCustomizations {
		if strings.ToLower(customization.Key) == query {
			score += 30
		} else if strings.Contains(strings.ToLower(customization.Key), query) {
			score += 15
		}

		if strings.Contains(strings.ToLower(customization.Label), query) {
			score += 12
		}

		if strings.Contains(strings.ToLower(customization.Description), query) {
			score += 8
		}

		// Exact keyword match
		for _, keyword := range customization.Keywords {
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
