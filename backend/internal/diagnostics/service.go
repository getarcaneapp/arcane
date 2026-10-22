package diagnostics

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime"
	"runtime/debug"
	"runtime/pprof"
	"runtime/trace"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/getarcaneapp/arcane/types/v3"
	kit "go.getarcane.app/kit/pkg"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v3/internal/environment"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/scheduler/runs"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/francis"
)

const (
	recentGCPauseSamples     = 16
	goroutineLeakProfileName = "goroutineleak"
	leakScanCacheTTL         = 30 * time.Second

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

	leakMu     sync.Mutex
	leakReport types.GoroutineLeakReport
}

func NewDiagnosticsService(actors *francis.Runtime, coordinator *runs.Coordinator, environments *environment.EnvironmentService) *DiagnosticsService {
	return &DiagnosticsService{startedAt: time.Now(), actors: actors, coordinator: coordinator, environments: environments}
}

func (s *DiagnosticsService) Collect() (types.RuntimeInfo, types.MemoryInfo, types.GCInfo) {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	var gc debug.GCStats
	debug.ReadGCStats(&gc)

	s.leakMu.Lock()
	leakScannedAt := s.leakReport.ScannedAt
	s.leakMu.Unlock()

	rt := types.RuntimeInfo{
		Goroutines:       runtime.NumGoroutine(),
		LeakedGoroutines: leakedGoroutineCount(),
		LeakScannedAt:    leakScannedAt,
		GOMAXPROCS:       runtime.GOMAXPROCS(0),
		NumCPU:           runtime.NumCPU(),
		GoVersion:        runtime.Version(),
		OS:               runtime.GOOS,
		Arch:             runtime.GOARCH,
		NumCgoCall:       runtime.NumCgoCall(),
		UptimeSeconds:    int64(time.Since(s.startedAt).Seconds()),
	}

	mi := types.MemoryInfo{
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

	gi := types.GCInfo{
		LastGC:         gc.LastGC,
		NumGC:          gc.NumGC,
		PauseTotalNs:   gc.PauseTotal.Nanoseconds(),
		RecentPausesNs: recent,
	}

	return rt, mi, gi
}

// ScanGoroutineLeaks runs a leak-detection GC and returns the leaked-goroutine
// pprof text (debug=1). debug=2 would dump every goroutine, not only leaks.
func (s *DiagnosticsService) ScanGoroutineLeaks() (types.GoroutineLeakReport, error) {
	s.leakMu.Lock()
	defer s.leakMu.Unlock()
	if time.Since(s.leakReport.ScannedAt) < leakScanCacheTTL {
		return s.leakReport, nil
	}

	p := pprof.Lookup(goroutineLeakProfileName)
	if p == nil {
		return types.GoroutineLeakReport{}, errors.New("goroutineleak profile is not available")
	}

	var buf bytes.Buffer
	if err := p.WriteTo(&buf, 1); err != nil {
		return types.GoroutineLeakReport{}, fmt.Errorf("write goroutineleak profile: %w", err)
	}

	s.leakReport = types.GoroutineLeakReport{
		Count:     p.Count(),
		Profile:   buf.String(),
		ScannedAt: time.Now().UTC(),
	}
	return s.leakReport, nil
}

// Goroutines groups the live goroutines by identical stack, largest group first.
// Records that differ only by pprof labels share a stack and merge into one group.
func (s *DiagnosticsService) Goroutines() (types.GoroutineProfile, error) {
	var buf bytes.Buffer
	if err := pprof.Lookup("goroutine").WriteTo(&buf, 1); err != nil {
		return types.GoroutineProfile{}, fmt.Errorf("write goroutine profile: %w", err)
	}

	profile := types.GoroutineProfile{Groups: []types.GoroutineGroup{}, CapturedAt: time.Now().UTC()}
	index := map[string]int{}
	// debug=1 text holds "N @ pcs" records of "#\tpc\tfunc+off\tfile:line" frames, separated by blank lines.
	// A tabwriter aligns the columns, so a run of tabs is one separator.
	for record := range strings.SplitSeq(buf.String(), "\n\n") {
		group := types.GoroutineGroup{Frames: []types.GoroutineFrame{}}
		for line := range strings.Lines(record) {
			fields := strings.FieldsFunc(line, func(r rune) bool { return r == '\t' || r == '\n' })
			if len(fields) == 0 {
				continue
			}
			switch head, _, isHead := strings.Cut(fields[0], " @"); {
			case isHead && !strings.HasPrefix(head, "#"):
				group.Count, _ = strconv.Atoi(head)
			case len(fields) == 4 && fields[0] == "#":
				function, _, _ := strings.Cut(fields[2], "+0x")
				sep := strings.LastIndexByte(fields[3], ':')
				lineNo, err := strconv.Atoi(fields[3][sep+1:])
				hasLine := sep >= 0 && err == nil
				group.Frames = append(group.Frames, types.GoroutineFrame{
					Function: function,
					File:     kit.Ternary(hasLine, fields[3][:max(sep, 0)], fields[3]),
					Line:     kit.Ternary(hasLine, lineNo, 0),
				})
				// Frames run innermost first, so the last non-runtime frame is the entry point.
				group.Label = kit.Ternary(group.Label == "" || !strings.HasPrefix(function, "runtime."), function, group.Label)
			}
		}
		if group.Count == 0 {
			continue
		}
		profile.Total += group.Count
		key := fmt.Sprint(group.Frames)
		if i, ok := index[key]; ok {
			profile.Groups[i].Count += group.Count
			continue
		}
		index[key] = len(profile.Groups)
		profile.Groups = append(profile.Groups, group)
	}
	slices.SortStableFunc(profile.Groups, func(a, b types.GoroutineGroup) int { return cmp.Compare(b.Count, a.Count) })
	return profile, nil
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

// leakedGoroutineCount returns the last-known leak count without
// triggering a leak-detection GC.
func leakedGoroutineCount() int {
	p := pprof.Lookup(goroutineLeakProfileName)
	if p == nil {
		return 0
	}
	return p.Count()
}

// CollectActors reports this process's Francis host, actor types, and run queue.
func (s *DiagnosticsService) CollectActors(ctx context.Context) (types.ActorDiagnostics, error) {
	d := types.ActorDiagnostics{EnvironmentID: environment.LocalEnvironmentID, Runs: map[string]int{}, CheckedAt: time.Now().UTC()}
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
		d.Hosts = append(d.Hosts, types.ActorHostInfo{HostID: host.HostID, Address: host.Address, LastHealthCheck: time.Time(host.LastHealthCheck)})
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

	d.ActorTypes, err = s.collectActorTypes(db)
	return d, err
}

// collectActorTypes reports counts, recent instances, and dead jobs per registered actor type.
func (s *DiagnosticsService) collectActorTypes(db *gorm.DB) ([]types.ActorTypeInfo, error) {
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

	out := make([]types.ActorTypeInfo, len(actorTypes))
	byType := make(map[string]*types.ActorTypeInfo, len(actorTypes))
	for i, actorType := range actorTypes {
		out[i] = types.ActorTypeInfo{ActorType: actorType.ActorType, ConcurrencyLimit: actorType.ConcurrencyLimit, Instances: []types.ActorInstance{}, RecentFailures: []types.ActorJobFailure{}}
		byType[actorType.ActorType] = &out[i]
	}
	addCounts(byType, active, func(info *types.ActorTypeInfo, row actorCountModel) { info.ActiveActors = row.Count })
	addCounts(byType, states, func(info *types.ActorTypeInfo, row actorCountModel) { info.StoredStates = row.Count })
	addCounts(byType, leased, func(info *types.ActorTypeInfo, row actorCountModel) { info.LeasedAlarms = row.Count })
	addCounts(byType, alarms, func(info *types.ActorTypeInfo, row actorCountModel) {
		if row.Kind == "job" {
			info.PendingJobs += row.Count
		} else {
			info.PendingAlarms += row.Count
		}
		if due := time.Time(row.NextDue); !due.IsZero() && (info.NextAlarmAt.IsZero() || due.Before(info.NextAlarmAt)) {
			info.NextAlarmAt = due
		}
	})
	addCounts(byType, terminal, func(info *types.ActorTypeInfo, row actorCountModel) {
		switch row.Kind {
		case "dead":
			info.DeadJobs += row.Count
		case "completed":
			info.CompletedJobs += row.Count
		}
	})
	for _, row := range instances {
		if info := byType[row.ActorType]; info != nil && len(info.Instances) < maxActorDetailRowsInternal {
			info.Instances = append(info.Instances, types.ActorInstance{ActorID: row.ActorID, Name: s.actors.ActorName(row.ActorID), HostID: row.HostID, ActivatedAt: time.Time(row.Activation)})
		}
	}
	for _, row := range failures {
		if info := byType[row.ActorType]; info != nil && len(info.RecentFailures) < maxActorDetailRowsInternal {
			info.RecentFailures = append(info.RecentFailures, types.ActorJobFailure{
				JobID: row.JobID, ActorID: row.ActorID, Name: s.actors.ActorName(row.ActorID), Method: row.JobMethod,
				Attempts: row.Attempts, LastError: kit.FromPtr(row.LastError), FailedAt: time.Time(row.EndedAt),
			})
		}
	}
	return out, nil
}

// addCounts applies grouped rows to their registered actor type.
func addCounts(byType map[string]*types.ActorTypeInfo, rows []actorCountModel, apply func(*types.ActorTypeInfo, actorCountModel)) {
	for _, row := range rows {
		if info := byType[row.ActorType]; info != nil {
			apply(info, row)
		}
	}
}

// CollectAllActors gathers actor diagnostics from the local and every active remote environment.
func (s *DiagnosticsService) CollectAllActors(ctx context.Context) []types.ActorDiagnostics {
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
	out := make([]types.ActorDiagnostics, len(remotes)+1)
	out[0] = local
	var wg sync.WaitGroup
	for i, env := range remotes {
		wg.Go(func() {
			var d types.ActorDiagnostics
			if proxyErr := s.environments.ProxyJSONRequestForEnvironment(ctx, env, http.MethodGet, "/api/environments/0/diagnostics/actors", nil, &d); proxyErr != nil {
				d = types.ActorDiagnostics{Error: proxyErr.Error(), CheckedAt: time.Now().UTC()}
			}
			d.EnvironmentID, d.EnvironmentName = env.ID, env.Name
			out[i+1] = d
		})
	}
	wg.Wait()
	return out
}
