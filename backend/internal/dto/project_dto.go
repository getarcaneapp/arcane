package dto

type CreateProjectDto struct {
	Name           string  `json:"name" binding:"required"`
	ComposeContent string  `json:"composeContent" binding:"required"`
	EnvContent     *string `json:"envContent,omitempty"`
}

type UpdateProjectDto struct {
	Name           *string             `json:"name,omitempty"`
	ComposeContent *string             `json:"composeContent,omitempty"`
	EnvContent     *string             `json:"envContent,omitempty"`
	Settings       *ProjectSettingsDto `json:"settings,omitempty"`
}

type CreateProjectReponseDto struct {
	ID           string             `json:"id"`
	Name         string             `json:"name"`
	DirName      string             `json:"dirName,omitempty"`
	Path         string             `json:"path"`
	Status       string             `json:"status"`
	StatusReason *string            `json:"statusReason,omitempty"`
	ServiceCount int                `json:"serviceCount"`
	RunningCount int                `json:"runningCount"`
	CreatedAt    string             `json:"createdAt"`
	UpdatedAt    string             `json:"updatedAt"`
	Settings     ProjectSettingsDto `json:"settings"`
}

type ProjectDetailsDto struct {
	ID             string             `json:"id"`
	Name           string             `json:"name"`
	DirName        string             `json:"dirName,omitempty"`
	Path           string             `json:"path"`
	ComposeContent string             `json:"composeContent,omitempty"`
	EnvContent     string             `json:"envContent,omitempty"`
	Status         string             `json:"status"`
	StatusReason   *string            `json:"statusReason,omitempty"`
	ServiceCount   int                `json:"serviceCount"`
	RunningCount   int                `json:"runningCount"`
	CreatedAt      string             `json:"createdAt"`
	UpdatedAt      string             `json:"updatedAt"`
	Services       []any              `json:"services,omitempty"`
	Settings       ProjectSettingsDto `json:"settings"`
}

type DestroyProjectDto struct {
	RemoveFiles   bool `json:"removeFiles,omitempty"`
	RemoveVolumes bool `json:"removeVolumes,omitempty"`
}

type ProjectStatusCounts struct {
	RunningProjects int `json:"runningProjects"`
	StoppedProjects int `json:"stoppedProjects"`
	TotalProjects   int `json:"totalProjects"`
}

type ProjectSettingsDto struct {
	AutoUpdate     *bool   `json:"autoUpdate"`
	AutoUpdateCron *string `json:"autoUpdateCron"`
}
