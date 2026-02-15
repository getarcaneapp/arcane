package services

import (
	"context"
	"testing"

	"github.com/getarcaneapp/arcane/backend/internal/config"
	"github.com/stretchr/testify/require"
)

func TestJobService_GetJobSchedules_DefaultGitOpsInterval(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)

	settingsSvc, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	jobSvc := NewJobService(db, settingsSvc, &config.Config{})
	cfg := jobSvc.GetJobSchedules(ctx)

	require.Equal(t, "0 */1 * * * *", cfg.GitopsSyncInterval)
}
