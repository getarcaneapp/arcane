package pagination

import (
	"cmp"
	"testing"

	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/stretchr/testify/require"
)

type environmentRow struct {
	base.EnvironmentTag

	Name string
}

func environmentRowConfig() Config[environmentRow] {
	return Config[environmentRow]{SortBindings: []SortBinding[environmentRow]{
		{Key: "name", Fn: func(a, b environmentRow) int { return cmp.Compare(a.Name, b.Name) }},
	}}
}

func environmentRowPages() []EnvironmentPage[environmentRow] {
	return []EnvironmentPage[environmentRow]{
		{Environment: base.EnvironmentRef{ID: "b", Name: "Beta"}, Items: []environmentRow{{Name: "x"}, {Name: "a"}}, TotalAvailable: 5},
		{Environment: base.EnvironmentRef{ID: "a", Name: "Alpha"}, Items: []environmentRow{{Name: "x"}, {Name: "c"}}, TotalAvailable: 3},
	}
}

func environmentRowLabels(rows []environmentRow) []string {
	labels := make([]string, 0, len(rows))
	for _, row := range rows {
		labels = append(labels, row.EnvironmentID+"/"+row.Name)
	}
	return labels
}

func TestMergeEnvironmentPagesBreaksTiesByEnvironment(t *testing.T) {
	rows, response := MergeEnvironmentPages(environmentRowConfig(), environmentRowPages(), QueryParams{SortParams: SortParams{Sort: "name", Order: SortAsc}})

	require.Equal(t, []string{"b/a", "a/c", "a/x", "b/x"}, environmentRowLabels(rows))
	require.Equal(t, "Alpha", rows[1].EnvironmentName)
	require.Equal(t, int64(4), response.TotalItems)
	require.Equal(t, int64(8), response.GrandTotalItems)
}

func TestMergeEnvironmentPagesSortsByEnvironment(t *testing.T) {
	ascending, _ := MergeEnvironmentPages(environmentRowConfig(), environmentRowPages(), QueryParams{SortParams: SortParams{Sort: "environment", Order: SortAsc}})
	descending, _ := MergeEnvironmentPages(environmentRowConfig(), environmentRowPages(), QueryParams{SortParams: SortParams{Sort: "environment", Order: SortDesc}})

	require.Equal(t, []string{"a/c", "a/x", "b/a", "b/x"}, environmentRowLabels(ascending))
	require.Equal(t, []string{"b/a", "b/x", "a/c", "a/x"}, environmentRowLabels(descending))
}

func TestMergeEnvironmentPagesSlicesPage(t *testing.T) {
	rows, response := MergeEnvironmentPages(environmentRowConfig(), environmentRowPages(), QueryParams{
		SortParams: SortParams{Sort: "environment", Order: SortAsc},
		Params:     Params{Start: 2, Limit: 2},
	})

	require.Equal(t, []string{"b/a", "b/x"}, environmentRowLabels(rows))
	require.Equal(t, int64(2), response.TotalPages)
	require.Equal(t, 2, response.CurrentPage)
}

func TestWithEnvironmentLeavesInputConfigUnchanged(t *testing.T) {
	config := environmentRowConfig()

	merged := WithEnvironment(config)

	require.Len(t, config.SortBindings, 1)
	require.Len(t, merged.SortBindings, 2)
	require.Equal(t, "environment", merged.SortBindings[1].Key)
}
