package proxy

import (
	"fmt"
	"strings"
	"testing"

	"github.com/libtnb/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"kit.getarcane.app/concurrency"

	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/arcane/v3/internal/database"
)

type testEnvironmentRow struct {
	AccessToken *string
	ID          string `gorm:"primaryKey"`
	Name        string
	Enabled     bool
}

func (testEnvironmentRow) TableName() string { return "environments" }

func setupProxyTestService(t *testing.T, rows []testEnvironmentRow) *Service {
	t.Helper()

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.NewReplacer("/", "_", " ", "_").Replace(t.Name()))
	gormDB, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, gormDB.AutoMigrate(&testEnvironmentRow{}))
	require.NoError(t, gormDB.Create(&rows).Error)

	return New(&database.DB{DB: gormDB}, nil, &concurrency.SyncGate{})
}

func TestListEnabledRemoteEnvironmentIDs(t *testing.T) {
	service := setupProxyTestService(t, []testEnvironmentRow{
		{ID: "0", Name: "Local", Enabled: true, AccessToken: new("local-token")},
		{ID: "remote", Name: "Remote", Enabled: true, AccessToken: new("remote-token")},
		{ID: "disabled", Name: "Disabled", Enabled: false, AccessToken: new("disabled-token")},
		{ID: "tokenless", Name: "Tokenless", Enabled: true},
		{ID: "blank-token", Name: "Blank", Enabled: true, AccessToken: new("  ")},
	})

	ids, err := service.ListEnabledRemoteEnvironmentIDs(t.Context())

	require.NoError(t, err)
	require.Equal(t, []string{"remote"}, ids)
}

func TestTargetByID(t *testing.T) {
	service := setupProxyTestService(t, []testEnvironmentRow{
		{ID: "0", Name: "Local", Enabled: true},
		{ID: "remote", Name: "Remote", Enabled: true, AccessToken: new("remote-token")},
	})

	target, err := service.TargetByID(t.Context(), "remote")
	require.NoError(t, err)
	require.Equal(t, &Target{id: "remote", name: "Remote", accessToken: new("remote-token")}, target)

	_, err = service.TargetByID(t.Context(), "missing")
	require.ErrorIs(t, err, common.ErrNotFound)

	_, err = service.TargetByID(t.Context(), "0")
	require.EqualError(t, err, "cannot proxy request to local environment")
}
