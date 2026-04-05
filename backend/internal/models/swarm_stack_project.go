package models

type SwarmStackProject struct {
	Name          string  `json:"name" sortable:"true"`
	DirName       *string `json:"dir_name"`
	EnvironmentID string  `json:"environment_id" gorm:"column:environment_id;index" sortable:"true"`
	Path          string  `json:"path" sortable:"true"`
	ServiceCount  int     `json:"service_count" gorm:"column:service_count" sortable:"true"`

	BaseModel
}

func (SwarmStackProject) TableName() string {
	return "swarm_stack_projects"
}
