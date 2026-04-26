package models

type ProjectStatus string

const (
	ProjectStatusRunning          ProjectStatus = "running"
	ProjectStatusStopped          ProjectStatus = "stopped"
	ProjectStatusPartiallyRunning ProjectStatus = "partially running"
	ProjectStatusUnknown          ProjectStatus = "unknown"
	ProjectStatusDeploying        ProjectStatus = "deploying"
	ProjectStatusStopping         ProjectStatus = "stopping"
	ProjectStatusRestarting       ProjectStatus = "restarting"
)

type Project struct {
	Name               string        `json:"name" sortable:"true" gorm:"index:idx_projects_name"`
	DirName            *string       `json:"dir_name"`
	Path               string        `json:"path" sortable:"true" gorm:"uniqueIndex"`
	Status             ProjectStatus `json:"status" sortable:"true"`
	StatusReason       *string       `json:"status_reason"`
	ServiceCount       int           `json:"service_count" sortable:"true"`
	RunningCount       int           `json:"running_count" sortable:"true"`
	GitOpsManagedBy    *string       `json:"gitops_managed_by,omitempty" gorm:"column:gitops_managed_by"`
	ComposeProjectName *string       `json:"compose_project_name,omitempty" gorm:"column:compose_project_name"`

	BaseModel
}

func (Project) TableName() string {
	return "projects"
}
