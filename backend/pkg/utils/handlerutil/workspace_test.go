package handlerutil

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/stretchr/testify/require"
)

type workspaceUploadFixture struct {
	name    string
	content []byte
}

func workspaceMultipartForm(t *testing.T, values map[string][]string, files []workspaceUploadFixture) multipart.Form {
	t.Helper()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for name, entries := range values {
		for _, value := range entries {
			require.NoError(t, writer.WriteField(name, value))
		}
	}
	for _, fixture := range files {
		part, err := writer.CreateFormFile("files", fixture.name)
		require.NoError(t, err)
		_, err = part.Write(fixture.content)
		require.NoError(t, err)
	}
	require.NoError(t, writer.Close())

	request, err := http.NewRequest(http.MethodPut, "/workspace", &body)
	require.NoError(t, err)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	require.NoError(t, request.ParseMultipartForm(int64(body.Len())))
	t.Cleanup(func() { _ = request.MultipartForm.RemoveAll() })
	return *request.MultipartForm
}

func requireWorkspaceHTTPStatus(t *testing.T, err error, expected int) {
	t.Helper()
	var statusErr huma.StatusError
	require.ErrorAs(t, err, &statusErr)
	require.Equal(t, expected, statusErr.GetStatus())
}

func TestParseWorkspaceJSONPartInternalRequiresExactlyOnePart(t *testing.T) {
	type manifest struct {
		Revision string `json:"revision"`
	}

	_, err := ParseMultipartJSONPart[manifest](workspaceMultipartForm(t, nil, nil), "manifest")
	requireWorkspaceHTTPStatus(t, err, http.StatusBadRequest)

	form := workspaceMultipartForm(t, map[string][]string{"manifest": {`{"revision":"one"}`, `{"revision":"two"}`}}, nil)
	_, err = ParseMultipartJSONPart[manifest](form, "manifest")
	requireWorkspaceHTTPStatus(t, err, http.StatusBadRequest)

	form = workspaceMultipartForm(t, map[string][]string{"manifest": {`{"revision":"current"}`}}, nil)
	parsed, err := ParseMultipartJSONPart[manifest](form, "manifest")
	require.NoError(t, err)
	require.Equal(t, "current", parsed.Revision)
}

func TestReadWorkspaceUploads(t *testing.T) {
	form := workspaceMultipartForm(t, nil, []workspaceUploadFixture{
		{name: "one.txt", content: []byte("one")},
		{name: "two.txt", content: []byte("two")},
	})
	uploads, err := ReadWorkspaceUploads(form, 16)
	require.NoError(t, err)
	require.Equal(t, []byte("one"), uploads[0])
	require.Equal(t, []byte("two"), uploads[1])
}

func TestReadWorkspaceUploadsInternalAcceptsAnyContentAndEnforcesLimit(t *testing.T) {
	tests := []struct {
		name     string
		content  []byte
		maxBytes int64
		status   int
	}{
		{name: "invalid UTF-8", content: []byte{0xff}, maxBytes: 16},
		{name: "NUL byte", content: []byte{'a', 0, 'b'}, maxBytes: 16},
		{name: "configured size", content: []byte("four"), maxBytes: 3, status: http.StatusRequestEntityTooLarge},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			form := workspaceMultipartForm(t, nil, []workspaceUploadFixture{{name: "upload.bin", content: tt.content}})
			uploads, err := ReadWorkspaceUploads(form, tt.maxBytes)
			if tt.status != 0 {
				requireWorkspaceHTTPStatus(t, err, tt.status)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.content, uploads[0])
		})
	}
}
