package search

import (
	"cmp"
	"reflect"
	"slices"
	"strings"

	"kit.getarcane.app/normalization"

	"getarcane.dev/types/v3"
)

func BuildCategories[T any]() []types.Category {
	catMetaMap := normalization.ExtractCategoryMetadata[T]()

	categories := map[string][]types.SettingMetadata{}
	var categoryOrder []string

	rt := reflect.TypeFor[T]()
	for field := range rt.Fields() {
		keyTag := field.Tag.Get("key")
		key, _, _ := strings.Cut(keyTag, ",")
		if key == "" {
			continue
		}

		metaTag := normalization.ParseMetaTag(field.Tag.Get("meta"))
		label := cmp.Or(metaTag["label"], key)
		typ := cmp.Or(metaTag["type"], "text")
		desc := metaTag["description"]
		keywords := normalization.ParseKeywords(metaTag["keywords"])
		categoryID := cmp.Or(metaTag["category"], normalization.ParseMetaTag(field.Tag.Get("catmeta"))["id"], "jobs")
		if categoryID == "internal" {
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

	var results []types.Category
	for _, catID := range categoryOrder {
		catMeta := catMetaMap[catID]
		if catMeta == nil {
			continue
		}

		results = append(results, types.Category{
			ID:          catMeta["id"],
			Title:       catMeta["title"],
			Description: catMeta["description"],
			Icon:        catMeta["icon"],
			URL:         catMeta["url"],
			Keywords:    normalization.ParseKeywords(catMeta["keywords"]),
			Settings:    categories[catID],
		})
	}

	return results
}

func Search(categories []types.Category, query string) types.SearchResponse {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return types.SearchResponse{Query: query}
	}

	var results []types.Category

	for _, cat := range categories {
		categoryMatch := matchesAny(query, cat.Title, cat.Description) || matchesAny(query, cat.Keywords...)
		matchingSettings := findMatchingSettings(cat.Settings, query)

		if categoryMatch || len(matchingSettings) > 0 {
			results = append(results, types.Category{
				ID:               cat.ID,
				Title:            cat.Title,
				Description:      cat.Description,
				Icon:             cat.Icon,
				URL:              cat.URL,
				Keywords:         cat.Keywords,
				Settings:         cat.Settings,
				RelevanceScore:   calculateRelevance(cat, matchingSettings, query),
				MatchingSettings: matchingSettings,
			})
		}
	}

	slices.SortFunc(results, func(a, b types.Category) int {
		return cmp.Compare(b.RelevanceScore, a.RelevanceScore)
	})

	return types.SearchResponse{
		Results: results,
		Query:   query,
		Count:   len(results),
	}
}

func matchesAny(query string, values ...string) bool {
	return slices.ContainsFunc(values, func(v string) bool {
		return strings.Contains(strings.ToLower(v), query)
	})
}

func findMatchingSettings(settings []types.SettingMetadata, query string) []types.SettingMetadata {
	var matching []types.SettingMetadata
	for _, setting := range settings {
		if matchesAny(query, setting.Key, setting.Label, setting.Description) || matchesAny(query, setting.Keywords...) {
			matching = append(matching, setting)
		}
	}
	return matching
}

func calculateRelevance(cat types.Category, matchingSettings []types.SettingMetadata, query string) int {
	score := 0

	if strings.Contains(strings.ToLower(cat.Title), query) {
		score += 20
	}
	if strings.Contains(strings.ToLower(cat.Description), query) {
		score += 15
	}

	for _, keyword := range cat.Keywords {
		if strings.ToLower(keyword) == query {
			score += 25
		} else if strings.Contains(strings.ToLower(keyword), query) {
			score += 10
		}
	}

	for _, setting := range matchingSettings {
		score += calculateSettingRelevance(setting, query)
	}

	return score
}

func calculateSettingRelevance(setting types.SettingMetadata, query string) int {
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
		} else if strings.Contains(strings.ToLower(keyword), query) {
			score += 5
		}
	}

	return score
}
