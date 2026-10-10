package diagnostics

import (
	"fmt"
	"time"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/francis"
)

// actorHostModel is a read-only view of a registered Francis host.
type actorHostModel struct {
	HostID          string            `gorm:"column:host_id"`
	Address         string            `gorm:"column:host_address"`
	LastHealthCheck actorTimeInternal `gorm:"column:host_last_health_check"`
}

func (actorHostModel) TableName() string { return francis.TablePrefix + "_hosts" }

// actorTypeModel is a registered actor type with its concurrency limit.
type actorTypeModel struct {
	ActorType        string
	ConcurrencyLimit int
}

// actorCountModel is one grouped count row from a Francis table.
type actorCountModel struct {
	ActorType string
	Kind      string
	Count     int
	NextDue   actorTimeInternal
}

// activeActorModel is a read-only view of an activated actor.
type activeActorModel struct {
	ActorType  string            `gorm:"column:actor_type"`
	ActorID    string            `gorm:"column:actor_id"`
	HostID     string            `gorm:"column:host_id"`
	Activation actorTimeInternal `gorm:"column:actor_activation"`
}

func (activeActorModel) TableName() string { return francis.TablePrefix + "_active_actors" }

// terminalJobModel is a read-only view of a finished actor job.
type terminalJobModel struct {
	JobID     string            `gorm:"column:job_id"`
	ActorType string            `gorm:"column:actor_type"`
	ActorID   string            `gorm:"column:actor_id"`
	JobMethod string            `gorm:"column:job_method"`
	Attempts  int               `gorm:"column:attempts"`
	LastError *string           `gorm:"column:last_error"`
	EndedAt   actorTimeInternal `gorm:"column:ended_at"`
}

func (terminalJobModel) TableName() string { return francis.TablePrefix + "_terminal_jobs" }

// actorTimeInternal scans Francis timestamps: SQLite stores epoch milliseconds, Postgres a timestamp.
type actorTimeInternal time.Time

func (t *actorTimeInternal) Scan(value any) error {
	switch v := value.(type) {
	case int64:
		*t = actorTimeInternal(time.UnixMilli(v).UTC())
	case time.Time:
		*t = actorTimeInternal(v.UTC())
	case nil:
		*t = actorTimeInternal{}
	default:
		return fmt.Errorf("unsupported actor timestamp %T", value)
	}
	return nil
}
