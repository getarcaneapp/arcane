package cmdutil

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getarcaneapp/arcane/cli/v3/internal/client"
	"github.com/getarcaneapp/arcane/cli/v3/internal/types"
)

func TestResourceRefFetch(t *testing.T) {
	type detail struct{ ID, Name, Extra string }
	type summary struct{ ID, Name string }
	for _, tt := range []struct {
		name, identifier, detailBody, searchBody, wantErr string
		noIdentifier                                      bool
		wantRequests                                      []string
	}{
		{name: "complete direct lookup", identifier: "id-1", wantRequests: []string{"/resources/id-1"}},
		{name: "summary fetches canonical detail", identifier: "arcane", wantRequests: []string{"/resources/arcane", "/resources?search=arcane&limit=-1", "/resources/id-1"}},
		{
			name: "detail API failure", identifier: "arcane", detailBody: `{"success":false,"error":"denied"}`, wantErr: "API error: denied",
			wantRequests: []string{"/resources/arcane", "/resources?search=arcane&limit=-1", "/resources/id-1"},
		},
		{
			name: "not found", identifier: "absent", searchBody: `{"success":true,"data":[]}`, wantErr: `record "absent" not found`,
			wantRequests: []string{"/resources/absent", "/resources?search=absent&limit=-1"},
		},
		{name: "empty identifier", wantErr: "record identifier is required"},
		{
			name: "missing detail identifier", identifier: "arcane", noIdentifier: true, wantErr: "requires GetPath and IdentifierOf",
			wantRequests: []string{"/resources/arcane", "/resources?search=arcane&limit=-1"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			requests := make(chan string, 3)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests <- r.URL.RequestURI()
				switch r.URL.Path {
				case "/resources/id-1":
					if tt.detailBody != "" {
						_, _ = fmt.Fprint(w, tt.detailBody)
						return
					}
					_, _ = fmt.Fprint(w, `{"success":true,"data":{"ID":"id-1","Name":"arcane","Extra":"full detail"}}`)
				case "/resources":
					if tt.searchBody != "" {
						_, _ = fmt.Fprint(w, tt.searchBody)
						return
					}
					_, _ = fmt.Fprint(w, `{"success":true,"data":[{"ID":"id-1","Name":"arcane"}]}`)
				default:
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer server.Close()
			c, newErr := client.NewUnauthenticated(&types.Config{ServerURL: server.URL})
			require.NoError(t, newErr)
			ref := ResourceRef[detail, summary]{
				Singular: "record", Plural: "records", IDHint: "the record ID", ListCmd: "records list",
				GetPath:      func(_, identifier string) string { return "/resources/" + identifier },
				ListPath:     func(string) string { return "/resources" },
				Matches:      func(item summary, lower, _ string) bool { return strings.ToLower(item.Name) == lower },
				Label:        func(item summary) string { return item.Name },
				Promote:      func(item summary) *detail { return &detail{ID: item.ID, Name: item.Name} },
				IdentifierOf: func(item detail) string { return item.ID },
			}
			if tt.noIdentifier {
				ref.IdentifierOf = nil
			}
			result, fetchErr := ref.Fetch(t.Context(), c, tt.identifier, false)
			if tt.wantErr != "" {
				require.ErrorContains(t, fetchErr, tt.wantErr)
			} else {
				require.NoError(t, fetchErr)
				require.Equal(t, &detail{ID: "id-1", Name: "arcane", Extra: "full detail"}, result)
			}
			require.Len(t, requests, len(tt.wantRequests))
			for _, path := range tt.wantRequests {
				require.Equal(t, path, <-requests)
			}
		})
	}
}
