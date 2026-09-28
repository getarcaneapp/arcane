// Package transfer coordinates moving Compose projects and named volumes
// between environments: preflight planning, durable transfer records and
// runs, the manager-side API, and the endpoint protocol every environment
// serves for the data plane.
package transfer

import (
	"time"

	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	transfertypes "github.com/getarcaneapp/arcane/types/v2/transfer"
)

// ResourceTransfer is the durable manager-side record of one transfer.
type ResourceTransfer struct {
	database.BaseModel

	IdempotencyKey              string                           `gorm:"column:idempotency_key;uniqueIndex"`
	Kind                        transfertypes.Kind               `gorm:"column:kind"`
	Mode                        transfertypes.Mode               `gorm:"column:mode"`
	SourceEnvironmentID         string                           `gorm:"column:source_environment_id;index"`
	DestinationEnvironmentID    string                           `gorm:"column:destination_environment_id"`
	Status                      transfertypes.Status             `gorm:"column:status;index"`
	Phase                       transfertypes.Phase              `gorm:"column:phase"`
	Attempt                     int                              `gorm:"column:attempt"`
	Plan                        transfertypes.Plan               `gorm:"column:plan;serializer:json"`
	Resources                   []transfertypes.ResourceProgress `gorm:"column:resources;serializer:json"`
	RecordedConsumers           []transfertypes.Consumer         `gorm:"column:recorded_consumers;serializer:json"`
	DestinationProjectID        string                           `gorm:"column:destination_project_id"`
	DestinationStartupAttempted bool                             `gorm:"column:destination_startup_attempted"`
	DestinationStartupAt        *time.Time                       `gorm:"column:destination_startup_at"`
	SourceHeld                  bool                             `gorm:"column:source_held"`
	CancelRequested             bool                             `gorm:"column:cancel_requested"`
	Recovery                    *transfertypes.RecoveryReport    `gorm:"column:recovery;serializer:json"`
	ActivityID                  string                           `gorm:"column:activity_id"`
	Error                       string                           `gorm:"column:error"`
	RequestedBy                 string                           `gorm:"column:requested_by"`
	FinishedAt                  *time.Time                       `gorm:"column:finished_at"`
}

// TableName pins the GORM table.
func (ResourceTransfer) TableName() string { return "resource_transfers" }
