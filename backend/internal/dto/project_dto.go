package dto

type IncludeFileDto struct {
	Path         string `json:"path"`
	RelativePath string `json:"relativePath"`
	Content      string `json:"content"`
}

type CreateProjectDto struct {
	Name           string  `json:"name" binding:"required"`
	ComposeContent string  `json:"composeContent" binding:"required"`
	EnvContent     *string `json:"envContent,omitempty"`
}

type UpdateProjectDto struct {
	Name           *string `json:"name,omitempty"`
	ComposeContent *string `json:"composeContent,omitempty"`
	EnvContent     *string `json:"envContent,omitempty"`
}

type UpdateProjectIncludeDto struct {
	RelativePath string `json:"relativePath" binding:"required"`
	Content      string `json:"content" binding:"required"`
}

type CreateProjectReponseDto struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	DirName      string  `json:"dirName,omitempty"`
	Path         string  `json:"path"`
	Status       string  `json:"status"`
	StatusReason *string `json:"statusReason,omitempty"`
	ServiceCount int     `json:"serviceCount"`
	RunningCount int     `json:"runningCount"`
	CreatedAt    string  `json:"createdAt"`
	UpdatedAt    string  `json:"updatedAt"`
}

type ProjectDetailsDto struct {
	ID             string           `json:"id"`
	Name           string           `json:"name"`
	DirName        string           `json:"dirName,omitempty"`
	Path           string           `json:"path"`
	ComposeContent string           `json:"composeContent,omitempty"`
	EnvContent     string           `json:"envContent,omitempty"`
	IncludeFiles   []IncludeFileDto `json:"includeFiles,omitempty"`
	Status         string           `json:"status"`
	StatusReason   *string          `json:"statusReason,omitempty"`
	ServiceCount   int              `json:"serviceCount"`
	RunningCount   int              `json:"runningCount"`
	CreatedAt      string           `json:"createdAt"`
	UpdatedAt      string           `json:"updatedAt"`
	Services       []any            `json:"services,omitempty"`
	GitOps         *GitOpsDto       `json:"gitOps,omitempty"`
}

type GitOpsDto struct {
	RepositoryID string `json:"repositoryId"`
	URL          string `json:"url"`
	Branch       string `json:"branch"`
	AutoSync     bool   `json:"autoSync"`
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

type ProjectImagePullDto struct {
	Credentials []ContainerRegistryCredential `json:"credentials,omitempty"`
}
