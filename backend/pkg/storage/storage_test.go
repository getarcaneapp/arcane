package storage

import (
	"context"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.etcd.io/etcd/server/v3/embed"

	"github.com/getarcaneapp/arcane/backend/internal/database"
)

func TestOpenSQLStoreReadWrite(t *testing.T) {
	db, err := database.Initialize(context.Background(), "file:"+filepath.Join(t.TempDir(), "storage.db"), database.MigrationOptions{})
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	store, err := Open(context.Background(), Config{
		Backend:   BackendSQL,
		SQLDB:     db,
		Namespace: "arcane-sql",
	})
	require.NoError(t, err)

	require.NoError(t, store.PutMeta(context.Background(), "storage-version", "1"))
	version, err := store.GetMeta(context.Background(), "storage-version")
	require.NoError(t, err)
	require.Equal(t, "1", version)

	require.NoError(t, store.PutRecord(context.Background(), "users", "u1", []byte(`{"id":"u1"}`)))

	record, err := store.GetRecord(context.Background(), "users", "u1")
	require.NoError(t, err)
	require.NotNil(t, record)
	require.Equal(t, []byte(`{"id":"u1"}`), record.Value)

	records, err := store.ListRecords(context.Background(), "users")
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.Equal(t, "u1", records[0].Key)

	require.NoError(t, store.DeleteRecord(context.Background(), "users", "u1"))
	record, err = store.GetRecord(context.Background(), "users", "u1")
	require.NoError(t, err)
	require.Nil(t, record)
}

func TestOpenEtcdStoreReadWrite(t *testing.T) {
	etcd, endpoint := startEmbeddedEtcd(t)
	_ = etcd

	store, err := Open(context.Background(), Config{
		Backend:            BackendEtcd,
		EtcdEndpoints:      []string{endpoint},
		Namespace:          "arcane-etcd",
		RequestTimeout:     5 * time.Second,
		EtcdDialTimeout:    5 * time.Second,
		MaxInlineValueSize: 8,
	})
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	require.NoError(t, store.PutMeta(context.Background(), "storage-version", "1"))
	version, err := store.GetMeta(context.Background(), "storage-version")
	require.NoError(t, err)
	require.Equal(t, "1", version)

	largeValue := []byte(`{"id":"u1","payload":"abcdefghijk"}`)
	require.NoError(t, store.PutRecord(context.Background(), "users", "u1", largeValue))

	record, err := store.GetRecord(context.Background(), "users", "u1")
	require.NoError(t, err)
	require.NotNil(t, record)
	require.Equal(t, largeValue, record.Value)

	records, err := store.ListRecords(context.Background(), "users")
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.Equal(t, "u1", records[0].Key)

	require.NoError(t, store.DeleteRecord(context.Background(), "users", "u1"))
	record, err = store.GetRecord(context.Background(), "users", "u1")
	require.NoError(t, err)
	require.Nil(t, record)
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
