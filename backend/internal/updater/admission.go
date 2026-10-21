package updater

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/getarcaneapp/arcane/types/v2/scheduler"
	"github.com/moby/moby/api/types/container"
	"go.getarcane.app/docker"
	kit "go.getarcane.app/kit/pkg"
	"go.getarcane.app/updater/labels"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/timeouts"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/runs"
)

type updateLeases struct {
	mu      sync.Mutex
	held    map[scheduler.AdmissionKey]bool
	leases  []*runs.Lease
	handoff bool
}

func updateAdmissionKeys(containers []container.Summary, targetIDs []string) []scheduler.AdmissionKey {
	identity := func(summary container.Summary) string {
		if project := strings.ToLower(strings.TrimSpace(docker.ComposeProjectLabel(summary.Labels))); project != "" {
			return "project:" + project
		}
		ownNames := slices.DeleteFunc(slices.Clone(summary.Names), func(name string) bool {
			return strings.Contains(strings.TrimPrefix(name, "/"), "/")
		})
		return "container:" + cmp.Or(docker.ContainerNameFromNames(ownNames), summary.ID)
	}
	lookup := func(ref string) (container.Summary, bool) {
		if index := slices.IndexFunc(containers, func(summary container.Summary) bool {
			return slices.ContainsFunc(summary.Names, func(name string) bool {
				return strings.TrimPrefix(name, "/") == ref
			})
		}); index >= 0 {
			return containers[index], true
		}
		index := slices.IndexFunc(containers, func(summary container.Summary) bool {
			return ref != "" && strings.HasPrefix(summary.ID, ref)
		})
		if index < 0 {
			return container.Summary{}, false
		}
		return containers[index], true
	}

	parent := map[string]string{}
	var root func(id string) string
	root = func(id string) string {
		next, ok := parent[id]
		if !ok || next == id {
			parent[id] = id
			return id
		}
		parent[id] = root(next)
		return parent[id]
	}
	union := func(a, b string) {
		parent[root(a)] = root(b)
	}
	for _, summary := range containers {
		self := identity(summary)
		root(self)
		if strings.HasPrefix(self, "container:") {
			union(self, "id:"+summary.ID)
		}
		networkMode := container.NetworkMode(summary.HostConfig.NetworkMode)
		peers := kit.TrimNonEmpty(strings.Split(summary.Labels[labels.LabelDependsOn], ","))
		if networkMode.IsContainer() {
			peers = append(peers, networkMode.ConnectedContainer())
		}
		for _, name := range summary.Names {
			if linker, _, linked := strings.Cut(strings.TrimPrefix(name, "/"), "/"); linked {
				peers = append(peers, linker)
			}
		}
		for _, ref := range peers {
			if peer, found := lookup(ref); found {
				union(self, identity(peer))
			}
		}
	}

	targetRoots := map[string]bool{}
	for _, id := range kit.TrimNonEmpty(targetIDs) {
		if project, isProject := strings.CutPrefix(id, "compose:"); isProject {
			targetRoots[root("project:"+strings.ToLower(strings.TrimSpace(project)))] = true
			continue
		}
		if summary, found := lookup(id); found {
			targetRoots[root(identity(summary))] = true
			continue
		}
		targetRoots[root("id:"+id)] = true
		targetRoots[root("container:"+strings.TrimPrefix(id, "/"))] = true
	}
	identities := slices.Sorted(maps.Keys(parent))
	keys := make([]scheduler.AdmissionKey, 0, len(identities))
	for _, id := range identities {
		if targetRoots[root(id)] {
			keys = append(keys, scheduler.AdmissionKey{Scope: "updater", ID: id})
		}
	}
	return keys
}

func (s *UpdaterService) acquireUpdate(ctx context.Context, containerIDs []string, wait bool) (context.Context, func(), error) {
	if len(kit.TrimNonEmpty(containerIDs)) == 0 {
		return s.admitUpdate(ctx, nil, wait)
	}
	containers, _, _, _, err := s.deps.Docker.GetAllContainers(ctx)
	if err != nil {
		return ctx, nil, fmt.Errorf("list containers: %w", err)
	}
	return s.admitUpdate(ctx, updateAdmissionKeys(containers, containerIDs), wait)
}

func (s *UpdaterService) admitUpdate(ctx context.Context, keys []scheduler.AdmissionKey, wait bool) (context.Context, func(), error) {
	owner, nested := ctx.Value(updateAdmissionKey{}).(*updateLeases)
	if !nested {
		owner = &updateLeases{held: map[scheduler.AdmissionKey]bool{}}
	}
	owner.mu.Lock()
	defer owner.mu.Unlock()
	keys = slices.DeleteFunc(keys, func(key scheduler.AdmissionKey) bool {
		return owner.held[key]
	})
	waitCtx, cancel := context.WithTimeout(ctx, timeouts.DefaultActivitySlotWait)
	defer cancel()
	acquired := make([]*runs.Lease, 0, len(keys))
	for _, key := range keys {
		lease, acquireErr := func() (*runs.Lease, error) {
			if wait {
				return s.admission.Acquire(waitCtx, key)
			}
			lease, admitted, tryErr := s.admission.TryAcquire(ctx, key)
			if tryErr == nil && !admitted {
				return nil, errUpdateBusy
			}
			return lease, tryErr
		}()
		if acquireErr != nil {
			for _, lease := range slices.Backward(acquired) {
				lease.Release(ctx)
			}
			if ctx.Err() == nil && errors.Is(acquireErr, context.DeadlineExceeded) {
				return ctx, nil, common.Classify(common.ErrTimeout, errors.New("timed out waiting for a conflicting container update to finish"))
			}
			return ctx, nil, acquireErr
		}
		acquired = append(acquired, lease)
	}
	for _, key := range keys {
		owner.held[key] = true
	}
	owner.leases = append(owner.leases, acquired...)
	if nested {
		return ctx, func() {}, nil
	}
	return context.WithValue(ctx, updateAdmissionKey{}, owner), func() {
		owner.mu.Lock()
		leases := slices.Clone(owner.leases)
		handoff := owner.handoff
		owner.mu.Unlock()
		free := func() {
			for _, lease := range slices.Backward(leases) {
				lease.Release(ctx)
			}
		}
		if handoff {
			time.AfterFunc(timeouts.DefaultDeployWait, free)
			return
		}
		free()
	}, nil
}
