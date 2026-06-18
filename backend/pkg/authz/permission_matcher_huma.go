package authz

import (
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
)

// MetaRequiredPermission is the huma.Operation.Metadata key under which the
// environment-scoped registration helper records the permission an operation
// requires. The remote environment proxy reads this metadata (via the OpenAPI
// document) to enforce the same permission locally before forwarding a request
// to an agent.
const MetaRequiredPermission = "arcane:requiredPermission"

const envPathPrefixInternal = "/environments/{id}"

// CollectFromHumaAPI walks every operation registered on humaAPI and records,
// for each environment-scoped operation, the permission it requires. The
// permission is read from the operation metadata set by the registration
// helper (see MetaRequiredPermission).
//
// Operation paths are stored as the resource suffix after /environments/{id}
// (for example "/containers/{containerId}/start") so the remote environment
// proxy can match them against forwarded request paths.
func (m *PermissionMatcher) CollectFromHumaAPI(humaAPI huma.API) {
	oapi := humaAPI.OpenAPI()
	if oapi == nil || oapi.Paths == nil {
		return
	}
	for path, item := range oapi.Paths {
		if item == nil {
			continue
		}
		suffix, ok := envResourceSuffixInternal(path)
		if !ok {
			continue
		}
		for method, op := range operationsByMethodInternal(item) {
			if op == nil {
				continue
			}
			if perm, ok := requiredPermissionInternal(op); ok {
				m.Add(method, suffix, perm)
				continue
			}
			// An environment-scoped operation that declares no required
			// permission is only allowed through the proxy when it is an
			// explicitly public endpoint (empty Security). Anything else is
			// left unmapped and denied by default.
			if isPublicOperationInternal(op) {
				m.AddPublic(method, suffix)
			}
		}
	}
}

// envResourceSuffixInternal returns the resource path after /environments/{id}
// for environment-scoped operation paths, e.g. "/containers" for
// "/environments/{id}/containers". It returns ok=false for paths that are not
// environment-scoped resource paths (including the bare "/environments/{id}").
func envResourceSuffixInternal(path string) (string, bool) {
	if !strings.HasPrefix(path, envPathPrefixInternal+"/") {
		return "", false
	}
	return strings.TrimPrefix(path, envPathPrefixInternal), true
}

func requiredPermissionInternal(op *huma.Operation) (string, bool) {
	if op.Metadata == nil {
		return "", false
	}
	perm, ok := op.Metadata[MetaRequiredPermission].(string)
	if !ok || perm == "" {
		return "", false
	}
	return perm, true
}

// isPublicOperationInternal reports whether op explicitly opts out of all
// security requirements (an empty, non-nil Security list). Such operations are
// intentionally public and are allowed through the proxy without a permission
// check. A nil Security list means the operation inherits the global security
// requirement and is therefore NOT public.
func isPublicOperationInternal(op *huma.Operation) bool {
	return op.Security != nil && len(op.Security) == 0
}

func operationsByMethodInternal(item *huma.PathItem) map[string]*huma.Operation {
	return map[string]*huma.Operation{
		http.MethodGet:     item.Get,
		http.MethodPost:    item.Post,
		http.MethodPut:     item.Put,
		http.MethodDelete:  item.Delete,
		http.MethodPatch:   item.Patch,
		http.MethodHead:    item.Head,
		http.MethodOptions: item.Options,
	}
}
