package models

import (
	"time"
)

type GitOpsSync struct {
	Name           string         `json:"name" sortable:"true" search:"sync,gitops,automation,deploy,deployment,continuous"`
	EnvironmentID  string         `json:"environmentId" sortable:"true"`
	Environment    *Environment   `json:"environment,omitempty" gorm:"foreignKey:EnvironmentID"`
	RepositoryID   string         `json:"repositoryId" sortable:"true"`
	Repository     *GitRepository `json:"repository,omitempty" gorm:"foreignKey:RepositoryID"`
	Branch         string         `json:"branch" sortable:"true" search:"branch,main,master,develop,feature,release"`
	ComposePath    string         `json:"composePath" sortable:"true" search:"compose,docker-compose,path,file,yaml,yml"`
	ProjectName    string         `json:"projectName" sortable:"true" search:"project,name,stack,application,service"` // Name of project to create/update
	ProjectID      *string        `json:"projectId,omitempty" sortable:"true"`                                         // Set after project is created
	Project        *Project       `json:"project,omitempty" gorm:"foreignKey:ProjectID"`
	AutoSync       bool           `json:"autoSync" sortable:"true" search:"auto,automatic,sync,continuous,scheduled"`
	SyncInterval   int            `json:"syncInterval" sortable:"true" search:"interval,frequency,schedule,cron,minutes"` // in minutes
	LastSyncAt     *time.Time     `json:"lastSyncAt,omitempty" sortable:"true"`
	LastSyncStatus *string        `json:"lastSyncStatus,omitempty" search:"status,success,failed,pending,error"`
	LastSyncError  *string        `json:"lastSyncError,omitempty"`
	LastSyncCommit *string        `json:"lastSyncCommit,omitempty" search:"commit,hash,sha,revision"`
	Enabled        bool           `json:"enabled" sortable:"true" search:"enabled,active,disabled"`
	BaseModel
}

func (GitOpsSync) TableName() string {
	return "gitops_syncs"
}
