package models

type GitRepository struct {
	Name                   string  `json:"name" sortable:"true" search:"git,repository,repo,source,version,control,github,gitlab,bitbucket"`
	URL                    string  `json:"url" sortable:"true" search:"url,git,clone,remote,https,ssh"`
	AuthType               string  `json:"authType" sortable:"true" search:"auth,authentication,credentials,token,ssh,http"` // none, http, ssh
	Username               string  `json:"username" sortable:"true" search:"username,user,login,account"`
	Token                  string  `json:"token" search:"token,password,credentials,secret,auth"` // encrypted
	SSHKey                 string  `json:"sshKey" search:"ssh,key,private,public,certificate"`    // encrypted
	SSHHostKeyVerification string  `json:"sshHostKeyVerification" gorm:"default:accept_new"`      // strict, accept_new, skip
	Description            *string `json:"description,omitempty" sortable:"true"`
	Enabled                bool    `json:"enabled" sortable:"true" search:"enabled,active,disabled"`
	BaseModel
}

func (GitRepository) TableName() string {
	return "git_repositories"
}

type CreateGitRepositoryRequest struct {
	Name                   string  `json:"name" binding:"required"`
	URL                    string  `json:"url" binding:"required"`
	AuthType               string  `json:"authType" binding:"required,oneof=none http ssh"`
	Username               string  `json:"username,omitempty"`
	Token                  string  `json:"token,omitempty"`
	SSHKey                 string  `json:"sshKey,omitempty"`
	SSHHostKeyVerification string  `json:"sshHostKeyVerification,omitempty" binding:"omitempty,oneof=strict accept_new skip"`
	Description            *string `json:"description,omitempty"`
	Enabled                *bool   `json:"enabled,omitempty"`
}

type UpdateGitRepositoryRequest struct {
	Name                   *string `json:"name,omitempty"`
	URL                    *string `json:"url,omitempty"`
	AuthType               *string `json:"authType,omitempty" binding:"omitempty,oneof=none http ssh"`
	Username               *string `json:"username,omitempty"`
	Token                  *string `json:"token,omitempty"`
	SSHKey                 *string `json:"sshKey,omitempty"`
	SSHHostKeyVerification *string `json:"sshHostKeyVerification,omitempty" binding:"omitempty,oneof=strict accept_new skip"`
	Description            *string `json:"description,omitempty"`
	Enabled                *bool   `json:"enabled,omitempty"`
}
