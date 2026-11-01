package diagnostics

import (
	"fmt"
	"time"

	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/francis"
)

type (
	// actorHostModel is a read-only view of a registered Francis host.
	actorHostModel struct {
		LastHealthCheck actorTime `gorm:"column:host_last_health_check"`
		HostID          string    `gorm:"column:host_id"`
		Address         string    `gorm:"column:host_address"`
	}

	// actorTypeModel is a registered actor type with its concurrency limit.
	actorTypeModel struct {
		ActorType        string
		ConcurrencyLimit int
	}

	// actorCountModel is one grouped count row from a Francis table.
	actorCountModel struct {
		NextDue   actorTime
		ActorType string
		Kind      string
		Count     int
	}

	// activeActorModel is a read-only view of an activated actor.
	activeActorModel struct {
		Activation actorTime `gorm:"column:actor_activation"`
		ActorType  string    `gorm:"column:actor_type"`
		ActorID    string    `gorm:"column:actor_id"`
		HostID     string    `gorm:"column:host_id"`
	}

	// terminalJobModel is a read-only view of a finished actor job.
	terminalJobModel struct {
		EndedAt   actorTime `gorm:"column:ended_at"`
		LastError *string   `gorm:"column:last_error"`
		JobID     string    `gorm:"column:job_id"`
		ActorType string    `gorm:"column:actor_type"`
		ActorID   string    `gorm:"column:actor_id"`
		JobMethod string    `gorm:"column:job_method"`
		Attempts  int       `gorm:"column:attempts"`
	}

	// actorTime scans Francis timestamps: SQLite stores epoch milliseconds, Postgres a timestamp.
	actorTime time.Time
)

func (actorHostModel) TableName() string { return francis.TablePrefix + "_hosts" }

func (activeActorModel) TableName() string { return francis.TablePrefix + "_active_actors" }

func (terminalJobModel) TableName() string { return francis.TablePrefix + "_terminal_jobs" }

func (t *actorTime) Scan(value any) error {
	switch v := value.(type) {
	case int64:
		*t = actorTime(time.UnixMilli(v).UTC())
	case time.Time:
		*t = actorTime(v.UTC())
	case nil:
		*t = actorTime{}
	default:
		return fmt.Errorf("unsupported actor timestamp %T", value)
	}
	return nil
}
