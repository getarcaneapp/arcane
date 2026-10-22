package cmdutil

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getarcaneapp/arcane/cli/v3/internal/client"
	"github.com/getarcaneapp/arcane/cli/v3/internal/types"
)

func TestValidatePermissions(t *testing.T) {
	for _, tt := range []struct {
		name    string
		perms   []string
		wantErr string
		calls   int
	}{
		{name: "empty skips manifest", calls: 0},
		{name: "known", perms: []string{"containers:start", "roles:list"}, calls: 1},
		{name: "unknown deduplicated", perms: []string{"containers:start", "bogus:perm", "bogus:perm"}, wantErr: "unknown permission(s): bogus:perm (", calls: 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			requests := make(chan string, 2)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests <- r.URL.Path
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprint(w, `{"success":true,"data":{"resources":[`+
					`{"key":"containers","label":"Containers","scope":"env","actions":[{"key":"start","permission":"containers:start","label":"Start"}]},`+
					`{"key":"roles","label":"Roles","scope":"global","actions":[{"key":"list","permission":"roles:list","label":"List"}]}]}}`)
			}))
			defer server.Close()
			c, err := client.New(&types.Config{ServerURL: server.URL, APIKey: "test-key"})
			require.NoError(t, err)

			validateErr := ValidatePermissions(t.Context(), c, tt.perms)

			if tt.wantErr == "" {
				require.NoError(t, validateErr)
			} else {
				require.ErrorContains(t, validateErr, tt.wantErr)
			}
			require.Len(t, requests, tt.calls)
			if tt.calls > 0 {
				require.Equal(t, "/api/roles/permissions", <-requests)
			}
		})
	}
}
