package authz

import (
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/samber/mo"
)

const (
	MetaRequiredPermission = "arcane:requiredPermission"

	MetaEnvironmentAggregate = "arcane:environmentAggregate"

	MetaActionParam = "arcane:actionParam"

	MetaActionPermissions = "arcane:actionPermissions"

	MetaRequireGlobalAdmin = "arcane:requireGlobalAdmin"

	MetaRequireSudo = "arcane:requireSudo"

	MetaPermissionConditions = "arcane:permissionConditions"

	MetaAccess = "arcane:access"

	envPathPrefix = "/environments/{id}"
)

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
			if op.Security != nil && len(op.Security) == 0 {
				m.AddPublic(method, suffix)
			}
		}
	}
}

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
