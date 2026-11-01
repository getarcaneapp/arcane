package container

import "time"

type (
	// RedeployJournal records a container Arcane renamed to a backup name, so recovery only touches its own backups.
	RedeployJournal struct {
		StartedAt    time.Time `json:"startedAt"`
		OriginalID   string    `json:"originalId"`
		OriginalName string    `json:"originalName"`
		BackupName   string    `json:"backupName"`
		NewName      string    `json:"newName"`
	}
)

const (
	// redeployJournalKeyPrefix keys redeploy journals in the KV store by the original container ID.
	redeployJournalKeyPrefix = "container_redeploy_journal:"
)
