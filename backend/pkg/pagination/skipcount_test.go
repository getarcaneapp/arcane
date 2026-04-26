package pagination

import (
	"testing"

	glsqlite "github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type widget struct {
	ID   string `gorm:"primaryKey"`
	Name string `sortable:"true"`
}

func newSkipCountTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(glsqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&widget{}))
	for i := range 5 {
		require.NoError(t, db.Create(&widget{ID: string(rune('a' + i)), Name: string(rune('A' + i))}).Error)
	}
	return db
}

func TestPaginateAndSortDB_SkipCountReturnsUnknownTotals(t *testing.T) {
	db := newSkipCountTestDB(t)

	var got []widget
	resp, err := PaginateAndSortDB(QueryParams{
		PaginationParams: PaginationParams{Start: 0, Limit: 2, SkipCount: true},
		SortParams:       SortParams{Sort: "Name", Order: SortOrder("asc")},
	}, db.Model(&widget{}), &got)
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Equal(t, UnknownTotal, resp.TotalItems)
	require.Equal(t, UnknownTotal, resp.TotalPages)
	require.Equal(t, 1, resp.CurrentPage)
	require.Equal(t, 2, resp.ItemsPerPage)
}

func TestPaginateAndSortDB_DefaultStillCounts(t *testing.T) {
	db := newSkipCountTestDB(t)

	var got []widget
	resp, err := PaginateAndSortDB(QueryParams{
		PaginationParams: PaginationParams{Start: 0, Limit: 2},
		SortParams:       SortParams{Sort: "Name", Order: SortOrder("asc")},
	}, db.Model(&widget{}), &got)
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Equal(t, int64(5), resp.TotalItems)
	require.Equal(t, int64(3), resp.TotalPages)
}

func TestPaginateAndSortDB_SkipCountShowAll(t *testing.T) {
	db := newSkipCountTestDB(t)

	var got []widget
	resp, err := PaginateAndSortDB(QueryParams{
		PaginationParams: PaginationParams{Start: 0, Limit: -1, SkipCount: true},
		SortParams:       SortParams{Sort: "Name", Order: SortOrder("asc")},
	}, db.Model(&widget{}), &got)
	require.NoError(t, err)
	require.Len(t, got, 5)
	require.Equal(t, UnknownTotal, resp.TotalItems)
	require.Equal(t, UnknownTotal, resp.TotalPages)
}
