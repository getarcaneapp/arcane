package storage

import (
	"context"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.etcd.io/etcd/server/v3/embed"

	"github.com/getarcaneapp/arcane/backend/internal/config"
	"github.com/getarcaneapp/arcane/backend/internal/database"
	"github.com/getarcaneapp/arcane/backend/internal/models"
)

func TestEtcdMirrorStoreImportAndHydrate(t *testing.T) {
	etcd, endpoint := startEmbeddedEtcd(t)

	sourceDSN := "file:" + filepath.Join(t.TempDir(), "source.db")
	sourceDB, err := database.Initialize(context.Background(), sourceDSN, database.MigrationOptions{})
	require.NoError(t, err)
	defer func() { _ = sourceDB.Close() }()

	now := time.Now().UTC()
	require.NoError(t, sourceDB.Create(&models.SettingVariable{Key: "instanceId", Value: "test-instance"}).Error)
	require.NoError(t, sourceDB.Create(&models.User{
		BaseModel: models.BaseModel{
			ID:        "user-1",
			CreatedAt: now,
		},
		Username:     "arcane",
		PasswordHash: "hash",
		Roles:        models.StringSlice{"admin"},
	}).Error)

	cfg := &config.Config{
		StorageBackend:         "etcd",
		EtcdEndpoints:          endpoint,
		EtcdNamespace:          "arcane-test",
		EtcdDialTimeout:        5 * time.Second,
		EtcdRequestTimeout:     5 * time.Second,
		EtcdImportFromDatabase: true,
	}

	store, err := NewEtcdMirrorStore(context.Background(), cfg)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	require.NoError(t, store.EnsureMetadata(context.Background()))
	require.NoError(t, store.ImportFromSQL(context.Background(), sourceDSN, false))

	mirrorDB, err := database.Initialize(context.Background(), "file::memory:?cache=shared", database.MigrationOptions{})
	require.NoError(t, err)
	defer func() { _ = mirrorDB.Close() }()

	require.NoError(t, store.HydrateMirror(context.Background(), mirrorDB))

	var user models.User
	require.NoError(t, mirrorDB.Where("id = ?", "user-1").First(&user).Error)
	require.Equal(t, "arcane", user.Username)

	_ = etcd
}

func TestInitializeRuntimeStorageEtcdDoesNotUseSQLSourceWithoutImport(t *testing.T) {
	etcd, endpoint := startEmbeddedEtcd(t)

	cfg := &config.Config{
		StorageBackend:         "etcd",
		DatabaseURL:            "postgres://definitely-invalid-import-source",
		EtcdEndpoints:          endpoint,
		EtcdNamespace:          "arcane-no-import",
		EtcdDialTimeout:        5 * time.Second,
		EtcdRequestTimeout:     5 * time.Second,
		EtcdImportFromDatabase: false,
	}

	runtimeStorage, err := InitializeRuntimeStorage(context.Background(), cfg)
	require.NoError(t, err)
	defer func() { _ = runtimeStorage.Close() }()

	require.Equal(t, "etcd", runtimeStorage.Backend)
	require.NotNil(t, runtimeStorage.DB)

	_ = etcd
}

func TestEtcdMirrorStoreImportRefusesReimportIntoInitializedNamespace(t *testing.T) {
	etcd, endpoint := startEmbeddedEtcd(t)

	sourceDSN := "file:" + filepath.Join(t.TempDir(), "source.db")
	sourceDB, err := database.Initialize(context.Background(), sourceDSN, database.MigrationOptions{})
	require.NoError(t, err)
	defer func() { _ = sourceDB.Close() }()

	require.NoError(t, sourceDB.Create(&models.SettingVariable{Key: "instanceId", Value: "test-instance"}).Error)

	cfg := &config.Config{
		StorageBackend:     "etcd",
		EtcdEndpoints:      endpoint,
		EtcdNamespace:      "arcane-reimport",
		EtcdDialTimeout:    5 * time.Second,
		EtcdRequestTimeout: 5 * time.Second,
	}

	store, err := NewEtcdMirrorStore(context.Background(), cfg)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	require.NoError(t, store.EnsureMetadata(context.Background()))
	require.NoError(t, store.ImportFromSQL(context.Background(), sourceDSN, false))

	err = store.ImportFromSQL(context.Background(), sourceDSN, false)
	require.Error(t, err)
	require.Contains(t, err.Error(), "refusing to re-import")

	_ = etcd
}

func startEmbeddedEtcd(t *testing.T) (*embed.Etcd, string) {
	t.Helper()

	cfg := embed.NewConfig()
	cfg.Dir = filepath.Join(t.TempDir(), "etcd")

	clientURL, err := url.Parse("http://127.0.0.1:0")
	require.NoError(t, err)
	peerURL, err := url.Parse("http://127.0.0.1:0")
	require.NoError(t, err)

	cfg.ListenClientUrls = []url.URL{*clientURL}
	cfg.AdvertiseClientUrls = []url.URL{*clientURL}
	cfg.ListenPeerUrls = []url.URL{*peerURL}
	cfg.AdvertisePeerUrls = []url.URL{*peerURL}
	cfg.InitialCluster = cfg.InitialClusterFromName(cfg.Name)

	etcd, err := embed.StartEtcd(cfg)
	require.NoError(t, err)

	select {
	case <-etcd.Server.ReadyNotify():
	case <-time.After(20 * time.Second):
		etcd.Server.Stop()
		t.Fatalf("embedded etcd did not start")
	}

	t.Cleanup(func() {
		etcd.Close()
	})

	return etcd, etcd.Clients[0].Addr().String()
}
