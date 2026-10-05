package francis

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/italypaleale/francis/actor"
	"github.com/italypaleale/francis/components/sqlite"
	"github.com/italypaleale/francis/host/local"
	"github.com/stretchr/testify/require"
)

func TestMigrateLegacyStore(t *testing.T) {
	mainPath := filepath.Join(t.TempDir(), "arcane.db")
	databaseURL := "file:" + mainPath + "?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_txlock=immediate"
	storeURL, err := StoreURL(databaseURL)
	require.NoError(t, err)
	require.Equal(t, "file:"+filepath.Join(filepath.Dir(mainPath), "arcane.francis.db"), storeURL)
	for input, expected := range map[string]string{
		"file:/data/arcane%23prod.db?_txlock=immediate":      "file:/data/arcane%23prod.francis.db",
		"file:/data/arcane%2Edb":                             "file:/data/arcane.francis.db",
		"file:data/arcane%23prod%2Edb":                       "file:data/arcane%23prod.francis.db",
		"file:data/arcane":                                   "file:data/arcane.francis.db",
		"postgres://arcane@localhost/arcane?sslmode=disable": "postgres://arcane@localhost/arcane?sslmode=disable",
	} {
		storeURL, err = StoreURL(input)
		require.NoError(t, err)
		require.Equal(t, expected, storeURL)
	}
	for _, input := range []string{"file::memory:", "file:shared?mode=memory&cache=shared", "mysql://arcane"} {
		_, err = StoreURL(input)
		require.Error(t, err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	start := func(options ...local.HostOption) *Runtime {
		runtime, newErr := New(databaseURL, "test-encryption-key", "test-instance", freePortInternal(t), options...)
		require.NoError(t, newErr)
		require.NoError(t, runtime.RegisterActor("test", func(string, *actor.Service) actor.Actor { return struct{}{} }))
		require.NoError(t, runtime.Start(ctx, ctx, nil))
		return runtime
	}
	legacy := start(local.WithSQLiteProvider(sqlite.SQLiteProviderOptions{ConnectionString: "file:" + mainPath, TablePrefix: TablePrefix}))
	require.NoError(t, legacy.Service().SetState(ctx, "test", "durable", "kept", nil))
	require.NoError(t, legacy.Stop(ctx))

	mainDB, err := sql.Open("sqlite", mainPath)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, mainDB.Close()) })
	for range 2 {
		require.NoError(t, MigrateLegacyStore(ctx, mainDB, databaseURL))
	}
	var tables int
	require.NoError(t, mainDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE name LIKE ?", TablePrefix+"_%").Scan(&tables))
	require.Zero(t, tables)

	migrated := start()
	t.Cleanup(func() { require.NoError(t, migrated.Stop(context.WithoutCancel(ctx))) })
	var state string
	require.NoError(t, migrated.Service().GetState(ctx, "test", "durable", &state))
	require.Equal(t, "kept", state)
}
