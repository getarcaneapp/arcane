package handlerutil

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/stretchr/testify/require"
)

func TestDownloadResponseContentLength(t *testing.T) {
	tests := []struct {
		name          string
		size          int64
		wantLengthSet bool
	}{
		{name: "known size sets Content-Length", size: 5, wantLengthSet: true},
		{name: "unknown size omits Content-Length", size: -1, wantLengthSet: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, api := humatest.New(t)
			huma.Get(api, "/download", func(ctx context.Context, _ *struct{}) (*huma.StreamResponse, error) {
				return DownloadResponse(io.NopCloser(strings.NewReader("hello")), tt.size, "out.log"), nil
			})

			resp := api.Get("/download")
			require.Equal(t, http.StatusOK, resp.Code)
			require.Equal(t, "hello", resp.Body.String())
			require.Equal(t, "attachment; filename=out.log", resp.Header().Get("Content-Disposition"))
			require.Equal(t, "application/octet-stream", resp.Header().Get("Content-Type"))
			_, lengthSet := resp.Header()["Content-Length"]
			require.Equal(t, tt.wantLengthSet, lengthSet)
		})
	}
}
