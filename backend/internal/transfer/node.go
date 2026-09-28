package transfer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/environment"
	transferlib "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/transfer"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/remenv"
	"github.com/getarcaneapp/arcane/types/v2/base"
	transfertypes "github.com/getarcaneapp/arcane/types/v2/transfer"
	uploadtypes "github.com/getarcaneapp/arcane/types/v2/upload"
	"github.com/moby/moby/client"
)

// Per-node operations every environment serves, and the dispatch the manager
// uses to run them either in-process (environment "0") or over the
// authenticated environment proxy. Each remote hop is one buffered request.

const (
	callTimeoutInternal  = 90 * time.Second
	chunkTimeoutInternal = 5 * time.Minute
	// longTimeoutInternal bounds operations that wait on Docker: image pulls,
	// container creation, deploys, and archive extraction.
	longTimeoutInternal = 30 * time.Minute
)

// ErrEndpointUnsupported means the environment predates the transfer protocol.
var ErrEndpointUnsupported = common.Classify(common.ErrConflict, errors.New("environment does not support transfers; upgrade its agent"))

// Capabilities reports the protocol, chunk bounds, and Docker daemon identity.
func (s *Service) Capabilities(ctx context.Context) (transfertypes.Capabilities, error) {
	dockerClient, err := s.docker.GetClient(ctx)
	if err != nil {
		return transfertypes.Capabilities{}, err
	}
	info, err := dockerClient.Info(ctx, client.InfoOptions{})
	if err != nil {
		return transfertypes.Capabilities{}, fmt.Errorf("docker info: %w", err)
	}
	return transfertypes.Capabilities{Protocol: transfertypes.ProtocolVersion, ChunkSize: transfertypes.ChunkSize, DaemonID: info.Info.ID, OS: info.Info.OSType, Arch: info.Info.Architecture}, nil
}

// Export archives the source (Docker's tar for volumes and host paths,
// go-archive for project directories) into the spool.
func (s *Service) Export(ctx context.Context, request transfertypes.ExportRequest) (transfertypes.Export, error) {
	var reader io.ReadCloser
	var cleanup func()
	var err error
	if request.Source.Kind == transfertypes.ResourceProjectDir {
		reader, cleanup, err = s.projects.OpenTransferArchive(ctx, request.Source.ProjectID)
	} else {
		reader, cleanup, err = s.volumes.OpenTransferArchive(ctx, request.Source)
	}
	if err != nil {
		return transfertypes.Export{}, err
	}
	defer func() {
		_ = reader.Close()
		if cleanup != nil {
			cleanup()
		}
	}()
	return s.spool.Create(ctx, reader)
}

// ExportRead serves one byte range of a staged archive.
func (s *Service) ExportRead(exportID string, offset, length int64) ([]byte, error) {
	data, err := s.spool.ReadAt(exportID, offset, length)
	if errors.Is(err, transferlib.ErrExportNotFound) {
		return nil, common.Classify(common.ErrNotFound, err)
	}
	return data, err
}

// Import checks the assembled upload against the source hash and extracts it
// into a freshly created, transfer-labelled target.
func (s *Service) Import(ctx context.Context, request transfertypes.ImportRequest) error {
	file, _, cleanup, err := s.uploads.Consume(ctx, uploadtypes.KindVolumeBackup, request.UploadID)
	if err != nil {
		return err
	}
	defer cleanup()
	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return fmt.Errorf("hash uploaded archive: %w", err)
	}
	if actual := hex.EncodeToString(hasher.Sum(nil)); actual != request.SHA256 {
		return common.Classify(common.ErrConflict, fmt.Errorf("uploaded archive hash %s does not match the source %s", actual, request.SHA256))
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if request.Target.Kind == transfertypes.ResourceProjectDir {
		_, err = s.projects.ImportTransferArchive(ctx, request.Target.ProjectDir, file)
		return err
	}
	return s.volumes.ImportTransferArchive(ctx, request.Target, file)
}

// Hold reserves a resource on this node for a transfer.
func (s *Service) Hold(ctx context.Context, request transfertypes.HoldRequest) (transfertypes.Hold, error) {
	if err := s.holds.Acquire(ctx, request.TransferID, request.Kind, request.Resource); err != nil {
		if errors.Is(err, transferlib.ErrHeldByOther) {
			return transfertypes.Hold{}, common.Classify(common.ErrResourceHeldByTransfer, err)
		}
		return transfertypes.Hold{}, err
	}
	hold, err := s.holds.Get(ctx, request.Kind, request.Resource)
	if err != nil || hold == nil {
		return transfertypes.Hold{}, err
	}
	return *hold, nil
}

func remoteErrorInternal(err error) error {
	var status *remenv.StatusError
	if !errors.As(err, &status) {
		return err
	}
	message := strings.TrimSpace(string(status.Body))
	var body struct {
		Detail string `json:"detail"`
	}
	if json.Unmarshal(status.Body, &body) == nil && strings.TrimSpace(body.Detail) != "" {
		message = body.Detail
	}
	if message == "" {
		message = fmt.Sprintf("remote endpoint returned status %d", status.StatusCode)
	}
	switch status.StatusCode {
	case http.StatusNotFound:
		return common.Classify(common.ErrNotFound, errors.New(message))
	case http.StatusConflict:
		return common.Classify(common.ErrConflict, errors.New(message))
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		return common.Classify(common.ErrBadRequest, errors.New(message))
	case http.StatusForbidden:
		return common.Classify(common.ErrForbidden, errors.New(message))
	default:
		return errors.New(message)
	}
}

// callInternal runs one node operation: in-process through local for the
// manager's own environment, otherwise as a proxied request to the agent.
func callInternal[T any](ctx context.Context, s *Service, envID, method, path string, body any, timeout time.Duration, local func() (T, error)) (T, error) {
	if envID == environment.LocalEnvironmentID {
		return local()
	}
	var zero T
	var encoded []byte
	if body != nil {
		var err error
		if encoded, err = json.Marshal(body); err != nil {
			return zero, err
		}
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	response, err := s.environments.ExecuteRemoteRequest(callCtx, envID, method, path, encoded)
	if err != nil {
		return zero, err
	}
	if err := response.RequireSuccess(); err != nil {
		return zero, remoteErrorInternal(err)
	}
	var envelope base.ApiResponse[T]
	if err := json.Unmarshal(response.Body, &envelope); err != nil {
		return zero, &remenv.DecodeError{Err: err}
	}
	return envelope.Data, nil
}

// discardInternal keeps only the error of a message-only operation.
func discardInternal(_ base.MessageResponse, err error) error { return err }
