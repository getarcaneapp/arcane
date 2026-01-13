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
	Name            string        `json:"name" sortable:"true"`
	DirName         *string       `json:"dir_name"`
	Path            string        `json:"path"`
	Status          ProjectStatus `json:"status" sortable:"true"`
	StatusReason    *string       `json:"status_reason"`
	ServiceCount    int           `json:"service_count" sortable:"true"`
	RunningCount    int           `json:"running_count" sortable:"true"`
	GitOpsManagedBy *string       `json:"gitops_managed_by,omitempty" gorm:"column:gitops_managed_by"`

	// Security: Track whether project was created by an admin user.
	// Lifecycle hooks are only executed for projects created by admins.
	CreatedByAdmin bool `json:"created_by_admin" gorm:"column:created_by_admin;default:false"`

	BaseModel
}

func (Project) TableName() string {
	return "projects"
}
