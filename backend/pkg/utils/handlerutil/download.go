package handlerutil

import (
	"io"
	"strconv"

	"github.com/danielgtaylor/huma/v2"
)

// DownloadResponse streams an attachment and closes its reader when streaming ends.
func DownloadResponse(reader io.ReadCloser, size int64, filename string) *huma.StreamResponse {
	return &huma.StreamResponse{Body: func(ctx huma.Context) {
		defer func() { _ = reader.Close() }()
		ctx.SetHeader("Content-Type", "application/octet-stream")
		ctx.SetHeader("Content-Disposition", "attachment; filename="+filename)
		ctx.SetHeader("Content-Length", strconv.FormatInt(size, 10))
		_, _ = io.Copy(ctx.BodyWriter(), reader)
	}}
}
