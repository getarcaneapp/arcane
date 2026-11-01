package sqlite

import (
	"errors"
	"testing"

	"github.com/libtnb/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type (
	widget struct {
		ID   string `gorm:"primaryKey"`
		Name string
	}
)

var errWidgetNotFound = errors.New("widget not found")

func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&widget{}))
	return db
}

func TestFirstWhereFound(t *testing.T) {
	db := newTestDB(t)
	require.NoError(t, db.Create(&widget{ID: "w-1", Name: "alpha"}).Error)

	got, err := FirstWhere[widget](t.Context(), db, errWidgetNotFound, "name = ?", "alpha")
	require.NoError(t, err)
	require.Equal(t, "w-1", got.ID)
}

func TestFirstWhereNotFoundReturnsSentinel(t *testing.T) {
	db := newTestDB(t)

	_, err := FirstWhere[widget](t.Context(), db, errWidgetNotFound, "name = ?", "missing")
	require.ErrorIs(t, err, errWidgetNotFound)
}

func TestFirstWhereNotFoundWithNilSentinel(t *testing.T) {
	db := newTestDB(t)

	_, err := FirstWhere[widget](t.Context(), db, nil, "name = ?", "missing")
	require.Error(t, err)
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
}
