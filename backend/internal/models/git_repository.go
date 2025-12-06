package models

import (
	"time"
)

type GitRepository struct {
	Name        string    `json:"name" sortable:"true"`
	URL         string    `json:"url" sortable:"true"`
	AuthType    string    `json:"authType" sortable:"true"` // none, http, ssh
	Username    string    `json:"username" sortable:"true"`
	Token       string    `json:"token"`       // encrypted
	SSHKey      string    `json:"sshKey"`      // encrypted
	Description *string   `json:"description,omitempty" sortable:"true"`
	Enabled     bool      `json:"enabled" sortable:"true"`
	CreatedAt   time.Time `json:"createdAt" sortable:"true"`
	UpdatedAt   time.Time `json:"updatedAt" sortable:"true"`
	BaseModel
}

func (GitRepository) TableName() string {
	return "git_repositories"
}

type CreateGitRepositoryRequest struct {
	Name        string  `json:"name" binding:"required"`
	URL         string  `json:"url" binding:"required"`
	AuthType    string  `json:"authType" binding:"required"` // none, http, ssh
	Username    string  `json:"username"`
	Token       string  `json:"token"`
	SSHKey      string  `json:"sshKey"`
	Description *string `json:"description"`
	Enabled     *bool   `json:"enabled"`
}

type UpdateGitRepositoryRequest struct {
	Name        *string `json:"name"`
	URL         *string `json:"url"`
	AuthType    *string `json:"authType"`
	Username    *string `json:"username"`
	Token       *string `json:"token"`
	SSHKey      *string `json:"sshKey"`
	Description *string `json:"description"`
	Enabled     *bool   `json:"enabled"`
}
