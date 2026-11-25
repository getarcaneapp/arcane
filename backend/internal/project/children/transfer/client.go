package transfer

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"

	"kit.getarcane.app"
	"kit.getarcane.app/docker"

	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/arcane/v3/internal/environment"
	"getarcane.dev/arcane/v3/pkg/agent"
	"getarcane.dev/types/v3"
)

type (
	Environments struct {
		Local     Environment
		Transport environment.RemoteTransport
		Tunnels   *agent.TunnelRegistry
	}

	remoteEnvironment struct {
		transport environment.RemoteTransport
		tunnels   *agent.TunnelRegistry
		id        string
	}

	streamLine struct {
		Error string `json:"error"`
		Done  bool   `json:"done"`
	}
)

const agentPrefix = "/api/agents/" + environment.LocalEnvironmentID

func (e Environments) Get(environmentID string) Environment {
	if environmentID == environment.LocalEnvironmentID {
		return e.Local
	}
	return remoteEnvironment{
		transport: e.Transport,
		tunnels:   e.Tunnels,
		id:        environmentID,
	}
}

func (e remoteEnvironment) Availability(context.Context) Availability {
	tunnel, ok := e.tunnels.Active(e.id).Get()
	switch {
	case !ok:
		return AvailabilityOffline
	case !slices.Contains(tunnel.MetadataSnapshot().Capabilities, agent.TunnelCapabilityProjectTransfer):
		return AvailabilityOutdated
	}
	return AvailabilityReady
}

func (e remoteEnvironment) Project(ctx context.Context, projectID string) (types.Project, error) {
	response, err := e.decode[types.ApiResponse[types.Project]](ctx, http.MethodGet, "/projects/"+url.PathEscape(projectID)+"/compose", nil)
	return response.Data, err
}

func (e remoteEnvironment) Projects(ctx context.Context, search string) ([]types.Project, error) {
	response, err := e.decode[types.Paginated[types.Project]](ctx, http.MethodGet, "/projects?limit=-1&search="+url.QueryEscape(search), nil)
	return response.Data, err
}

func (e remoteEnvironment) Volume(ctx context.Context, name string) (types.Volume, bool, error) {
	response, err := e.decode[types.ApiResponse[*types.Volume]](ctx, http.MethodGet, "/volumes/"+url.PathEscape(name), nil)
	if statusErr, ok := errors.AsType[*agent.StatusError](err); ok && statusErr.StatusCode == http.StatusNotFound {
		return types.Volume{}, false, nil
	}
	if err != nil {
		return types.Volume{}, false, err
	}
	return kit.FromPtr(response.Data), true, nil
}

func (e remoteEnvironment) Network(ctx context.Context, name string) (types.NetworkInspect, bool, error) {
	response, err := e.decode[types.ApiResponse[types.NetworkInspect]](ctx, http.MethodGet, "/networks/"+url.PathEscape(name), nil)
	if statusErr, ok := errors.AsType[*agent.StatusError](err); ok && statusErr.StatusCode == http.StatusNotFound {
		return types.NetworkInspect{}, false, nil
	}
	if err != nil {
		return types.NetworkInspect{}, false, err
	}
	return response.Data, true, nil
}

func (e remoteEnvironment) CreateVolume(ctx context.Context, volume types.Volume, _ types.Actor) error {
	_, err := e.decode[types.ApiResponse[*types.Volume]](ctx, http.MethodPost, "/volumes", volume)
	return err
}

func (e remoteEnvironment) CreateNetwork(ctx context.Context, network types.Network, _ types.Actor) error {
	_, err := e.decode[types.ApiResponse[types.NetworkCreateResponse]](ctx, http.MethodPost, "/networks", network)
	return err
}

func (e remoteEnvironment) ComposeProjectInUse(ctx context.Context, name string) (bool, error) {
	response, err := e.decode[types.Paginated[struct{}]](
		ctx, http.MethodGet,
		"/containers?limit=1&includeInternal=true&includeHidden=true&label="+url.QueryEscape(docker.ComposeProjectLabelKey+"="+name),
		nil,
	)
	return len(response.Data) > 0, err
}

func (e remoteEnvironment) ExportProject(ctx context.Context, projectID string, request types.ExportProjectArchive, _ types.Actor) (types.UploadSession, error) {
	response, err := e.decode[types.ApiResponse[types.UploadSession]](agent.AwaitUntilDeadline(ctx), http.MethodPost, "/projects/"+url.PathEscape(projectID)+"/export?attached=true", request)
	return response.Data, err
}

func (e remoteEnvironment) ImportProject(ctx context.Context, request types.ImportProjectArchive, _ types.Actor) (string, error) {
	response, err := e.decode[types.ApiResponse[types.CreateProjectResponse]](agent.AwaitUntilDeadline(ctx), http.MethodPost, "/projects/import?attached=true", request)
	return response.Data.ID, err
}

func (e remoteEnvironment) CreateUpload(ctx context.Context, kind string, request types.CreateUploadSessionRequest) (types.UploadSession, error) {
	response, err := e.decode[types.ApiResponse[types.UploadSession]](ctx, http.MethodPost, "/uploads/"+url.PathEscape(kind), request)
	return response.Data, err
}

func (e remoteEnvironment) Upload(ctx context.Context, kind, uploadID string) (types.UploadSession, error) {
	response, err := e.decode[types.ApiResponse[types.UploadSession]](ctx, http.MethodGet, "/uploads/"+url.PathEscape(kind)+"/"+url.PathEscape(uploadID), nil)
	return response.Data, err
}

func (e remoteEnvironment) ReadChunk(ctx context.Context, kind, uploadID string, index int) ([]byte, error) {
	response, err := e.call(ctx, http.MethodGet, "/uploads/"+url.PathEscape(kind)+"/"+url.PathEscape(uploadID)+"/chunks/"+strconv.Itoa(index), nil)
	if err != nil {
		return nil, err
	}
	return response.Body, nil
}

func (e remoteEnvironment) WriteChunk(ctx context.Context, kind, uploadID string, index int, data []byte) error {
	_, err := e.call(ctx, http.MethodPut, "/uploads/"+url.PathEscape(kind)+"/"+url.PathEscape(uploadID)+"/chunks/"+strconv.Itoa(index), data)
	return err
}

func (e remoteEnvironment) DeleteUpload(ctx context.Context, kind, uploadID string) error {
	_, err := e.call(ctx, http.MethodDelete, "/uploads/"+url.PathEscape(kind)+"/"+url.PathEscape(uploadID), nil)
	if statusErr, ok := errors.AsType[*agent.StatusError](err); ok && statusErr.StatusCode == http.StatusNotFound {
		return nil
	}
	return err
}

func (e remoteEnvironment) DeployProject(ctx context.Context, projectID string, _ types.Actor) error {
	response, err := e.call(ctx, http.MethodPost, "/projects/"+url.PathEscape(projectID)+"/deploy?action=up", nil)
	if err != nil {
		return err
	}
	scanner := bufio.NewScanner(bytes.NewReader(response.Body))
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		var line streamLine
		if json.Unmarshal(scanner.Bytes(), &line) != nil {
			continue
		}
		if line.Error != "" {
			return errors.New(line.Error)
		}
		if line.Done {
			return nil
		}
	}
	return errors.Join(errors.New("deployment stream ended without completing"), scanner.Err())
}

func (e remoteEnvironment) DownProject(ctx context.Context, projectID string, _ types.Actor) error {
	_, err := e.call(ctx, http.MethodPost, "/projects/"+url.PathEscape(projectID)+"?action=down", nil)
	return err
}

func (e remoteEnvironment) DestroyProject(ctx context.Context, projectID string, _ types.Actor) error {
	body, err := json.Marshal(types.ProjectDestroy{RemoveFiles: new(true)})
	if err != nil {
		return err
	}
	_, err = e.call(ctx, http.MethodDelete, "/projects/"+url.PathEscape(projectID), body)
	return err
}

func (e remoteEnvironment) DeleteVolume(ctx context.Context, name string, _ types.Actor) error {
	_, err := e.call(ctx, http.MethodDelete, "/volumes/"+url.PathEscape(name), nil)
	if statusErr, ok := errors.AsType[*agent.StatusError](err); ok && statusErr.StatusCode == http.StatusNotFound {
		return nil
	}
	return err
}

func (e remoteEnvironment) call(ctx context.Context, method, path string, body []byte) (*agent.RemoteResponse, error) {
	response, err := e.transport.ExecuteRemoteRequest(ctx, e.id, method, agentPrefix+path, body)
	if err != nil {
		return nil, err
	}
	switch response.StatusCode {
	case http.StatusConflict:
		kind := common.ErrConflict
		if bytes.Contains(response.Body, []byte(common.ErrProjectLocked.Error())) {
			kind = common.ErrProjectLocked
		}
		return nil, common.Classify(kind, response.RequireSuccess())
	case http.StatusNotFound:
		return nil, common.Classify(common.ErrNotFound, response.RequireSuccess())
	}
	if successErr := response.RequireSuccess(); successErr != nil {
		return nil, successErr
	}
	return response, nil
}

func (e remoteEnvironment) decode[T any](ctx context.Context, method, path string, request any) (T, error) {
	var out T
	var body []byte
	if request != nil {
		encoded, err := json.Marshal(request)
		if err != nil {
			return out, err
		}
		body = encoded
	}
	response, err := e.call(ctx, method, path, body)
	if err != nil {
		return out, err
	}
	if decodeErr := json.Unmarshal(response.Body, &out); decodeErr != nil {
		return out, fmt.Errorf("decode response from %s: %w", path, decodeErr)
	}
	return out, nil
}
