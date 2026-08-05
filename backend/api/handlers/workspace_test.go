package handlers

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"testing"

	"emperror.dev/errors"
	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/stretchr/testify/require"
)

type workspaceUploadFixtureInternal struct {
	name    string
	content []byte
}

func workspaceMultipartFormInternal(t *testing.T, values map[string][]string, files []workspaceUploadFixtureInternal) multipart.Form {
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

func requireWorkspaceHTTPStatusInternal(t *testing.T, err error, expected int) {
	t.Helper()
	var statusErr huma.StatusError
	require.ErrorAs(t, err, &statusErr)
	require.Equal(t, expected, statusErr.GetStatus())
}

func TestParseWorkspaceJSONPartInternalRequiresExactlyOnePart(t *testing.T) {
	type manifest struct {
		Revision string `json:"revision"`
	}

	_, err := parseWorkspaceJSONPartInternal[manifest](workspaceMultipartFormInternal(t, nil, nil), "manifest")
	requireWorkspaceHTTPStatusInternal(t, err, http.StatusBadRequest)

	form := workspaceMultipartFormInternal(t, map[string][]string{"manifest": {`{"revision":"one"}`, `{"revision":"two"}`}}, nil)
	_, err = parseWorkspaceJSONPartInternal[manifest](form, "manifest")
	requireWorkspaceHTTPStatusInternal(t, err, http.StatusBadRequest)

	form = workspaceMultipartFormInternal(t, map[string][]string{"manifest": {`{"revision":"current"}`}}, nil)
	parsed, err := parseWorkspaceJSONPartInternal[manifest](form, "manifest")
	require.NoError(t, err)
	require.Equal(t, "current", parsed.Revision)
}

func TestReadWorkspaceUploadsInternal(t *testing.T) {
	form := workspaceMultipartFormInternal(t, nil, []workspaceUploadFixtureInternal{
		{name: "one.txt", content: []byte("one")},
		{name: "two.txt", content: []byte("two")},
	})
	uploads, err := readWorkspaceUploadsInternal(form, 16)
	require.NoError(t, err)
	require.Equal(t, []byte("one"), uploads[0])
	require.Equal(t, []byte("two"), uploads[1])
}

func TestReadWorkspaceUploadsInternalRejectsInvalidTextAndConfiguredLimit(t *testing.T) {
	tests := []struct {
		name     string
		content  []byte
		maxBytes int64
		status   int
	}{
		{name: "invalid UTF-8", content: []byte{0xff}, maxBytes: 16, status: http.StatusBadRequest},
		{name: "NUL byte", content: []byte{'a', 0, 'b'}, maxBytes: 16, status: http.StatusBadRequest},
		{name: "configured size", content: []byte("four"), maxBytes: 3, status: http.StatusRequestEntityTooLarge},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			form := workspaceMultipartFormInternal(t, nil, []workspaceUploadFixtureInternal{{name: "upload.txt", content: tt.content}})
			_, err := readWorkspaceUploadsInternal(form, tt.maxBytes)
			requireWorkspaceHTTPStatusInternal(t, err, tt.status)
		})
	}
}

func TestProjectWorkspaceHTTPErrorInternalMapsProjectState(t *testing.T) {
	requireWorkspaceHTTPStatusInternal(t, projectWorkspaceHTTPErrorInternal(common.Classify(common.ErrProjectNotFound, errors.New("missing"))), http.StatusNotFound)
	requireWorkspaceHTTPStatusInternal(t, projectWorkspaceHTTPErrorInternal(common.Classify(common.ErrProjectArchived, errors.New("archived"))), http.StatusConflict)
}
