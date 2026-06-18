package api

import (
	"net/http"
	"sort"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
)

// TestEnvScopedOperationsDeclarePermission guards the remote-environment proxy
// authorization model: every operation served under /environments/{id}/<sub>
// must declare its required permission via middleware.RegisterWithPermission,
// which records it in the operation metadata. The proxy reads this metadata to
// enforce the same permission it enforces locally before forwarding a request
// to an agent. An operation missing this metadata would be denied by the proxy
// (default-deny), so this test fails loudly if any env-scoped operation is
// registered with a bare huma.Register instead of RegisterWithPermission.
func TestEnvScopedOperationsDeclarePermission(t *testing.T) {
	api := SetupAPIForSpec()
	oapi := api.OpenAPI()
	if oapi == nil || oapi.Paths == nil {
		t.Fatal("expected an OpenAPI document with paths")
	}

	var missing []string
	for path, item := range oapi.Paths {
		if !strings.HasPrefix(path, "/environments/{id}/") {
			continue
		}
		for method, op := range envScopedTestOperations(item) {
			if op == nil {
				continue
			}
			// Operations that explicitly opt out of authentication (an empty,
			// non-nil Security list) are intentionally public and require no
			// permission metadata.
			if op.Security != nil && len(op.Security) == 0 {
				continue
			}
			perm, ok := op.Metadata[authz.MetaRequiredPermission].(string)
			if !ok || perm == "" {
				missing = append(missing, method+" "+path)
			}
		}
	}

	if len(missing) > 0 {
		sort.Strings(missing)
		t.Errorf("%d env-scoped operation(s) missing required-permission metadata; register them with middleware.RegisterWithPermission:\n  %s",
			len(missing), strings.Join(missing, "\n  "))
	}
}

func envScopedTestOperations(item *huma.PathItem) map[string]*huma.Operation {
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
