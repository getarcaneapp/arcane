package podman

import (
	"archive/tar"
	"bytes"
	"fmt"
	"path/filepath"

	podmantypes "github.com/getarcaneapp/arcane/types/v2/podman"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
)

func buildTar(files []podmantypes.QuadletFile) (*bytes.Buffer, error) {
	buf := &bytes.Buffer{}
	writer := tar.NewWriter(buf)
	seen := make(map[string]struct{}, len(files))

	for _, file := range files {
		if file.Name == "" || file.Name == "." || file.Name == ".." || filepath.Base(file.Name) != file.Name {
			return nil, common.Classify(common.ErrBadRequest, fmt.Errorf("invalid file name %q", file.Name))
		}
		if _, ok := seen[file.Name]; ok {
			return nil, common.Classify(common.ErrBadRequest, fmt.Errorf("duplicate file name %q", file.Name))
		}
		seen[file.Name] = struct{}{}

		if err := writer.WriteHeader(&tar.Header{
			Name:     file.Name,
			Mode:     0o644,
			Size:     int64(len(file.Content)),
			Typeflag: tar.TypeReg,
		}); err != nil {
			return nil, fmt.Errorf("write tar header for %s: %w", file.Name, err)
		}
		if _, err := writer.Write([]byte(file.Content)); err != nil {
			return nil, fmt.Errorf("write tar entry for %s: %w", file.Name, err)
		}
	}

	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("close tar: %w", err)
	}
	return buf, nil
}
