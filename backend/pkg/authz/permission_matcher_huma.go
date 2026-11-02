package authz

import (
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/samber/mo"
)

const (
	// MetaRequiredPermission is the huma.Operation.Metadata key under which the
	// environment-scoped registration helper records the permission an operation
	// requires. The remote environment proxy reads this metadata (via the OpenAPI
	// document) to enforce the same permission locally before forwarding a request
	// to an agent.
	MetaRequiredPermission = "arcane:requiredPermission"

	MetaEnvironmentAggregate = "arcane:environmentAggregate"

	MetaActionParam = "arcane:actionParam"

	MetaActionPermissions = "arcane:actionPermissions"

	envPathPrefix = "/environments/{id}"
)

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
		suffix, ok := envResourceSuffix(path).Get()
		if !ok {
			continue
		}
		for method, op := range operationsByMethod(item) {
			if op == nil {
				continue
			}
			if aggregate, _ := op.Metadata[MetaEnvironmentAggregate].(bool); aggregate && method == http.MethodGet {
				if m.aggregates == nil {
					m.aggregates = map[string]struct{}{}
				}
				m.aggregates[method+" "+suffix] = struct{}{}
			}
			param, hasParam := op.Metadata[MetaActionParam].(string)
			perms, hasPerms := op.Metadata[MetaActionPermissions].(map[string]string)
			if hasParam && hasPerms {
				m.AddActions(method, suffix, param, perms)
				continue
			}
			if perm, localOk := requiredPermission(op).Get(); localOk {
				m.Add(method, suffix, perm)
				continue
			}
			// An environment-scoped operation that declares no required
			// permission is only allowed through the proxy when it is an
			// explicitly public endpoint (empty Security). Anything else is
			// left unmapped and denied by default.
			if op.Security != nil && len(op.Security) == 0 {
				m.AddPublic(method, suffix)
			}
		}
	}
}

// envResourceSuffix returns the resource path after /environments/{id}
// for environment-scoped operation paths, e.g. "/containers" for
// "/environments/{id}/containers". It returns None for paths that are not
// environment-scoped resource paths (including the bare "/environments/{id}").
func envResourceSuffix(path string) mo.Option[string] {
	if !strings.HasPrefix(path, envPathPrefix+"/") {
		return mo.None[string]()
	}
	return mo.Some(strings.TrimPrefix(path, envPathPrefix))
}

func requiredPermission(op *huma.Operation) mo.Option[string] {
	if op.Metadata == nil {
		return mo.None[string]()
	}
	perm, ok := op.Metadata[MetaRequiredPermission].(string)
	if !ok || perm == "" {
		return mo.None[string]()
	}
	return mo.Some(perm)
}

func operationsByMethod(item *huma.PathItem) map[string]*huma.Operation {
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
