package handlerutil

import (
	"context"
	"encoding/json/v2"
	stderrors "errors"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/remenv"
)

// RemoteJSONProxy is the remote environment transport operation used by domain handlers.
type RemoteJSONProxy func(context.Context, string, string, string, []byte, any) error

// JSON marshals a request, invokes the environment proxy, and translates transport errors for Huma.
func (p RemoteJSONProxy) JSON[T any](
	ctx context.Context,
	environmentID string,
	method string,
	path string,
	requestBody any,
) (*T, error) {
	body, err := marshalRemoteRequestBodyInternal(requestBody)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to marshal request body: " + err.Error())
	}

	var output T
	if err := p(ctx, environmentID, method, path, body, &output); err != nil {
		return nil, TranslateRemoteProxyError(err)
	}

	return &output, nil
}

func marshalRemoteRequestBodyInternal(requestBody any) ([]byte, error) {
	switch value := requestBody.(type) {
	case nil:
		return nil, nil
	case []byte:
		return value, nil
	default:
		return json.Marshal(value)
	}
}

// TranslateRemoteProxyError converts environment transport failures into Huma errors.
func TranslateRemoteProxyError(err error) error {
	if transportErr, ok := stderrors.AsType[*remenv.TransportError](err); ok {
		return huma.Error502BadGateway("failed to proxy request to environment: " + transportErr.Error())
	}

	if statusErr, ok := stderrors.AsType[*remenv.StatusError](err); ok {
		return huma.NewError(statusErr.StatusCode, "environment returned error: "+string(statusErr.Body), nil)
	}

	if decodeErr, ok := stderrors.AsType[*remenv.DecodeError](err); ok {
		return huma.Error500InternalServerError("failed to decode environment response: " + decodeErr.Error())
	}

	return huma.Error500InternalServerError("failed to proxy request to environment: " + err.Error())
}
