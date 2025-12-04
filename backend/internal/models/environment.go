package models

import "time"

type Environment struct {
	Name        string     `json:"name" sortable:"true"`
	ApiUrl      string     `json:"apiUrl" gorm:"column:api_url" sortable:"true"`
	Status      string     `json:"status" sortable:"true"`
	Enabled     bool       `json:"enabled" sortable:"true"`
	LastSeen    *time.Time `json:"lastSeen" gorm:"column:last_seen"`
	AccessToken *string    `json:"-" gorm:"column:access_token"`
	Tags        []string   `json:"tags" gorm:"-"`

	BaseModel
}

func (Environment) TableName() string { return "environments" }

type EnvironmentStatus string

const (
	EnvironmentStatusOnline  EnvironmentStatus = "online"
	EnvironmentStatusOffline EnvironmentStatus = "offline"
	EnvironmentStatusError   EnvironmentStatus = "error"
)

type EnvironmentTag struct {
	EnvironmentID string `json:"environmentId" gorm:"column:environment_id;primaryKey"`
	Tag           string `json:"tag" gorm:"primaryKey"`
}

func (EnvironmentTag) TableName() string { return "environment_tags" }

type EnvironmentFilter struct {
	UserID       string                        `json:"userId" gorm:"column:user_id;not null;index"`
	Name         string                        `json:"name" gorm:"not null"`
	IsDefault    bool                          `json:"isDefault" gorm:"column:is_default;default:false"`
	SearchQuery  string                        `json:"searchQuery" gorm:"column:search_query;default:''"`
	SelectedTags StringSlice                   `json:"selectedTags" gorm:"column:selected_tags;type:jsonb;default:'[]'"`
	ExcludedTags StringSlice                   `json:"excludedTags" gorm:"column:excluded_tags;type:jsonb;default:'[]'"`
	TagMode      EnvironmentFilterTagMode      `json:"tagMode" gorm:"column:tag_mode;default:'any'"`
	StatusFilter EnvironmentFilterStatusFilter `json:"statusFilter" gorm:"column:status_filter;default:'all'"`
	GroupBy      EnvironmentFilterGroupBy      `json:"groupBy" gorm:"column:group_by;default:'none'"`

	BaseModel
}

func (EnvironmentFilter) TableName() string {
	return "environment_filters"
}

type EnvironmentFilterTagMode string

const (
	TagModeAny EnvironmentFilterTagMode = "any"
	TagModeAll EnvironmentFilterTagMode = "all"
)

type EnvironmentFilterStatusFilter string

const (
	StatusFilterAll     EnvironmentFilterStatusFilter = "all"
	StatusFilterOnline  EnvironmentFilterStatusFilter = "online"
	StatusFilterOffline EnvironmentFilterStatusFilter = "offline"
)

type EnvironmentFilterGroupBy string

const (
	GroupByNone   EnvironmentFilterGroupBy = "none"
	GroupByStatus EnvironmentFilterGroupBy = "status"
	GroupByTags   EnvironmentFilterGroupBy = "tags"
)
