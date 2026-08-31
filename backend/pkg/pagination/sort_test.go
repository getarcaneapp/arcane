package pagination

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSortFunctionUsesCustomDescendingComparatorWhenProvided(t *testing.T) {
	items := []int{2, 1, 0}

	sorted := sortFunction(items, SortParams{Sort: "ports", Order: SortDesc}, []SortBinding[int]{
		{
			Key: "ports",
			Fn: func(a, b int) int {
				switch {
				case a < b:
					return -1
				case a > b:
					return 1
				default:
					return 0
				}
			},
			DescFn: func(a, b int) int {
				aIsEmpty := a == 0
				bIsEmpty := b == 0
				switch {
				case aIsEmpty && bIsEmpty:
					return 0
				case aIsEmpty:
					return 1
				case bIsEmpty:
					return -1
				case a > b:
					return -1
				case a < b:
					return 1
				default:
					return 0
				}
			},
		},
	})

	require.Equal(t, []int{2, 1, 0}, sorted)
}

type sortItem struct {
	ID    string
	Group string
}

func sortItemBindings() []SortBinding[sortItem] {
	return []SortBinding[sortItem]{
		{
			Key: "id",
			Fn:  func(a, b sortItem) int { return strings.Compare(a.ID, b.ID) },
		},
		{
			Key: "group",
			Fn:  func(a, b sortItem) int { return strings.Compare(a.Group, b.Group) },
		},
	}
}

func sortItemIDs(items []sortItem) []string {
	ids := make([]string, len(items))
	for i, item := range items {
		ids[i] = item.ID
	}
	return ids
}

func TestSortFunctionFallsBackToFirstBindingWhenSortEmpty(t *testing.T) {
	items := []sortItem{{ID: "b"}, {ID: "c"}, {ID: "a"}}

	sorted := sortFunction(items, SortParams{}, sortItemBindings())

	require.Equal(t, []string{"a", "b", "c"}, sortItemIDs(sorted))
}

func TestSortFunctionFallsBackToFirstBindingWhenSortUnknown(t *testing.T) {
	items := []sortItem{{ID: "b"}, {ID: "c"}, {ID: "a"}}

	sorted := sortFunction(items, SortParams{Sort: "bogus"}, sortItemBindings())

	require.Equal(t, []string{"a", "b", "c"}, sortItemIDs(sorted))
}

func TestSortFunctionHonoursDescendingOrderOnFallback(t *testing.T) {
	items := []sortItem{{ID: "b"}, {ID: "c"}, {ID: "a"}}

	sorted := sortFunction(items, SortParams{Order: SortDesc}, sortItemBindings())

	require.Equal(t, []string{"c", "b", "a"}, sortItemIDs(sorted))
}

func TestSortFunctionBreaksTiesWithFirstBinding(t *testing.T) {
	items := []sortItem{
		{ID: "d", Group: "y"},
		{ID: "b", Group: "x"},
		{ID: "c", Group: "y"},
		{ID: "a", Group: "x"},
	}

	sorted := sortFunction(items, SortParams{Sort: "group"}, sortItemBindings())
	require.Equal(t, []string{"a", "b", "c", "d"}, sortItemIDs(sorted))

	// Descending reverses the requested key only; the tie-break stays ascending.
	sorted = sortFunction(items, SortParams{Sort: "group", Order: SortDesc}, sortItemBindings())
	require.Equal(t, []string{"c", "d", "a", "b"}, sortItemIDs(sorted))
}

func TestSortFunctionWithoutBindingsReturnsItemsUnchanged(t *testing.T) {
	items := []sortItem{{ID: "b"}, {ID: "a"}}

	sorted := sortFunction(items, SortParams{Sort: "id"}, nil)

	require.Equal(t, []string{"b", "a"}, sortItemIDs(sorted))
}

// A page walk must return every item exactly once even when the source order
// changes between requests, as Docker's map-iteration order does for volumes
// and networks — both with no sort param and with a low-cardinality sort key.
func TestSearchOrderAndPaginateStablePageWalkAcrossReorderedInput(t *testing.T) {
	source := make([]sortItem, 0, 25)
	for i := range 25 {
		source = append(source, sortItem{ID: fmt.Sprintf("%02d", i), Group: fmt.Sprintf("g%d", i%3)})
	}
	config := Config[sortItem]{SortBindings: sortItemBindings()}

	for _, sort := range []string{"", "group"} {
		t.Run("sort="+sort, func(t *testing.T) {
			r := rand.New(rand.NewPCG(7, 11))
			seen := map[string]int{}
			const limit = 4
			for start := 0; start < len(source); start += limit {
				shuffled := slices.Clone(source)
				r.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })

				result := config.SearchOrderAndPaginate(shuffled, QueryParams{
					Sort:  sort,
					Start: start, Limit: limit,
				})
				for _, item := range result.Items {
					seen[item.ID]++
				}
			}

			require.Len(t, seen, len(source), "every item must appear in the walk")
			for id, count := range seen {
				require.Equal(t, 1, count, "item %s must appear exactly once", id)
			}
		})
	}
}
