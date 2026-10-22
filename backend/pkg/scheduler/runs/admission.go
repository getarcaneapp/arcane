package runs

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"
	"uuid"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/italypaleale/francis/actor"
	kit "go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/francis"
)

const admissionType = "resource-admission"

type Admission struct {
	service *actor.Service
	epoch   string
	mu      sync.Mutex
	pending map[string]map[string]types.SchedulerAdmissionCommand
}
type Lease struct {
	admission *Admission
	id        string
	token     string
	once      sync.Once
}
type admissionActor struct {
	id      string
	service *actor.Service
}

func NewAdmission(service *actor.Service, epoch string) *Admission {
	return &Admission{service: service, epoch: epoch, pending: make(map[string]map[string]types.SchedulerAdmissionCommand)}
}

func (a *Admission) Register(runtime *francis.Runtime) error {
	return runtime.RegisterActor(admissionType, func(id string, service *actor.Service) actor.Actor {
		return &admissionActor{id: id, service: service}
	})
}

func (a *Admission) TryAcquire(ctx context.Context, key types.SchedulerAdmissionKey) (*Lease, bool, error) {
	id := kit.SHA256Hex(key.Scope + "\x00" + key.ID)
	token := uuid.New().String()
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	if err := a.retryReleases(ctx, id); err != nil {
		return nil, false, err
	}
	// Finish admission after submission so cancellation cannot orphan a lease.
	invokeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	envelope, err := a.service.Invoke(invokeCtx, admissionType, id, "acquire", types.SchedulerAdmissionCommand{Epoch: a.epoch, Token: token})
	if err != nil {
		a.release(ctx, id, types.SchedulerAdmissionCommand{Epoch: a.epoch, Token: token})
		return nil, false, err
	}
	var acquired bool
	if decodeErr := envelope.Decode(&acquired); decodeErr != nil {
		a.release(ctx, id, types.SchedulerAdmissionCommand{Epoch: a.epoch, Token: token})
		return nil, false, decodeErr
	}
	if !acquired {
		return nil, false, nil
	}
	return &Lease{admission: a, id: id, token: token}, true, nil
}

func (a *Admission) Acquire(ctx context.Context, key types.SchedulerAdmissionKey) (*Lease, error) {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		lease, admitted, err := a.TryAcquire(ctx, key)
		if err != nil || admitted {
			return lease, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

func (l *Lease) Release(ctx context.Context) {
	if l == nil {
		return
	}
	l.once.Do(func() {
		l.admission.release(ctx, l.id, types.SchedulerAdmissionCommand{Epoch: l.admission.epoch, Token: l.token})
	})
}

func (a *Admission) release(ctx context.Context, id string, command types.SchedulerAdmissionCommand) {
	a.mu.Lock()
	if a.pending[id] == nil {
		a.pending[id] = make(map[string]types.SchedulerAdmissionCommand)
	}
	a.pending[id][command.Token] = command
	a.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := a.retryReleases(ctx, id); err != nil {
		slog.ErrorContext(ctx, "Resource admission release failed; will retry before further admission", "resource", id, "error", err)
	}
}

func (a *Admission) retryReleases(ctx context.Context, id string) error {
	a.mu.Lock()
	pending := make([]types.SchedulerAdmissionCommand, 0, len(a.pending[id]))
	for _, command := range a.pending[id] {
		pending = append(pending, command)
	}
	a.mu.Unlock()
	for _, command := range pending {
		if _, err := a.service.Invoke(ctx, admissionType, id, "release", command); err != nil {
			return err
		}
		a.mu.Lock()
		delete(a.pending[id], command.Token)
		if len(a.pending[id]) == 0 {
			delete(a.pending, id)
		}
		a.mu.Unlock()
	}
	return nil
}

func (a *admissionActor) Invoke(ctx context.Context, method string, data actor.Envelope) (any, error) {
	var state types.SchedulerAdmissionState
	err := a.service.GetState(ctx, admissionType, a.id, &state)
	if err != nil && !errors.Is(err, actor.ErrStateNotFound) {
		return nil, err
	}
	var command types.SchedulerAdmissionCommand
	if decodeErr := data.Decode(&command); decodeErr != nil {
		return nil, decodeErr
	}
	switch method {
	case "acquire":
		if state.Epoch == command.Epoch && state.Token != "" {
			return false, nil
		}
		state = types.SchedulerAdmissionState{Epoch: command.Epoch, Token: command.Token, AcquiredAt: time.Now().UTC()}
	case "release":
		if state.Epoch != command.Epoch || state.Token != command.Token {
			return false, nil
		}
		state.Token = ""
	default:
		return nil, errors.New("unknown resource admission command")
	}
	if setStateErr := a.service.SetState(ctx, admissionType, a.id, state, nil); setStateErr != nil {
		return nil, setStateErr
	}
	return true, nil
}
