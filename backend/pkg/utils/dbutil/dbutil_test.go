package dbutil

import (
	"context"
	"errors"
	"testing"

	glsqlite "github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type widget struct {
	ID   string `gorm:"primaryKey"`
	Name string
}

var errWidgetNotFound = errors.New("widget not found")

func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(glsqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&widget{}))
	return db
}

func TestFirstWhereFound(t *testing.T) {
	db := newTestDB(t)
	require.NoError(t, db.Create(&widget{ID: "w-1", Name: "alpha"}).Error)

	got, err := FirstWhere[widget](context.Background(), db, errWidgetNotFound, "name = ?", "alpha")
	require.NoError(t, err)
	require.Equal(t, "w-1", got.ID)
}

func TestFirstWhereNotFoundReturnsSentinel(t *testing.T) {
	db := newTestDB(t)

	_, err := FirstWhere[widget](context.Background(), db, errWidgetNotFound, "name = ?", "missing")
	require.ErrorIs(t, err, errWidgetNotFound)
}

func TestFirstWhereNotFoundWithNilSentinel(t *testing.T) {
	db := newTestDB(t)

	_, err := FirstWhere[widget](context.Background(), db, nil, "name = ?", "missing")
	require.Error(t, err)
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
}

func TestWithTxCommit(t *testing.T) {
	db := newTestDB(t)

	err := WithTx(context.Background(), db, func(tx *gorm.DB) error {
		return tx.Create(&widget{ID: "w-1", Name: "alpha"}).Error
	})
	require.NoError(t, err)

	got, err := FirstWhere[widget](context.Background(), db, errWidgetNotFound, "id = ?", "w-1")
	require.NoError(t, err)
	require.Equal(t, "alpha", got.Name)
}

func TestWithTxRollback(t *testing.T) {
	db := newTestDB(t)
	boom := errors.New("boom")

	err := WithTx(context.Background(), db, func(tx *gorm.DB) error {
		if err := tx.Create(&widget{ID: "w-1", Name: "alpha"}).Error; err != nil {
			return err
		}
		return boom
	})
	require.ErrorIs(t, err, boom)

	_, err = FirstWhere[widget](context.Background(), db, errWidgetNotFound, "id = ?", "w-1")
	require.ErrorIs(t, err, errWidgetNotFound)
}
