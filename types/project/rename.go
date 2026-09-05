package project

import (
	"context"
	"time"

	volumetypes "github.com/getarcaneapp/arcane/types/v2/volume"
	"github.com/moby/moby/client"
)

const (
	RenameJournalKeyPrefix         = "project_rename_journal:"
	RenameRollbackCleanupKeyPrefix = "project_rename_rollback_cleanup:"

	RenameJournalPhaseStarted                = "started"
	RenameJournalPhaseTargetsCopied          = "targets_copied"
	RenameJournalPhaseOldVolumesRemoved      = "old_volumes_removed"
	RenameJournalPhaseProjectStateCommitted  = "project_state_committed"
	RenameJournalPhaseSourceCleanupPending   = "source_cleanup_pending"
	RenameJournalPhaseProjectStateRolledBack = "project_state_rolled_back"
)

type RenameJournal struct {
	ProjectID  string                      `json:"projectId"`
	OldName    string                      `json:"oldName"`
	NewName    string                      `json:"newName"`
	OldPath    string                      `json:"oldPath"`
	NewPath    string                      `json:"newPath"`
	OldDirName *string                     `json:"oldDirName,omitempty"`
	NewDirName string                      `json:"newDirName"`
	Phase      string                      `json:"phase"`
	Volumes    []volumetypes.JournalVolume `json:"volumes,omitempty"`
	UpdatedAt  time.Time                   `json:"updatedAt"`
}

type RenameRollbackCleanup struct {
	ProjectID string                      `json:"projectId"`
	OldName   string                      `json:"oldName"`
	OldPath   string                      `json:"oldPath"`
	NewName   string                      `json:"newName"`
	NewPath   string                      `json:"newPath"`
	Volumes   []volumetypes.JournalVolume `json:"volumes,omitempty"`
	UpdatedAt time.Time                   `json:"updatedAt"`
}

// RenameRecoveryOperations supplies persistence and Docker access to rename recovery.
type RenameRecoveryOperations struct {
	Docker               func(context.Context, bool) (*client.Client, error)
	WriteJournal         func(context.Context, *RenameJournal, string) error
	ClearJournal         func(context.Context, string) error
	ClearRollbackCleanup func(context.Context, string) error
	RestoreState         func(context.Context, *RenameJournal) error
	WriteRollbackCleanup func(context.Context, *RenameJournal) error
}
