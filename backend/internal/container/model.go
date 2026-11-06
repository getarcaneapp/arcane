package container

import "time"

type (
	RedeployJournal struct {
		StartedAt    time.Time `json:"startedAt"`
		OriginalID   string    `json:"originalId"`
		OriginalName string    `json:"originalName"`
		BackupName   string    `json:"backupName"`
		NewName      string    `json:"newName"`
	}
)

const (
	redeployJournalKeyPrefix = "container_redeploy_journal:"
)
