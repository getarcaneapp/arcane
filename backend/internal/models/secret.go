package models

type Secret struct {
	Name          string  `json:"name" sortable:"true"`
	EnvironmentID string  `json:"environmentId" sortable:"true"`
	Content       string  `json:"content"`
	FilePath      *string `json:"filePath,omitempty"`
	Description   *string `json:"description,omitempty" sortable:"true"`
	BaseModel
}

func (Secret) TableName() string {
	return "secrets"
}
