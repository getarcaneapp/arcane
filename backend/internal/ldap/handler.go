package ldap

import (
	"context"

	"getarcane.dev/arcane/v3/pkg/handlerutil"
	"getarcane.dev/types/v3"
)

type (
	LdapHandler struct {
		service *LdapService
	}
)

func (h *LdapHandler) Sync(ctx context.Context, _ *struct{}) (*handlerutil.Out[types.MessageResponse], error) {
	if err := h.service.SyncAll(ctx); err != nil {
		return nil, handlerutil.APIError(err)
	}
	return handlerutil.MessageOutput("LDAP sync completed", ""), nil
}
