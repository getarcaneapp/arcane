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
	CommitAuthorName       string  `json:"commitAuthorName" gorm:"column:commit_author_name"`
	CommitAuthorEmail      string  `json:"commitAuthorEmail" gorm:"column:commit_author_email"`
	SigningKey             string  `json:"signingKey" gorm:"column:signing_key"`                      // encrypted
	SigningKeyPassphrase   string  `json:"signingKeyPassphrase" gorm:"column:signing_key_passphrase"` // encrypted
	Description            *string `json:"description,omitempty" sortable:"true"`
	Enabled                bool    `json:"enabled" sortable:"true" search:"enabled,active,disabled"`
}

func (GitRepository) TableName() string {
	return "git_repositories"
}

// HasToken reports whether an HTTP token is stored.
func (r GitRepository) HasToken() bool {
	return r.Token != ""
}

// HasSshKey reports whether an SSH private key is stored.
func (r GitRepository) HasSshKey() bool {
	return r.SSHKey != ""
}

// HasSigningKey reports whether an OpenPGP signing key is stored.
func (r GitRepository) HasSigningKey() bool {
	return r.SigningKey != ""
}
