package variable

import (
	"getarcane.dev/arcane/v3/internal/database"
	"getarcane.dev/arcane/v3/internal/environment"
)

type (
	GlobalVariable struct {
		database.BaseModel

		Key          string                    `json:"key" gorm:"column:key" sortable:"true"`
		Value        string                    `json:"-" gorm:"column:value"`
		Environments []environment.Environment `json:"-" gorm:"many2many:global_variable_environments;joinForeignKey:GlobalVariableID;joinReferences:EnvironmentID"`
		IsSecret     bool                      `json:"isSecret" gorm:"column:is_secret"`
		// No gorm default tag: a default:true tag would make GORM omit false on
		// insert and the column default would silently win.
		AllEnvironments      bool `json:"allEnvironments" gorm:"column:all_environments"`
		InjectIntoContainers bool `json:"injectIntoContainers" gorm:"column:inject_into_containers"`
	}
)

func (GlobalVariable) TableName() string { return "global_variables" }
