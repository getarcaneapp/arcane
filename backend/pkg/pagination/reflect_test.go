package pagination

import (
	"testing"

	"github.com/libtnb/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Widgets with duplicate names, inserted in neither id nor name order, so that
// only a deterministic ORDER BY yields a stable page walk.
func newTieBreakTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&widget{}))
	for _, w := range []widget{
		{ID: "c", Name: "X"},
		{ID: "a", Name: "X"},
		{ID: "e", Name: "Y"},
		{ID: "b", Name: "X"},
		{ID: "d", Name: "Y"},
	} {
		require.NoError(t, db.Create(&w).Error)
	}
	return db
}

func walkWidgetPages(t *testing.T, db *gorm.DB, sort string) []string {
	t.Helper()
	var ids []string
	for start := 0; start < 5; start += 2 {
		var got []widget
		_, err := PaginateAndSortDB(QueryParams{
			Start: start, Limit: 2,
			Sort: sort, Order: "asc",
		}, db.Model(&widget{}), &got)
		require.NoError(t, err)
		for _, w := range got {
			ids = append(ids, w.ID)
		}
	}
	return ids
}

func TestPaginateAndSortDB_NoSortFallsBackToIDOrder(t *testing.T) {
	db := newTieBreakTestDB(t)

	require.Equal(t, []string{"a", "b", "c", "d", "e"}, walkWidgetPages(t, db, ""))
}

func TestPaginateAndSortDB_SortTiesBrokenByID(t *testing.T) {
	db := newTieBreakTestDB(t)

	require.Equal(t, []string{"a", "b", "c", "d", "e"}, walkWidgetPages(t, db, "name"))
}
