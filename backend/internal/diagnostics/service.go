package diagnostics

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime"
	"runtime/debug"
	"runtime/pprof"
	"runtime/trace"
	"sync"
	"time"

	"github.com/getarcaneapp/arcane/types/v2/system"
	kit "go.getarcane.app/kit/pkg"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v2/internal/environment"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/runs"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/francis"
)

const (
	recentGCPauseSamples     = 16
	goroutineLeakProfileName = "goroutineleak"

	// maxActorDetailRowsInternal caps the instances and failures listed per actor type.
	maxActorDetailRowsInternal = 10
)

// DiagnosticsService gathers Go runtime, memory, and garbage-collector
// statistics for the diagnostics endpoints. It holds no external dependencies;
// WebSocket metrics and worker-goroutine counts are merged in at the handler
// layer to avoid an import cycle with the api/ws package.
type DiagnosticsService struct {
	startedAt    time.Time
	actors       *francis.Runtime
	coordinator  *runs.Coordinator
	environments *environment.EnvironmentService

	leakMu        sync.Mutex
	leakScannedAt time.Time
}

func NewDiagnosticsService(actors *francis.Runtime, coordinator *runs.Coordinator, environments *environment.EnvironmentService) *DiagnosticsService {
	return &DiagnosticsService{startedAt: time.Now(), actors: actors, coordinator: coordinator, environments: environments}
}

func (s *DiagnosticsService) Collect() (system.RuntimeInfo, system.MemoryInfo, system.GCInfo) {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	var gc debug.GCStats
	debug.ReadGCStats(&gc)

	s.leakMu.Lock()
	leakScannedAt := s.leakScannedAt
	s.leakMu.Unlock()

	rt := system.RuntimeInfo{
		Goroutines:       runtime.NumGoroutine(),
		LeakedGoroutines: leakedGoroutineCountInternal(),
		LeakScannedAt:    leakScannedAt,
		GOMAXPROCS:       runtime.GOMAXPROCS(0),
		NumCPU:           runtime.NumCPU(),
		GoVersion:        runtime.Version(),
		OS:               runtime.GOOS,
		Arch:             runtime.GOARCH,
		NumCgoCall:       runtime.NumCgoCall(),
		UptimeSeconds:    int64(time.Since(s.startedAt).Seconds()),
	}

	mi := system.MemoryInfo{
		Alloc:         mem.Alloc,
		TotalAlloc:    mem.TotalAlloc,
		Sys:           mem.Sys,
		HeapAlloc:     mem.HeapAlloc,
		HeapSys:       mem.HeapSys,
		HeapInuse:     mem.HeapInuse,
		HeapIdle:      mem.HeapIdle,
		HeapReleased:  mem.HeapReleased,
		HeapObjects:   mem.HeapObjects,
		StackInuse:    mem.StackInuse,
		StackSys:      mem.StackSys,
		MSpanInuse:    mem.MSpanInuse,
		MCacheInuse:   mem.MCacheInuse,
		NextGC:        mem.NextGC,
		NumGC:         mem.NumGC,
		NumForcedGC:   mem.NumForcedGC,
		GCCPUFraction: mem.GCCPUFraction,
	}

	// gc.Pause is ordered most-recent-first; cap the slice we expose.
	pauses := gc.Pause
	if len(pauses) > recentGCPauseSamples {
		pauses = pauses[:recentGCPauseSamples]
	}
	recent := make([]int64, len(pauses))
	for i, p := range pauses {
		recent[i] = p.Nanoseconds()
	}

	gi := system.GCInfo{
		LastGC:         gc.LastGC,
		NumGC:          gc.NumGC,
		PauseTotalNs:   gc.PauseTotal.Nanoseconds(),
		RecentPausesNs: recent,
	}

	return rt, mi, gi
}

// ScanGoroutineLeaks runs a leak-detection GC and returns the leaked-goroutine
// pprof text (debug=1). debug=2 would dump every goroutine, not only leaks.
func (s *DiagnosticsService) ScanGoroutineLeaks() (system.GoroutineLeakReport, error) {
	p := pprof.Lookup(goroutineLeakProfileName)
	if p == nil {
		return system.GoroutineLeakReport{}, errors.New("goroutineleak profile is not available")
	}

	var buf bytes.Buffer
	if err := p.WriteTo(&buf, 1); err != nil {
		return system.GoroutineLeakReport{}, fmt.Errorf("write goroutineleak profile: %w", err)
	}

	now := time.Now().UTC()
	s.leakMu.Lock()
	s.leakScannedAt = now
	s.leakMu.Unlock()

	return system.GoroutineLeakReport{
		Count:     p.Count(),
		Profile:   buf.String(),
		ScannedAt: now,
	}, nil
}

// Profile captures a pprof profile in the given debug format. The CPU profile
// and execution trace sample for seconds or until ctx ends.
func (s *DiagnosticsService) Profile(ctx context.Context, name string, seconds, debugLevel int) ([]byte, error) {
	var buf bytes.Buffer
	sample := func() {
		timer := time.NewTimer(time.Duration(seconds) * time.Second)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
		}
	}
	switch name {
	case "profile":
		if err := pprof.StartCPUProfile(&buf); err != nil {
			return nil, fmt.Errorf("start CPU profile: %w", err)
		}
		sample()
		pprof.StopCPUProfile()
	case "trace":
		if err := trace.Start(&buf); err != nil {
			return nil, fmt.Errorf("start trace: %w", err)
		}
		sample()
		trace.Stop()
	default:
		p := pprof.Lookup(name)
		if p == nil {
			return nil, fmt.Errorf("unknown profile %q", name)
		}
		if err := p.WriteTo(&buf, debugLevel); err != nil {
			return nil, fmt.Errorf("write %s profile: %w", name, err)
		}
	}
	return buf.Bytes(), ctx.Err()
}

// leakedGoroutineCountInternal returns the last-known leak count without
// triggering a leak-detection GC.
func leakedGoroutineCountInternal() int {
	p := pprof.Lookup(goroutineLeakProfileName)
	if p == nil {
		return 0
	}
	return p.Count()
}

// CollectActors reports this process's Francis host, actor types, and run queue.
func (s *DiagnosticsService) CollectActors(ctx context.Context) (system.ActorDiagnostics, error) {
	d := system.ActorDiagnostics{EnvironmentID: environment.LocalEnvironmentID, Runs: map[string]int{}, CheckedAt: time.Now().UTC()}
	select {
	case <-s.actors.Ready():
		d.Ready = true
	default:
		return d, nil
	}

	store, err := s.actors.Store()
	if err != nil {
		return d, err
	}
	db := store.WithContext(ctx)
	var hosts []actorHostModel
	if err = db.Order("host_id").Find(&hosts).Error; err != nil {
		return d, fmt.Errorf("list actor hosts: %w", err)
	}
	for _, host := range hosts {
		d.Hosts = append(d.Hosts, system.ActorHostInfo{HostID: host.HostID, Address: host.Address, LastHealthCheck: time.Time(host.LastHealthCheck)})
	}

	// Records names coordinator actors, so load it before resolving instance names.
	records, err := s.coordinator.Records(ctx)
	if err != nil {
		return d, err
	}
	for _, record := range records {
		for _, run := range record.Runs {
			d.Runs[string(run.Status)]++
		}
	}

	d.ActorTypes, err = s.collectActorTypesInternal(db)
	return d, err
}

// collectActorTypesInternal reports counts, recent instances, and dead jobs per registered actor type.
func (s *DiagnosticsService) collectActorTypesInternal(db *gorm.DB) ([]system.ActorTypeInfo, error) {
	var actorTypes []actorTypeModel
	err := db.Table(francis.TablePrefix + "_host_actor_types").
		Select("actor_type, MAX(actor_concurrency_limit) AS concurrency_limit").
		Group("actor_type").Order("actor_type").Scan(&actorTypes).Error
	if err != nil {
		return nil, fmt.Errorf("list actor types: %w", err)
	}
	var active, states, alarms, leased, terminal []actorCountModel
	var instances []activeActorModel
	var failures []terminalJobModel
	err = errors.Join(
		db.Table(francis.TablePrefix+"_active_actors").Select("actor_type, COUNT(*) AS count").Group("actor_type").Scan(&active).Error,
		db.Table(francis.TablePrefix+"_actor_state").Select("actor_type, COUNT(*) AS count").Group("actor_type").Scan(&states).Error,
		db.Table(francis.TablePrefix+"_alarms").Select("actor_type, alarm_kind AS kind, COUNT(*) AS count, MIN(alarm_due_time) AS next_due").Group("actor_type, alarm_kind").Scan(&alarms).Error,
		db.Table(francis.TablePrefix+"_alarms").Select("actor_type, COUNT(*) AS count").Where("alarm_lease_id IS NOT NULL").Group("actor_type").Scan(&leased).Error,
		db.Table(francis.TablePrefix+"_terminal_jobs").Select("actor_type, job_status AS kind, COUNT(*) AS count").Group("actor_type, job_status").Scan(&terminal).Error,
		db.Order("actor_activation DESC").Limit(500).Find(&instances).Error,
		db.Where("job_status = ?", "dead").Order("ended_at DESC").Limit(100).Find(&failures).Error,
	)
	if err != nil {
		return nil, fmt.Errorf("collect actor activity: %w", err)
	}

	out := make([]system.ActorTypeInfo, len(actorTypes))
	byType := make(map[string]*system.ActorTypeInfo, len(actorTypes))
	for i, actorType := range actorTypes {
		out[i] = system.ActorTypeInfo{ActorType: actorType.ActorType, ConcurrencyLimit: actorType.ConcurrencyLimit, Instances: []system.ActorInstance{}, RecentFailures: []system.ActorJobFailure{}}
		byType[actorType.ActorType] = &out[i]
	}
	addCountsInternal(byType, active, func(info *system.ActorTypeInfo, row actorCountModel) { info.ActiveActors = row.Count })
	addCountsInternal(byType, states, func(info *system.ActorTypeInfo, row actorCountModel) { info.StoredStates = row.Count })
	addCountsInternal(byType, leased, func(info *system.ActorTypeInfo, row actorCountModel) { info.LeasedAlarms = row.Count })
	addCountsInternal(byType, alarms, func(info *system.ActorTypeInfo, row actorCountModel) {
		if row.Kind == "job" {
			info.PendingJobs += row.Count
		} else {
			info.PendingAlarms += row.Count
		}
		if due := time.Time(row.NextDue); !due.IsZero() && (info.NextAlarmAt.IsZero() || due.Before(info.NextAlarmAt)) {
			info.NextAlarmAt = due
		}
	})
	addCountsInternal(byType, terminal, func(info *system.ActorTypeInfo, row actorCountModel) {
		switch row.Kind {
		case "dead":
			info.DeadJobs += row.Count
		case "completed":
			info.CompletedJobs += row.Count
		}
	})
	for _, row := range instances {
		if info := byType[row.ActorType]; info != nil && len(info.Instances) < maxActorDetailRowsInternal {
			info.Instances = append(info.Instances, system.ActorInstance{ActorID: row.ActorID, Name: s.actors.ActorName(row.ActorID), HostID: row.HostID, ActivatedAt: time.Time(row.Activation)})
		}
	}
	for _, row := range failures {
		if info := byType[row.ActorType]; info != nil && len(info.RecentFailures) < maxActorDetailRowsInternal {
			info.RecentFailures = append(info.RecentFailures, system.ActorJobFailure{
				JobID: row.JobID, ActorID: row.ActorID, Name: s.actors.ActorName(row.ActorID), Method: row.JobMethod,
				Attempts: row.Attempts, LastError: kit.FromPtr(row.LastError), FailedAt: time.Time(row.EndedAt),
			})
		}
	}
	return out, nil
}

// addCountsInternal applies grouped rows to their registered actor type.
func addCountsInternal(byType map[string]*system.ActorTypeInfo, rows []actorCountModel, apply func(*system.ActorTypeInfo, actorCountModel)) {
	for _, row := range rows {
		if info := byType[row.ActorType]; info != nil {
			apply(info, row)
		}
	}
}

// CollectAllActors gathers actor diagnostics from the local and every active remote environment.
func (s *DiagnosticsService) CollectAllActors(ctx context.Context) []system.ActorDiagnostics {
	local, err := s.CollectActors(ctx)
	if err != nil {
		local.Error = err.Error()
	}
	if env, envErr := s.environments.GetEnvironmentByIDCached(ctx, environment.LocalEnvironmentID); envErr == nil {
		local.EnvironmentName = env.Name
	}

	remotes, err := s.environments.ListActiveRemoteEnvironments(ctx)
	if err != nil {
		slog.WarnContext(ctx, "Failed to list environments for actor diagnostics", "error", err)
	}
	out := make([]system.ActorDiagnostics, len(remotes)+1)
	out[0] = local
	var wg sync.WaitGroup
	for i, env := range remotes {
		wg.Go(func() {
			var d system.ActorDiagnostics
			if proxyErr := s.environments.ProxyJSONRequestForEnvironment(ctx, env, http.MethodGet, "/api/environments/0/diagnostics/actors", nil, &d); proxyErr != nil {
				d = system.ActorDiagnostics{Error: proxyErr.Error(), CheckedAt: time.Now().UTC()}
			}
			d.EnvironmentID, d.EnvironmentName = env.ID, env.Name
			out[i+1] = d
		})
	}
	wg.Wait()
	return out
}
