package authz

import (
	"cmp"
	"maps"
	"slices"
	"strings"

	"github.com/danielgtaylor/huma/v2"
)

type (
	OperationRecorder struct {
		huma.API

		hidden []*huma.Operation
	}

	PermissionRoute struct {
		Method     string
		Path       string
		Access     string
		Permission string
		Conditions PermissionConditions
	}

	PermissionExport struct {
		Resources               []PermissionExportResource `json:"resources"`
		Endpoints               []PermissionExportEndpoint `json:"endpoints"`
		UnreferencedPermissions []string                   `json:"unreferencedPermissions"`
	}

	PermissionExportResource struct {
		Key         string                   `json:"key"`
		Label       string                   `json:"label"`
		Description string                   `json:"description"`
		Scope       string                   `json:"scope"`
		Actions     []PermissionExportAction `json:"actions"`
	}

	PermissionExportAction struct {
		Key              string                        `json:"key"`
		Permission       string                        `json:"permission"`
		Label            string                        `json:"label"`
		Description      string                        `json:"description"`
		Requires         []string                      `json:"requires"`
		RequiresExpanded []string                      `json:"requiresExpanded"`
		Endpoints        []PermissionExportEndpointRef `json:"endpoints"`
	}

	PermissionExportEndpointRef struct {
		Action      *PermissionExportActionSelect `json:"action,omitzero"`
		Condition   *PermissionCondition          `json:"condition,omitzero"`
		Method      string                        `json:"method"`
		Path        string                        `json:"path"`
		OperationID string                        `json:"operationId,omitempty"`
		Summary     string                        `json:"summary,omitempty"`
	}

	PermissionExportActionSelect struct {
		Param string `json:"param"`
		Value string `json:"value"`
	}

	PermissionExportEndpoint struct {
		Method            string               `json:"method"`
		Path              string               `json:"path"`
		OperationID       string               `json:"operationId,omitempty"`
		Summary           string               `json:"summary,omitempty"`
		Tags              []string             `json:"tags,omitempty"`
		Access            string               `json:"access"`
		Permission        string               `json:"permission,omitempty"`
		ActionParam       string               `json:"actionParam,omitempty"`
		ActionPermissions map[string]string    `json:"actionPermissions,omitempty"`
		Conditions        PermissionConditions `json:"conditionalPermissions,omitempty"`
		GlobalAdmin       bool                 `json:"globalAdmin,omitzero"`
	}
)

const (
	EndpointAccessPermission        = "permission"
	EndpointAccessActionPermissions = "actionPermissions"
	EndpointAccessGlobalAdmin       = "globalAdmin"
	EndpointAccessSudo              = "sudo"
	EndpointAccessPublic            = "public"
	EndpointAccessAuthenticated     = "authenticated"
	EndpointAccessAgentToken        = "agentToken"
	EndpointAccessWebhookToken      = "webhookToken"
	EndpointAccessSubjectToken      = "subjectToken"
)

func (r *OperationRecorder) DocumentOperation(op *huma.Operation) {
	if op.Hidden {
		r.hidden = append(r.hidden, op)
		return
	}
	r.OpenAPI().AddOperation(op)
}

func BuildPermissionExport(humaAPI *OperationRecorder, routes []PermissionRoute) PermissionExport {
	var endpoints []PermissionExportEndpoint
	for path, item := range humaAPI.OpenAPI().Paths {
		for method, op := range operationsByMethod(item) {
			if op != nil {
				endpoints = append(endpoints, exportEndpoint(method, path, op))
			}
		}
	}
	for _, op := range humaAPI.hidden {
		endpoints = append(endpoints, exportEndpoint(op.Method, op.Path, op))
	}
	for _, route := range routes {
		segments := strings.Split(route.Path, "/")
		for i, segment := range segments {
			if name, ok := strings.CutPrefix(segment, ":"); ok {
				segments[i] = "{" + name + "}"
			}
		}
		endpoints = append(endpoints, PermissionExportEndpoint{
			Method:     route.Method,
			Path:       strings.Join(segments, "/"),
			Access:     cmp.Or(route.Access, EndpointAccessPermission),
			Permission: route.Permission,
			Conditions: route.Conditions,
		})
	}
	slices.SortFunc(endpoints, func(a, b PermissionExportEndpoint) int {
		return cmp.Or(cmp.Compare(a.Path, b.Path), cmp.Compare(a.Method, b.Method))
	})

	refs := map[string][]PermissionExportEndpointRef{}
	for _, endpoint := range endpoints {
		if endpoint.Permission != "" {
			refs[endpoint.Permission] = append(refs[endpoint.Permission], PermissionExportEndpointRef{
				Method:      endpoint.Method,
				Path:        endpoint.Path,
				OperationID: endpoint.OperationID,
				Summary:     endpoint.Summary,
			})
		}
		for _, value := range slices.Sorted(maps.Keys(endpoint.ActionPermissions)) {
			refs[endpoint.ActionPermissions[value]] = append(refs[endpoint.ActionPermissions[value]], PermissionExportEndpointRef{
				Method:      endpoint.Method,
				Path:        endpoint.Path,
				OperationID: endpoint.OperationID,
				Summary:     endpoint.Summary,
				Action: &PermissionExportActionSelect{
					Param: endpoint.ActionParam,
					Value: value,
				},
			})
		}
		for _, condition := range endpoint.Conditions {
			refs[condition.Permission] = append(refs[condition.Permission], PermissionExportEndpointRef{
				Method:      endpoint.Method,
				Path:        endpoint.Path,
				OperationID: endpoint.OperationID,
				Summary:     endpoint.Summary,
				Condition:   new(condition),
			})
		}
	}

	catalog := PermissionCatalog()
	resources := make([]PermissionExportResource, len(catalog))
	var unreferenced []string
	for i, resource := range catalog {
		actions := make([]PermissionExportAction, len(resource.Actions))
		for j, action := range resource.Actions {
			if len(refs[action.Permission]) == 0 {
				unreferenced = append(unreferenced, action.Permission)
			}
			actions[j] = PermissionExportAction{
				Key:         action.Key,
				Permission:  action.Permission,
				Label:       action.Label,
				Description: action.Description,
				Requires:    action.Requires,
				RequiresExpanded: slices.DeleteFunc(ExpandPermissions([]string{action.Permission}), func(perm string) bool {
					return perm == action.Permission
				}),
				Endpoints: refs[action.Permission],
			}
		}
		resources[i] = PermissionExportResource{
			Key:         resource.Key,
			Label:       resource.Label,
			Description: resource.Description,
			Scope:       resource.Scope,
			Actions:     actions,
		}
	}

	return PermissionExport{
		Resources:               resources,
		Endpoints:               endpoints,
		UnreferencedPermissions: unreferenced,
	}
}

func exportEndpoint(method, path string, op *huma.Operation) PermissionExportEndpoint {
	actionParam, _ := op.Metadata[MetaActionParam].(string)
	actionPerms, _ := op.Metadata[MetaActionPermissions].(map[string]string)
	perm := requiredPermission(op).OrEmpty()
	globalAdmin, _ := op.Metadata[MetaRequireGlobalAdmin].(bool)
	sudo, _ := op.Metadata[MetaRequireSudo].(bool)
	conditions, _ := op.Metadata[MetaPermissionConditions].(PermissionConditions)
	access, _ := op.Metadata[MetaAccess].(string)

	return PermissionExportEndpoint{
		Method:      method,
		Path:        path,
		OperationID: op.OperationID,
		Summary:     op.Summary,
		Tags:        op.Tags,
		Access: func() string {
			switch {
			case access != "":
				return access
			case len(actionPerms) > 0:
				return EndpointAccessActionPermissions
			case perm != "":
				return EndpointAccessPermission
			case globalAdmin:
				return EndpointAccessGlobalAdmin
			case sudo:
				return EndpointAccessSudo
			case op.Security != nil && len(op.Security) == 0:
				return EndpointAccessPublic
			default:
				return EndpointAccessAuthenticated
			}
		}(),
		Permission:        perm,
		ActionParam:       actionParam,
		ActionPermissions: actionPerms,
		Conditions:        conditions,
		GlobalAdmin:       globalAdmin && (len(actionPerms) > 0 || perm != ""),
	}
}
