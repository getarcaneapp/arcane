package search

import (
	"reflect"
	"slices"
	"sort"
	"strings"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/getarcaneapp/arcane/types/v2/category"
	"github.com/getarcaneapp/arcane/types/v2/meta"
	searchtypes "github.com/getarcaneapp/arcane/types/v2/search"
)

// BuildCategories extracts searchable metadata in model field order.
func BuildCategories[T any](profile searchtypes.Profile) []category.Category {
	catMetaMap := utils.ExtractCategoryMetadata[T]()

	categories := map[string][]meta.Metadata{}
	var categoryOrder []string

	rt := reflect.TypeFor[T]()
	for field := range rt.Fields() {
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
		if categoryID == "" && profile == searchtypes.SettingsProfile {
			catMeta := utils.ParseMetaTag(field.Tag.Get("catmeta"))
			categoryID = catMeta["id"]
		}
		if categoryID == "" {
			categoryID = "defaults"
			if profile == searchtypes.SettingsProfile {
				categoryID = "jobs"
			}
		}

		if categoryID == "internal" && profile == searchtypes.SettingsProfile {
			continue
		}

		if len(categories[categoryID]) == 0 && !slices.Contains(categoryOrder, categoryID) {
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

	results := []category.Category{}
	for _, catID := range categoryOrder {
		catMeta := catMetaMap[catID]
		if catMeta == nil {
			continue
		}

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

// Search matches category metadata using the selected ranking profile.
func Search(categories []category.Category, query string, profile searchtypes.Profile) searchtypes.Response {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return searchtypes.Response{
			Results: []category.Category{},
			Query:   query,
			Count:   0,
		}
	}

	var results []category.Category
	if profile == searchtypes.CustomizeProfile {
		results = []category.Category{}
	}

	for _, cat := range categories {
		categoryMatch := categoryMatchesInternal(cat, query)

		matchingSettings := findMatchingSettingsInternal(cat.Settings, query)

		if categoryMatch || len(matchingSettings) > 0 {
			relevanceScore := calculateRelevanceInternal(cat, matchingSettings, query, profile)

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

			if profile == searchtypes.CustomizeProfile {
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

	return searchtypes.Response{
		Results: results,
		Query:   query,
		Count:   len(results),
	}
}

func categoryMatchesInternal(cat category.Category, query string) bool {
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

func findMatchingSettingsInternal(settings []meta.Metadata, query string) []meta.Metadata {
	var matching []meta.Metadata
	for _, setting := range settings {
		if settingMatchesInternal(setting, query) {
			matching = append(matching, setting)
		}
	}
	return matching
}

func settingMatchesInternal(setting meta.Metadata, query string) bool {
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

func calculateRelevanceInternal(cat category.Category, matchingSettings []meta.Metadata, query string, profile searchtypes.Profile) int {
	score := 0

	if profile == searchtypes.CustomizeProfile && strings.ToLower(cat.Title) == query {
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
			if profile == searchtypes.CustomizeProfile {
				break
			}
		} else if strings.Contains(strings.ToLower(keyword), query) {
			score += 10
			if profile == searchtypes.CustomizeProfile {
				break
			}
		}
	}

	for _, setting := range matchingSettings {
		score += calculateSettingRelevanceInternal(setting, query, profile)
	}

	return score
}

func calculateSettingRelevanceInternal(setting meta.Metadata, query string, profile searchtypes.Profile) int {
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
			if profile == searchtypes.CustomizeProfile {
				break
			}
		} else if strings.Contains(strings.ToLower(keyword), query) {
			score += 5
			if profile == searchtypes.CustomizeProfile {
				break
			}
		}
	}

	return score
}
