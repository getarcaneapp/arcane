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

type SettingsSearchService struct {
	categories []category.Category
	once       sync.Once
}

func NewSettingsSearchService() *SettingsSearchService {
	s := &SettingsSearchService{}
	s.initCategories()
	return s
}

func (s *SettingsSearchService) initCategories() {
	s.once.Do(func() {
		s.categories = s.buildCategoriesFromModel()
	})
}

// GetSettingsCategories returns all available settings categories with their metadata
func (s *SettingsSearchService) GetSettingsCategories() []category.Category {
	return s.categories
}

func (s *SettingsSearchService) buildCategoriesFromModel() []category.Category {
	// Extract category metadata from struct tags (catmeta)
	catMetaMap := utils.ExtractCategoryMetadata(models.Settings{}, nil)

	// map category id -> list of settings
	categories := map[string][]meta.Metadata{}
	categoryOrder := []string{} // Track order from first appearance in struct

	rt := reflect.TypeOf(models.Settings{})
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
			categoryID = "general"
		}

		// Skip internal category
		if categoryID == "internal" {
			continue
		}

		// Track category order from first appearance
		if len(categories[categoryID]) == 0 && !utils.Contains(categoryOrder, categoryID) {
			categoryOrder = append(categoryOrder, categoryID)
		}

		sm := meta.Metadata{
			Key:         key,
			Label:       label,
			Type:        typ,
			Description: desc,
			Keywords:    keywords,
		}

		categories[categoryID] = append(categories[categoryID], sm)
	}

	// Build final category list in struct order
	results := []category.Category{}
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

		results = append(results, category.Category{
			ID:          catMeta["id"],
			Title:       catMeta["title"],
			Description: catMeta["description"],
			Icon:        catMeta["icon"],
			URL:         catMeta["url"],
			Keywords:    keywords,
			Settings:    categories[catID],
		})
	}

	return results
}

// Search performs a relevance-scored search across settings categories and individual settings
func (s *SettingsSearchService) Search(query string) search.Response {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return search.Response{
			Results: []category.Category{},
			Query:   query,
			Count:   0,
		}
	}

	categories := s.GetSettingsCategories()
	var results []category.Category

	for _, cat := range categories {
		// Check if category matches
		categoryMatch := s.categoryMatches(cat, query)

		// Check individual settings with enhanced matching
		matchingSettings := s.findMatchingSettings(cat.Settings, query)

		if categoryMatch || len(matchingSettings) > 0 {
			// Calculate relevance score based on match quality
			relevanceScore := s.calculateRelevance(cat, matchingSettings, query)

			categoryResult := category.Category{
				ID:             cat.ID,
				Title:          cat.Title,
				Description:    cat.Description,
				Icon:           cat.Icon,
				URL:            cat.URL,
				Keywords:       cat.Keywords,
				Settings:       cat.Settings,
				RelevanceScore: relevanceScore,
			}

			if len(matchingSettings) > 0 {
				categoryResult.MatchingSettings = matchingSettings
			}

			results = append(results, categoryResult)
		}
	}

	// Sort by relevance (highest first)
	s.sortByRelevance(results)

	return search.Response{
		Results: results,
		Query:   query,
		Count:   len(results),
	}
}

func (s *SettingsSearchService) categoryMatches(cat category.Category, query string) bool {
	if strings.Contains(strings.ToLower(cat.Title), query) {
		return true
	}
	if strings.Contains(strings.ToLower(cat.Description), query) {
		return true
	}
	for _, keyword := range cat.Keywords {
		if strings.Contains(strings.ToLower(keyword), query) {
			return true
		}
	}
	return false
}

func (s *SettingsSearchService) findMatchingSettings(settings []meta.Metadata, query string) []meta.Metadata {
	var matching []meta.Metadata
	for _, setting := range settings {
		if s.settingMatches(setting, query) {
			matching = append(matching, setting)
		}
	}
	return matching
}

func (s *SettingsSearchService) settingMatches(setting meta.Metadata, query string) bool {
	if strings.Contains(strings.ToLower(setting.Key), query) {
		return true
	}
	if strings.Contains(strings.ToLower(setting.Label), query) {
		return true
	}
	if strings.Contains(strings.ToLower(setting.Description), query) {
		return true
	}
	for _, keyword := range setting.Keywords {
		if strings.Contains(strings.ToLower(keyword), query) {
			return true
		}
	}
	return false
}

func (s *SettingsSearchService) calculateRelevance(cat category.Category, matchingSettings []meta.Metadata, query string) int {
	score := 0

	// Category title/description match gets high score
	if strings.Contains(strings.ToLower(cat.Title), query) {
		score += 20
	}
	if strings.Contains(strings.ToLower(cat.Description), query) {
		score += 15
	}

	// Exact keyword match
	for _, keyword := range cat.Keywords {
		if strings.ToLower(keyword) == query {
			score += 25
		} else if strings.Contains(strings.ToLower(keyword), query) {
			score += 10
		}
	}

	// Add score for individual setting matches
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

		for _, keyword := range setting.Keywords {
			if strings.ToLower(keyword) == query {
				score += 20
			} else if strings.Contains(strings.ToLower(keyword), query) {
				score += 5
			}
		}
	}

	return score
}

func (s *SettingsSearchService) sortByRelevance(results []category.Category) {
	sort.Slice(results, func(i, j int) bool {
		return results[i].RelevanceScore > results[j].RelevanceScore
	})
}
