package models

// ProjectTag stores one normalized tag association and its management source.
type ProjectTag struct {
	ProjectID string `json:"projectId" gorm:"column:project_id;primaryKey"`
	Name      string `json:"name" gorm:"column:name;primaryKey"`
	Source    string `json:"source" gorm:"column:source;primaryKey"`
	Color     string `json:"color" gorm:"column:color;not null;default:gray"`
}

// TableName returns the database table used for project tag associations.
func (ProjectTag) TableName() string {
	return "project_tags"
}
