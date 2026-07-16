package database

import (
	"context"
	"errors"
	"testing"

	sqlite "github.com/libtnb/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type widget struct {
	ID   string `gorm:"primaryKey"`
	Name string
}

var errWidgetNotFound = errors.New("widget not found")

func newTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&widget{}))
	return &DB{DB: db}
}

func TestFirstFound(t *testing.T) {
	db := newTestDB(t)
	require.NoError(t, db.Create(context.Background(), &widget{ID: "w-1", Name: "alpha"}))

	got, err := db.First[widget](context.Background(), errWidgetNotFound, "name = ?", "alpha")
	require.NoError(t, err)
	require.Equal(t, "w-1", got.ID)
}

func TestFirstNotFoundReturnsSentinel(t *testing.T) {
	db := newTestDB(t)

	_, err := db.First[widget](context.Background(), errWidgetNotFound, "name = ?", "missing")
	require.ErrorIs(t, err, errWidgetNotFound)
}

func TestFirstNotFoundWithNilSentinel(t *testing.T) {
	db := newTestDB(t)

	_, err := db.First[widget](context.Background(), nil, "name = ?", "missing")
	require.Error(t, err)
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
}

func TestWithTxCommit(t *testing.T) {
	db := newTestDB(t)

	err := db.WithTx(context.Background(), func(tx *gorm.DB) error {
		return tx.Create(&widget{ID: "w-1", Name: "alpha"}).Error
	})
	require.NoError(t, err)

	got, err := db.First[widget](context.Background(), errWidgetNotFound, "id = ?", "w-1")
	require.NoError(t, err)
	require.Equal(t, "alpha", got.Name)
}

func TestWithTxRollback(t *testing.T) {
	db := newTestDB(t)
	boom := errors.New("boom")

	err := db.WithTx(context.Background(), func(tx *gorm.DB) error {
		if err := tx.Create(&widget{ID: "w-1", Name: "alpha"}).Error; err != nil {
			return err
		}
		return boom
	})
	require.ErrorIs(t, err, boom)

	_, err = db.First[widget](context.Background(), errWidgetNotFound, "id = ?", "w-1")
	require.ErrorIs(t, err, errWidgetNotFound)
}
