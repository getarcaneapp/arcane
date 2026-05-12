package handlers

import (
	"context"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/internal/services"
	"github.com/stretchr/testify/require"
)

func TestGitRepositoryHandlers_RequireAdmin(t *testing.T) {
	handler := &GitRepositoryHandler{repoService: &services.GitRepositoryService{}}

	tests := []struct {
		name string
		call func() error
	}{
		{
			name: "create repository",
			call: func() error {
				_, err := handler.CreateRepository(context.Background(), &CreateGitRepositoryInput{})
				return err
			},
		},
		{
			name: "update repository",
			call: func() error {
				_, err := handler.UpdateRepository(context.Background(), &UpdateGitRepositoryInput{ID: "repo-1"})
				return err
			},
		},
		{
			name: "delete repository",
			call: func() error {
				_, err := handler.DeleteRepository(context.Background(), &DeleteGitRepositoryInput{ID: "repo-1"})
				return err
			},
		},
		{
			name: "test repository",
			call: func() error {
				_, err := handler.TestRepository(context.Background(), &TestGitRepositoryInput{ID: "repo-1", Branch: "main"})
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.call()
			require.Error(t, err)

			var statusErr huma.StatusError
			require.ErrorAs(t, err, &statusErr)
			require.Equal(t, http.StatusForbidden, statusErr.GetStatus())
			require.Contains(t, statusErr.Error(), "admin access required")
		})
	}
}
