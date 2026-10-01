package runs

import (
	"context"
	"log/slog"
	"sync"
	"time"
	"uuid"

	"emperror.dev/errors"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/francis"
	st "github.com/getarcaneapp/arcane/types/v2/scheduler"
	"github.com/italypaleale/francis/actor"
	kit "go.getarcane.app/kit/pkg"
)

const admissionTypeInternal = "resource-admission"

type Admission struct {
	service *actor.Service
	epoch   string
	mu      sync.Mutex
	pending map[string]map[string]st.AdmissionCommand
}
type Lease struct {
	admission *Admission
	id        string
	token     string
	once      sync.Once
}
type admissionActorInternal struct {
	id      string
	service *actor.Service
}

func NewAdmission(service *actor.Service, epoch string) *Admission {
	return &Admission{service: service, epoch: epoch, pending: make(map[string]map[string]st.AdmissionCommand)}
}

func (a *Admission) Register(runtime *francis.Runtime) error {
	return runtime.RegisterActor(admissionTypeInternal, func(id string, service *actor.Service) actor.Actor {
		return &admissionActorInternal{id: id, service: service}
	})
}

func (a *Admission) TryAcquire(ctx context.Context, key st.AdmissionKey) (*Lease, bool, error) {
	id := kit.SHA256Hex(key.Scope + "\x00" + key.ID)
	token := uuid.New().String()
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	if err := a.retryReleasesInternal(ctx, id); err != nil {
		return nil, false, err
	}
	// Finish admission after submission so cancellation cannot orphan a lease.
	invokeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	envelope, err := a.service.Invoke(invokeCtx, admissionTypeInternal, id, "acquire", st.AdmissionCommand{Epoch: a.epoch, Token: token})
	if err != nil {
		a.releaseInternal(ctx, id, st.AdmissionCommand{Epoch: a.epoch, Token: token})
		return nil, false, err
	}
	var acquired bool
	if err := envelope.Decode(&acquired); err != nil {
		a.releaseInternal(ctx, id, st.AdmissionCommand{Epoch: a.epoch, Token: token})
		return nil, false, err
	}
	if !acquired {
		return nil, false, nil
	}
	return &Lease{admission: a, id: id, token: token}, true, nil
}

func (l *Lease) Release(ctx context.Context) {
	if l == nil {
		return
	}
	l.once.Do(func() {
		l.admission.releaseInternal(ctx, l.id, st.AdmissionCommand{Epoch: l.admission.epoch, Token: l.token})
	})
}

func (a *Admission) releaseInternal(ctx context.Context, id string, command st.AdmissionCommand) {
	a.mu.Lock()
	if a.pending[id] == nil {
		a.pending[id] = make(map[string]st.AdmissionCommand)
	}
	a.pending[id][command.Token] = command
	a.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := a.retryReleasesInternal(ctx, id); err != nil {
		slog.ErrorContext(ctx, "Resource admission release failed; will retry before further admission", "resource", id, "error", err)
	}
}

func (a *Admission) retryReleasesInternal(ctx context.Context, id string) error {
	a.mu.Lock()
	pending := make([]st.AdmissionCommand, 0, len(a.pending[id]))
	for _, command := range a.pending[id] {
		pending = append(pending, command)
	}
	a.mu.Unlock()
	for _, command := range pending {
		if _, err := a.service.Invoke(ctx, admissionTypeInternal, id, "release", command); err != nil {
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

func (a *admissionActorInternal) Invoke(ctx context.Context, method string, data actor.Envelope) (any, error) {
	var state st.AdmissionState
	err := a.service.GetState(ctx, admissionTypeInternal, a.id, &state)
	if err != nil && !errors.Is(err, actor.ErrStateNotFound) {
		return nil, err
	}
	var command st.AdmissionCommand
	if err := data.Decode(&command); err != nil {
		return nil, err
	}
	switch method {
	case "acquire":
		if state.Epoch == command.Epoch && state.Token != "" {
			return false, nil
		}
		state = st.AdmissionState{Epoch: command.Epoch, Token: command.Token, AcquiredAt: time.Now().UTC()}
	case "release":
		if state.Epoch != command.Epoch || state.Token != command.Token {
			return false, nil
		}
		state.Token = ""
	default:
		return nil, errors.New("unknown resource admission command")
	}
	if err := a.service.SetState(ctx, admissionTypeInternal, a.id, state, nil); err != nil {
		return nil, err
	}
	return true, nil
}
