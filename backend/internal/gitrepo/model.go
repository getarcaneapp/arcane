package gitrepo

import "github.com/getarcaneapp/arcane/backend/v2/internal/database"

type GitRepository struct {
	database.BaseModel

	Name                   string  `json:"name" sortable:"true" search:"git,repository,repo,source,version,control,github,gitlab,bitbucket"`
	URL                    string  `json:"url" sortable:"true" search:"url,git,clone,remote,https,ssh"`
	AuthType               string  `json:"authType" sortable:"true" search:"auth,authentication,credentials,token,ssh,http"` // none, http, ssh
	Username               string  `json:"username" sortable:"true" search:"username,user,login,account"`
	Token                  string  `json:"token" search:"token,password,credentials,secret,auth"` // encrypted
	SSHKey                 string  `json:"sshKey" search:"ssh,key,private,public,certificate"`    // encrypted
	SSHHostKeyVerification string  `json:"sshHostKeyVerification" gorm:"default:accept_new"`      // strict, accept_new, skip
	Description            *string `json:"description,omitempty" sortable:"true"`
	Enabled                bool    `json:"enabled" sortable:"true" search:"enabled,active,disabled"`
}

func (GitRepository) TableName() string {
	return "git_repositories"
}
