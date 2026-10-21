package podman

import (
	"archive/tar"
	"bytes"
	"fmt"

	"github.com/getarcaneapp/arcane/types/v3"
	kit "go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
)

func buildTar(files []types.QuadletFile) (*bytes.Buffer, error) {
	buf := &bytes.Buffer{}
	writer := tar.NewWriter(buf)
	seen := make(map[string]struct{}, len(files))

	for _, file := range files {
		name, err := kit.ValidateFileName(file.Name)
		if err != nil {
			return nil, common.Classify(common.ErrBadRequest, fmt.Errorf("invalid file name %q: %w", file.Name, err))
		}
		if _, ok := seen[name]; ok {
			return nil, common.Classify(common.ErrBadRequest, fmt.Errorf("duplicate file name %q", name))
		}
		seen[name] = struct{}{}

		if headerErr := writer.WriteHeader(&tar.Header{
			Name:     name,
			Mode:     0o644,
			Size:     int64(len(file.Content)),
			Typeflag: tar.TypeReg,
		}); headerErr != nil {
			return nil, fmt.Errorf("write tar header for %s: %w", name, headerErr)
		}
		if _, writeErr := writer.Write([]byte(file.Content)); writeErr != nil {
			return nil, fmt.Errorf("write tar entry for %s: %w", name, writeErr)
		}
	}

	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("close tar: %w", err)
	}
	return buf, nil
}
