package runtimestatus

import (
	"cmp"
	"context"
	"log/slog"
	"maps"
	"net/http"
	"slices"
	"sync"
	"time"

	"go.uber.org/fx"

	"kit.getarcane.app"
	"kit.getarcane.app/docker/streams/agg"

	"getarcane.dev/arcane/v3/internal/environment"
	"getarcane.dev/arcane/v3/internal/event"
	"getarcane.dev/arcane/v3/internal/podman"
	"getarcane.dev/arcane/v3/internal/swarm"
	"getarcane.dev/arcane/v3/pkg/authz"
	"getarcane.dev/types/v3"
)

type (
	RuntimeStatusService struct {
		environmentService *environment.EnvironmentService
		eventService       *event.EventService
		swarmService       *swarm.SwarmService
		podmanService      *podman.Service
		hub                *agg.Hub[[]types.EnvironmentRuntimeStatus]
	}

	RuntimeStatusServiceParams struct {
		fx.In

		EnvironmentService *environment.EnvironmentService
		EventService       *event.EventService
		SwarmService       *swarm.SwarmService
		PodmanService      *podman.Service
	}

	tracker struct {
		service   *RuntimeStatusService
		publish   func([]types.EnvironmentRuntimeStatus)
		wake      chan struct{}
		pending   map[string]bool
		sessions  map[string]string
		revisions map[string]types.EnvironmentChanges
		statuses  map[string]types.EnvironmentRuntimeStatus
		mu        sync.Mutex
	}

	subscriber struct {
		ctx         context.Context
		permissions *authz.PermissionSet
		events      chan<- types.EnvironmentStreamEvent
		fingerprint uint64
		sent        bool
	}
)

const (
	hubKey           = "runtime"
	streamEventType  = "runtime"
	resolveTimeout   = 15 * time.Second
	swarmStatusPath  = "/api/agents/0/swarm/status"
	podmanStatusPath = "/api/agents/0/podman/status"
)

var (
	statusChangeKinds       = []string{"all", "swarm"}
	podmanStatusChangeKinds = []string{"all", "swarm", "podman", "container"}
)

func NewRuntimeStatusService(p RuntimeStatusServiceParams) *RuntimeStatusService {
	return &RuntimeStatusService{
		environmentService: p.EnvironmentService,
		eventService:       p.EventService,
		swarmService:       p.SwarmService,
		podmanService:      p.PodmanService,
		hub:                &agg.Hub[[]types.EnvironmentRuntimeStatus]{},
	}
}

func (s *RuntimeStatusService) RunStreamProducer(ctx context.Context, ps *authz.PermissionSet, events chan<- types.EnvironmentStreamEvent) {
	sub := &subscriber{
		ctx:         ctx,
		permissions: ps,
		events:      events,
	}
	s.hub.Subscribe(ctx, hubKey, s.track, sub.deliver)
}

func (s *RuntimeStatusService) track(ctx context.Context, publish func([]types.EnvironmentRuntimeStatus)) {
	t := &tracker{
		service:   s,
		publish:   publish,
		wake:      make(chan struct{}, 1),
		pending:   map[string]bool{},
		sessions:  map[string]string{},
		revisions: map[string]types.EnvironmentChanges{},
		statuses:  map[string]types.EnvironmentRuntimeStatus{},
	}
	runtimeChanges, unsubscribeRuntime := s.environmentService.SubscribeRuntimeChanges()
	defer unsubscribeRuntime()
	defer s.eventService.Changes.Subscribe(t.observe)()

	t.sync(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-runtimeChanges:
			t.sync(ctx)
		case <-t.wake:
			t.resolvePending(ctx)
		}
	}
}

func (s *RuntimeStatusService) resolve(ctx context.Context, environmentID string) types.EnvironmentRuntimeStatus {
	resolveCtx, cancel := context.WithTimeout(ctx, resolveTimeout)
	defer cancel()
	if environmentID == environment.LocalEnvironmentID {
		swarmStatus, err := s.swarmService.GetStatus(resolveCtx)
		if err != nil {
			slog.DebugContext(ctx, "Failed to read local swarm status", "error", err)
		}
		return types.EnvironmentRuntimeStatus{
			Swarm:         kit.Ternary(err == nil, &swarmStatus, nil),
			Podman:        new(s.podmanService.Status(resolveCtx)),
			EnvironmentID: environmentID,
		}
	}

	var wg sync.WaitGroup
	var swarmStatus *types.SwarmRuntimeStatus
	var podmanStatus *types.PodmanStatus
	wg.Go(func() {
		swarmStatus = proxyStatus[types.SwarmRuntimeStatus](resolveCtx, s.environmentService, environmentID, swarmStatusPath)
	})
	wg.Go(func() {
		podmanStatus = proxyStatus[types.PodmanStatus](resolveCtx, s.environmentService, environmentID, podmanStatusPath)
	})
	wg.Wait()
	return types.EnvironmentRuntimeStatus{
		Swarm:         swarmStatus,
		Podman:        podmanStatus,
		EnvironmentID: environmentID,
	}
}

func proxyStatus[T any](ctx context.Context, environmentService *environment.EnvironmentService, environmentID, path string) *T {
	var out types.ApiResponse[T]
	if err := environmentService.ProxyJSONRequest(ctx, environmentID, http.MethodGet, path, nil, &out); err != nil {
		slog.DebugContext(ctx, "Failed to read environment runtime status", "environmentId", environmentID, "path", path, "error", err)
		return nil
	}
	return &out.Data
}

func (t *tracker) sync(ctx context.Context) {
	envs, err := t.service.environmentService.ListVisibleEnvironments(ctx)
	if err != nil {
		if ctx.Err() == nil {
			slog.WarnContext(ctx, "Runtime status tracker failed to list environments", "error", err)
		}
		return
	}
	connections := make(map[string]string, len(envs))
	for _, env := range envs {
		live := t.service.environmentService.ApplyRuntimeState(env)
		if live.Enabled && (live.ID == environment.LocalEnvironmentID || kit.FromPtr(live.Connected)) {
			connections[live.ID] = kit.FromPtr(live.AgentSessionID)
		}
	}

	t.mu.Lock()
	if t.reconcile(connections) {
		t.publish(t.snapshotLocked())
	}
	t.mu.Unlock()
	t.resolvePending(ctx)
}

func (t *tracker) reconcile(connections map[string]string) bool {
	removed := false
	for environmentID := range t.sessions {
		if _, connected := connections[environmentID]; connected {
			continue
		}
		_, hadStatus := t.statuses[environmentID]
		removed = removed || hadStatus
		delete(t.sessions, environmentID)
		delete(t.revisions, environmentID)
		delete(t.statuses, environmentID)
		delete(t.pending, environmentID)
	}
	for environmentID, session := range connections {
		if known, tracked := t.sessions[environmentID]; tracked && known == session {
			continue
		}
		t.sessions[environmentID] = session
		t.revisions[environmentID] = t.service.eventService.EnvironmentChanges(environmentID)
		t.pending[environmentID] = true
	}
	return removed
}

func (t *tracker) observe(changes types.EnvironmentChanges) {
	t.mu.Lock()
	defer t.mu.Unlock()
	previous, tracked := t.revisions[changes.EnvironmentID]
	if !tracked {
		return
	}
	t.revisions[changes.EnvironmentID] = changes
	if !statusMayHaveChanged(previous, changes, kit.FromPtr(t.statuses[changes.EnvironmentID].Podman).Detected) {
		return
	}
	t.pending[changes.EnvironmentID] = true
	select {
	case t.wake <- struct{}{}:
	default:
	}
}

func (t *tracker) resolvePending(ctx context.Context) {
	t.mu.Lock()
	environmentIDs := slices.Sorted(maps.Keys(t.pending))
	clear(t.pending)
	t.mu.Unlock()

	var wg sync.WaitGroup
	for _, environmentID := range environmentIDs {
		wg.Go(func() {
			t.apply(ctx, t.service.resolve(ctx, environmentID))
		})
	}
	wg.Wait()
}

func (t *tracker) apply(ctx context.Context, result types.EnvironmentRuntimeStatus) {
	if ctx.Err() != nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.store(result)
	t.publish(t.snapshotLocked())
}

func (t *tracker) store(result types.EnvironmentRuntimeStatus) {
	if _, tracked := t.sessions[result.EnvironmentID]; !tracked || result.Swarm == nil && result.Podman == nil {
		return
	}
	t.statuses[result.EnvironmentID] = result
}

func (t *tracker) snapshotLocked() []types.EnvironmentRuntimeStatus {
	return slices.SortedFunc(maps.Values(t.statuses), compareStatuses)
}

func compareStatuses(a, b types.EnvironmentRuntimeStatus) int {
	return cmp.Compare(a.EnvironmentID, b.EnvironmentID)
}

func statusMayHaveChanged(previous, current types.EnvironmentChanges, podmanDetected bool) bool {
	if previous.Epoch != current.Epoch {
		return true
	}
	for _, kind := range kit.Ternary(podmanDetected, podmanStatusChangeKinds, statusChangeKinds) {
		if current.Kinds[kind] > previous.Kinds[kind] {
			return true
		}
	}
	return false
}

func visibleStatuses(statuses []types.EnvironmentRuntimeStatus, permissions *authz.PermissionSet) []types.EnvironmentRuntimeStatus {
	visible := make([]types.EnvironmentRuntimeStatus, 0, len(statuses))
	for _, status := range statuses {
		swarmStatus := kit.Ternary(permissions.Allows(authz.PermSwarmRead, status.EnvironmentID), status.Swarm, nil)
		podmanStatus := kit.Ternary(permissions.Allows(authz.PermPodmanRead, status.EnvironmentID), status.Podman, nil)
		if swarmStatus == nil && podmanStatus == nil {
			continue
		}
		visible = append(visible, types.EnvironmentRuntimeStatus{
			Swarm:         swarmStatus,
			Podman:        podmanStatus,
			EnvironmentID: status.EnvironmentID,
		})
	}
	return visible
}

func (sub *subscriber) deliver(statuses []types.EnvironmentRuntimeStatus) bool {
	visible := visibleStatuses(statuses, sub.permissions)
	fingerprint := kit.Fingerprint(visible)
	if sub.sent && fingerprint == sub.fingerprint {
		return true
	}
	sub.fingerprint = fingerprint
	sub.sent = true
	return agg.Send(sub.ctx, sub.events, types.EnvironmentStreamEvent{
		Timestamp: time.Now(),
		Type:      streamEventType,
		Runtime:   visible,
	})
}
