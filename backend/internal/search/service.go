package search

import (
	"cmp"
	"reflect"
	"slices"
	"sort"
	"strings"

	"github.com/getarcaneapp/arcane/types/v3"
	"go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
)

type CustomizeSearchService struct {
	categories []types.Category
}

func NewCustomizeSearchService() *CustomizeSearchService {
	return &CustomizeSearchService{categories: BuildCategories[CustomizeItem](types.SearchProfileCustomize)}
}

// GetCustomizeCategories returns the category index initialized by the service.
func (s *CustomizeSearchService) GetCustomizeCategories() []types.Category {
	return s.categories
}

// BuildCategories extracts searchable metadata in model field order.
func BuildCategories[T any](profile types.SearchProfile) []types.Category {
	catMetaMap := utils.ExtractCategoryMetadata[T]()

	categories := map[string][]types.SettingMetadata{}
	var categoryOrder []string

	rt := reflect.TypeFor[T]()
	for field := range rt.Fields() {
		keyTag := field.Tag.Get("key")
		key, _, _ := strings.Cut(keyTag, ",")
		if key == "" {
			continue
		}

		metaTag := utils.ParseMetaTag(field.Tag.Get("meta"))
		label := cmp.Or(metaTag["label"], key)
		typ := cmp.Or(metaTag["type"], "text")
		desc := metaTag["description"]
		keywords := utils.ParseKeywords(metaTag["keywords"])
		categoryID := metaTag["category"]
		if categoryID == "" && profile == types.SearchProfileSettings {
			catMeta := utils.ParseMetaTag(field.Tag.Get("catmeta"))
			categoryID = catMeta["id"]
		}
		if categoryID == "" {
			categoryID = kit.Ternary(profile == types.SearchProfileSettings, "jobs", "defaults")
		}

		if categoryID == "internal" && profile == types.SearchProfileSettings {
			continue
		}

		if len(categories[categoryID]) == 0 && !slices.Contains(categoryOrder, categoryID) {
			categoryOrder = append(categoryOrder, categoryID)
		}

		sm := types.SettingMetadata{
			Key:         key,
			Label:       label,
			Type:        typ,
			Description: desc,
			Keywords:    keywords,
		}

		categories[categoryID] = append(categories[categoryID], sm)
	}

	results := []types.Category{}
	for _, catID := range categoryOrder {
		catMeta := catMetaMap[catID]
		if catMeta == nil {
			continue
		}

		keywords := utils.ParseKeywords(catMeta["keywords"])
		if keywords == nil {
			keywords = []string{}
		}

		results = append(results, types.Category{
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

// Search matches category metadata using the selected ranking profile.
func Search(categories []types.Category, query string, profile types.SearchProfile) types.SearchResponse {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return types.SearchResponse{
			Results: []types.Category{},
			Query:   query,
			Count:   0,
		}
	}

	var results []types.Category
	if profile == types.SearchProfileCustomize {
		results = []types.Category{}
	}

	for _, cat := range categories {
		categoryMatch := categoryMatches(cat, query)

		matchingSettings := findMatchingSettings(cat.Settings, query)

		if categoryMatch || len(matchingSettings) > 0 {
			relevanceScore := calculateRelevance(cat, matchingSettings, query, profile)

			categoryResult := types.Category{
				ID:             cat.ID,
				Title:          cat.Title,
				Description:    cat.Description,
				Icon:           cat.Icon,
				URL:            cat.URL,
				Keywords:       cat.Keywords,
				Settings:       cat.Settings,
				RelevanceScore: relevanceScore,
			}

			if profile == types.SearchProfileCustomize {
				categoryResult.MatchingSettings = cat.MatchingSettings
			}

			if len(matchingSettings) > 0 {
				categoryResult.MatchingSettings = matchingSettings
			}

			results = append(results, categoryResult)
		}
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].RelevanceScore > results[j].RelevanceScore
	})

	return types.SearchResponse{
		Results: results,
		Query:   query,
		Count:   len(results),
	}
}

func categoryMatches(cat types.Category, query string) bool {
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

func findMatchingSettings(settings []types.SettingMetadata, query string) []types.SettingMetadata {
	var matching []types.SettingMetadata
	for _, setting := range settings {
		if settingMatches(setting, query) {
			matching = append(matching, setting)
		}
	}
	return matching
}

func settingMatches(setting types.SettingMetadata, query string) bool {
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

func calculateRelevance(cat types.Category, matchingSettings []types.SettingMetadata, query string, profile types.SearchProfile) int {
	score := 0

	if profile == types.SearchProfileCustomize && strings.ToLower(cat.Title) == query {
		score += 30
	} else if strings.Contains(strings.ToLower(cat.Title), query) {
		score += 20
	}
	if strings.Contains(strings.ToLower(cat.Description), query) {
		score += 15
	}

	for _, keyword := range cat.Keywords {
		if strings.ToLower(keyword) == query {
			score += 25
			if profile == types.SearchProfileCustomize {
				break
			}
		} else if strings.Contains(strings.ToLower(keyword), query) {
			score += 10
			if profile == types.SearchProfileCustomize {
				break
			}
		}
	}

	for _, setting := range matchingSettings {
		score += calculateSettingRelevance(setting, query, profile)
	}

	return score
}

func calculateSettingRelevance(setting types.SettingMetadata, query string, profile types.SearchProfile) int {
	score := 0
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
			if profile == types.SearchProfileCustomize {
				break
			}
		} else if strings.Contains(strings.ToLower(keyword), query) {
			score += 5
			if profile == types.SearchProfileCustomize {
				break
			}
		}
	}

	return score
}
