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

type ProjectSettings struct {
	AutoUpdate     *bool   `json:"auto_update"`      // NULL = follow global
	AutoUpdateCron *string `json:"auto_update_cron"` // NULL = immediate
}

type Project struct {
	Name         string        `json:"name" sortable:"true"`
	DirName      *string       `json:"dir_name"`
	Path         string        `json:"path"`
	Status       ProjectStatus `json:"status" sortable:"true"`
	StatusReason *string       `json:"status_reason"`
	ServiceCount int           `json:"service_count" sortable:"true"`
	RunningCount int           `json:"running_count" sortable:"true"`

	ProjectSettings

	BaseModel
}

func (Project) TableName() string {
	return "projects"
}
