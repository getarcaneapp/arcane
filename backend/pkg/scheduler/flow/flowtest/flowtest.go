// Package flowtest runs workflow engines on isolated Francis hosts for tests.
package flowtest

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/italypaleale/francis/builtin/workflow"
	"github.com/libtnb/sqlite"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/kv"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/flow"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/runs"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/francis"
	francistest "github.com/getarcaneapp/arcane/backend/v2/pkg/utils/francis/testing"
)

// Harness is an engine with its host and coordinator, ready for workflows to be defined.
type Harness struct {
	Runtime     *francis.Runtime
	Coordinator *runs.Coordinator
	Engine      *flow.Engine
}

// New builds a harness on an in-memory host, or on the given database URL.
func New(t testing.TB, activities flow.Activities, databaseURLs ...string) *Harness {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if migrateErr := db.AutoMigrate(&kv.KVEntry{}); migrateErr != nil {
		t.Fatal(migrateErr)
	}
	runtime := francistest.New(t, databaseURLs...)
	coordinator := runs.New(kv.NewKVService(&database.DB{DB: db}), runtime.Service(), time.UTC)
	if registerErr := coordinator.Register(runtime); registerErr != nil {
		t.Fatal(registerErr)
	}
	engine, err := flow.New(t.Context(), runtime, coordinator, activities)
	if err != nil {
		t.Fatal(err)
	}
	return &Harness{Runtime: runtime, Coordinator: coordinator, Engine: engine}
}

// Start starts the host and the engine once every workflow is defined.
func (h *Harness) Start(t testing.TB) {
	t.Helper()
	francistest.Start(t, h.Runtime)
	if err := h.Engine.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	h.Engine.Ready()
	t.Cleanup(func() { _ = h.Engine.Stop(context.WithoutCancel(t.Context())) })
}

// AssertDefinitions fails when a workflow's graph no longer matches the
// Fingerprint its definition declares, which means it changed without a version bump.
func AssertDefinitions(t testing.TB, h *Harness) {
	t.Helper()
	for _, wf := range h.Engine.Workflows() {
		definition := wf.Francis()
		service := definition.Service(h.Runtime.Service())
		// A fresh host has no instances, so resetting installs this binary's fingerprint.
		if err := service.ForgetVersion(t.Context(), definition.Version()); err != nil {
			t.Fatal(err)
		}
		infos, err := service.Definitions(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		index := slices.IndexFunc(infos, func(info workflow.DefinitionInfo) bool { return info.Version == definition.Version() })
		if index < 0 {
			t.Fatalf("workflow %q version %d is not registered", definition.Name(), definition.Version())
		}
		if got := infos[index].Fingerprint; got != wf.Fingerprint() {
			t.Errorf("workflow %q no longer matches its Fingerprint: bump Version if this change is not yet released, then set Fingerprint to %q", definition.Name(), got)
		}
	}
}
