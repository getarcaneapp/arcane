package authz

import (
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
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

	envPathPrefix = "/agents/{id}"
)

func (m *PermissionMatcher) CollectFromHumaAPI(humaAPI huma.API) {
	oapi := humaAPI.OpenAPI()
	if oapi == nil || oapi.Paths == nil {
		return
	}
	for path, item := range oapi.Paths {
		if item == nil || !strings.HasPrefix(path, envPathPrefix+"/") {
			continue
		}
		suffix := strings.TrimPrefix(path, envPathPrefix)
		for method, op := range operationsByMethod(item) {
			if op == nil {
				continue
			}
			if aggregate, _ := op.Metadata[MetaEnvironmentAggregate].(bool); aggregate {
				m.aggregates[method+" "+suffix] = struct{}{}
			}
			param, hasParam := op.Metadata[MetaActionParam].(string)
			perms, hasPerms := op.Metadata[MetaActionPermissions].(map[string]string)
			if hasParam && hasPerms {
				m.AddActions(method, suffix, param, perms)
				continue
			}
			if perm, localOk := op.Metadata[MetaRequiredPermission].(string); localOk && perm != "" {
				m.Add(method, suffix, perm)
				continue
			}
			if op.Security != nil && len(op.Security) == 0 {
				m.Add(method, suffix, "")
				continue
			}
			// Sudo-only routes stay unproxied, but are known so they answer 403 rather than 404.
			m.AddDenied(method, suffix)
		}
	}
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
