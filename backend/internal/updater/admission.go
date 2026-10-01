package updater

import (
	"context"

	"emperror.dev/errors"
	schedulertypes "github.com/getarcaneapp/arcane/types/v2/scheduler"
)

type updateAdmissionKeyInternal struct{}

var errUpdateBusyInternal = errors.New("another container update is running")

func (s *UpdaterService) acquireUpdateInternal(ctx context.Context) (context.Context, func(), error) {
	if s.admission == nil || ctx.Value(updateAdmissionKeyInternal{}) == s {
		return ctx, func() {}, nil
	}
	lease, admitted, err := s.admission.TryAcquire(ctx, schedulertypes.AdmissionKey{Scope: "updater"})
	if err != nil {
		return ctx, nil, err
	}
	if !admitted {
		return ctx, nil, errUpdateBusyInternal
	}
	return context.WithValue(ctx, updateAdmissionKeyInternal{}, s), func() { lease.Release(ctx) }, nil
}
