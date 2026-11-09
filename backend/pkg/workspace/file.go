package workspace

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"path"
	"path/filepath"
	"strings"

	"kit.getarcane.app/acfs"
	acfstypes "kit.getarcane.app/acfs/types"
	kitfs "kit.getarcane.app/fs"

	"getarcane.dev/types/v3"
)

func ReadFile(ctx context.Context, rootPath, rel string, entry acfstypes.Entry, maxFileSizeBytes int64) (types.WorkspaceFileContent, error) {
	response := types.WorkspaceFileContent{
		Path:         filepath.Join(rootPath, filepath.FromSlash(rel)),
		RelativePath: rel,
		Name:         entry.Name,
		Size:         entry.Size,
	}
	switch {
	case entry.IsSymlink:
		return ReadOnlyFile(response, types.FileReadOnlySymlink, ""), nil
	case !strings.HasPrefix(entry.Mode, "-"):
		return ReadOnlyFile(response, types.FileReadOnlySpecial, ""), nil
	case entry.Size > maxFileSizeBytes:
		return ReadOnlyFile(response, types.FileReadOnlyTooLarge, ""), nil
	}
	reader, _, err := acfs.Root{Path: rootPath}.OpenRead(ctx, "/"+rel, maxFileSizeBytes)
	if err != nil {
		return types.WorkspaceFileContent{}, fmt.Errorf("read workspace file: %w", err)
	}
	defer func() { _ = reader.Close() }()
	content, err := io.ReadAll(reader)
	if err != nil {
		return types.WorkspaceFileContent{}, fmt.Errorf("read workspace file: %w", err)
	}
	if !IsTextContent(content) {
		return ReadOnlyFile(response, types.FileReadOnlyBinary, http.DetectContentType(content)), nil
	}
	return types.WorkspaceFileContent{
		Path:         response.Path,
		RelativePath: response.RelativePath,
		Name:         response.Name,
		Content:      string(content),
		MimeType:     http.DetectContentType(content),
		Size:         response.Size,
		Editable:     true,
	}, nil
}

func ReadOnlyFile(file types.WorkspaceFileContent, reason, mimeType string) types.WorkspaceFileContent {
	return types.WorkspaceFileContent{
		Path:           file.Path,
		RelativePath:   file.RelativePath,
		Name:           file.Name,
		Content:        file.Content,
		MimeType:       mimeType,
		ReadOnlyReason: reason,
		Size:           file.Size,
	}
}

func WriteFile(ctx context.Context, rootPath, rel string, content io.Reader, size int64) error {
	if symlinkErr := ensureNoSymlink(ctx, acfs.Root{Path: rootPath}, rel); symlinkErr != nil {
		return symlinkErr
	}
	if mkdirErr := (acfs.Root{Path: rootPath}).MkdirAll(ctx, kitfs.LogicalPath(path.Dir(rel)), kitfs.DirPerm); mkdirErr != nil {
		return fmt.Errorf("create parent directory: %w", mkdirErr)
	}
	if _, writeErr := (acfs.Root{Path: rootPath}).WriteFrom(ctx, "/"+rel, content, acfs.WriteFromOptions{Size: size, Mode: kitfs.FilePerm}); writeErr != nil {
		return fmt.Errorf("write workspace file: %w", writeErr)
	}
	return nil
}
