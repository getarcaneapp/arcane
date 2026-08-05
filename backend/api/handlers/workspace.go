package handlers

import (
	"encoding/json/v2"
	"fmt"
	"io"
	"mime/multipart"

	"github.com/danielgtaylor/huma/v2"
	workspacepkg "github.com/getarcaneapp/arcane/backend/v2/pkg/workspace"
)

func parseWorkspaceJSONPartInternal[T any](form multipart.Form, partName string) (T, error) {
	var result T
	values := form.Value[partName]
	if len(values) != 1 {
		return result, huma.Error400BadRequest("exactly one " + partName + " part is required")
	}
	if err := json.Unmarshal([]byte(values[0]), &result); err != nil {
		return result, huma.Error400BadRequest("invalid " + partName + " JSON")
	}
	return result, nil
}

func readWorkspaceUploadsInternal(form multipart.Form, maxFileSizeBytes int64) (map[int][]byte, error) {
	headers := form.File["files"]
	uploads := make(map[int][]byte, len(headers))
	limitMessage := fmt.Sprintf("uploaded file exceeds configured %d MiB workspace limit", maxFileSizeBytes/(1024*1024))
	for index, header := range headers {
		if header.Size > maxFileSizeBytes {
			return nil, huma.Error413RequestEntityTooLarge(limitMessage)
		}
		file, err := header.Open()
		if err != nil {
			return nil, huma.Error400BadRequest("failed to open uploaded file")
		}
		content, readErr := io.ReadAll(io.LimitReader(file, maxFileSizeBytes+1))
		closeErr := file.Close()
		if readErr != nil {
			return nil, huma.Error400BadRequest("failed to read uploaded file")
		}
		if closeErr != nil {
			return nil, huma.Error400BadRequest("failed to close uploaded file")
		}
		if int64(len(content)) > maxFileSizeBytes {
			return nil, huma.Error413RequestEntityTooLarge(limitMessage)
		}
		if err := workspacepkg.ValidateTextContent(content, maxFileSizeBytes); err != nil {
			return nil, huma.Error400BadRequest(err.Error())
		}
		uploads[index] = content
	}
	return uploads, nil
}
