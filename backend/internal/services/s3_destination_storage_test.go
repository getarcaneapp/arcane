package services

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	backuptypes "github.com/getarcaneapp/arcane/types/v2/backup"
	"github.com/stretchr/testify/require"
)

func TestS3DestinationService_TestS3DestinationRoundTrip(t *testing.T) {
	var object []byte
	var requestMethods []string
	expectedPathPrefix := "/arcane-backups/production/.arcane-connection-test-"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NotEmpty(t, r.Header.Get("Authorization"))
		require.True(t, strings.HasPrefix(r.URL.Path, expectedPathPrefix))
		requestMethods = append(requestMethods, r.Method)
		switch r.Method {
		case http.MethodPut:
			var err error
			object, err = io.ReadAll(r.Body)
			require.NoError(t, err)
			w.Header().Set("ETag", `"test-etag"`)
			w.WriteHeader(http.StatusOK)
		case http.MethodGet:
			w.Header().Set("Content-Length", strconv.Itoa(len(object)))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(object)
		case http.MethodDelete:
			object = nil
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "unsupported method", http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()

	service, _ := setupS3DestinationServiceTestInternal(t)
	require.NoError(t, service.TestS3DestinationConfiguration(context.Background(), backuptypes.CreateS3Destination{
		Name:            "Unsaved destination",
		Endpoint:        server.URL,
		Bucket:          "arcane-backups",
		Region:          "",
		AccessKeyID:     "access-key",
		SecretAccessKey: "secret-key",
		Prefix:          "production",
		UseSSL:          false,
		ForcePathStyle:  true,
	}))
	destination, err := service.CreateS3Destination(context.Background(), backuptypes.CreateS3Destination{
		Name:            "Test destination",
		Endpoint:        server.URL,
		Bucket:          "arcane-backups",
		Region:          "",
		AccessKeyID:     "access-key",
		SecretAccessKey: "secret-key",
		Prefix:          "production",
		UseSSL:          false,
		ForcePathStyle:  true,
	})
	require.NoError(t, err)

	require.NoError(t, service.TestS3Destination(context.Background(), destination.ID, nil))

	expectedPathPrefix = "/edited-bucket/edited/.arcane-connection-test-"
	require.NoError(t, service.TestS3Destination(context.Background(), destination.ID, &backuptypes.UpdateS3Destination{
		Name:           destination.Name,
		Endpoint:       server.URL,
		Bucket:         "edited-bucket",
		Region:         "",
		AccessKeyID:    destination.AccessKeyID,
		Prefix:         "edited",
		UseSSL:         false,
		ForcePathStyle: true,
	}))

	require.Equal(t, []string{
		http.MethodPut, http.MethodGet, http.MethodDelete,
		http.MethodPut, http.MethodGet, http.MethodDelete,
		http.MethodPut, http.MethodGet, http.MethodDelete,
	}, requestMethods)
	require.Empty(t, object)
	persisted, err := service.GetS3Destination(context.Background(), destination.ID)
	require.NoError(t, err)
	require.Equal(t, "arcane-backups", persisted.Bucket)
	require.Equal(t, "production", persisted.Prefix)
	require.Empty(t, persisted.Region)
}
