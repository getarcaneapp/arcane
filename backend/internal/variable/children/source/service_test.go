package source

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/libtnb/sqlite"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"kit.getarcane.app/sys/crypto"

	"getarcane.dev/arcane/v3/internal/database"
	"getarcane.dev/arcane/v3/internal/environment"
	"getarcane.dev/types/v3"
)

func unavailableDocker(context.Context) (*client.Client, error) {
	return nil, errors.New("docker is not available in tests")
}

func passScope(_ context.Context, _ *database.DB, allEnvironments bool, envIDs []string) ([]string, error) {
	if allEnvironments {
		return nil, nil
	}
	return envIDs, nil
}

func serveSecrets(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/secrets" || r.Header.Get("Authorization") != "Bearer secret-token" {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if _, err := w.Write([]byte(`{"DB_PASSWORD": "pw", "bad-key": "x"}`)); err != nil {
		panic(err)
	}
}

func TestSecretSource_PresetFetchesKeysForScopedEnvironment(t *testing.T) {
	ctx := t.Context()
	db, err := gorm.Open(sqlite.Open("file:secret-source-test?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&SecretSource{}, &environment.Environment{}))
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	crypto.InitEncryption(crypto.Config{
		EncryptionKey: "test-encryption-key-for-testing-32bytes-min",
		Environment:   "test",
	})
	dbWrap := &database.DB{DB: db}
	require.NoError(t, dbWrap.Create(&environment.Environment{
		ID:      "1",
		Name:    "one",
		ApiUrl:  "http://one",
		Enabled: true,
	}).Error)
	server := httptest.NewServer(http.HandlerFunc(serveSecrets))
	t.Cleanup(server.Close)
	service := NewService(ServiceParams{
		DB:             dbWrap,
		DockerClient:   unavailableDocker,
		NormalizeScope: passScope,
	})

	created, err := service.Create(ctx, types.SecretSourceInput{
		Name:           "vault",
		PresetID:       "generic-http-json",
		Inputs:         map[string]string{"baseUrl": server.URL, "endpoint": "/secrets"},
		Credentials:    map[string]string{"token": "secret-token"},
		KeyPrefix:      "APP_",
		EnvironmentIDs: []string{"1"},
	}, map[string]bool{
		"name":           true,
		"presetId":       true,
		"inputs":         true,
		"credentials":    true,
		"keyPrefix":      true,
		"environmentIds": true,
	})
	require.NoError(t, err)
	require.Equal(t, []string{"token"}, created.CredentialKeys)
	require.Equal(t, 1, created.LastKeyCount)
	require.Equal(t, 1, created.LastSkippedKeyCount)

	var stored SecretSource
	require.NoError(t, db.First(&stored, "id = ?", created.ID).Error)
	require.NotContains(t, stored.Credentials, "secret-token")

	layers, err := service.EffectiveLayers(ctx, "1")
	require.NoError(t, err)
	require.Equal(t, [][]types.EnvVariable{{{
		Key:      "APP_DB_PASSWORD",
		Value:    "pw",
		NoInject: true,
	}}}, layers)

	layers, err = service.EffectiveLayers(ctx, "2")
	require.NoError(t, err)
	require.Empty(t, layers)
}
