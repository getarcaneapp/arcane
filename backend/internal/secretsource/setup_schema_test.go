package secretsource

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
)

// The setup routes are called with partial targets: no target while the
// wizard loads, and Bitwarden targets that have no environment. The request
// schema must accept them and leave validation to the provider.
func TestSetupRequestSchemaAcceptsPartialTargets(t *testing.T) {
	_, api := humatest.New(t)
	huma.Register(api, huma.Operation{OperationID: "plan", Method: http.MethodPost, Path: "/e/{id}/p/{projectId}/plan"},
		func(_ context.Context, _ *SetupPlanInput) (*struct{}, error) { return nil, nil })
	huma.Register(api, huma.Operation{OperationID: "apply", Method: http.MethodPost, Path: "/e/{id}/p/{projectId}/apply"},
		func(_ context.Context, _ *SetupApplyInput) (*struct{}, error) { return nil, nil })

	cases := []struct{ path, body string }{
		{"/e/0/p/x/plan", `{"sourceId":"s"}`},
		{"/e/0/p/x/plan", `{"sourceId":"s","target":{"mode":"new-folder","folderName":"app"}}`},
		{"/e/0/p/x/plan", `{"sourceId":"s","target":{"mode":"new-project","projectName":"app","environment":"prod"}}`},
		{"/e/0/p/x/apply", `{"sourceId":"s","target":{"mode":"new-folder","folderName":"app"},"keys":["A"],"values":"import",` +
			`"envFile":"remove","keepBackup":true,"grantDeployIdentity":false,"required":true,"autoRedeploy":false}`},
	}
	for _, tc := range cases {
		resp := api.Post(tc.path, strings.NewReader(tc.body))
		if resp.Code == http.StatusUnprocessableEntity || resp.Code == http.StatusBadRequest {
			t.Errorf("%s %s: status %d: %s", tc.path, tc.body, resp.Code, resp.Body.String())
		}
	}
}
