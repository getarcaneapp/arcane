package pagination

import (
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
