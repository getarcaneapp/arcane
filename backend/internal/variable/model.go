package variable

import (
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/environment"
)

// GlobalVariable is a manager-level key/value variable materialized into each
// environment's .env.global file. Value holds ciphertext when IsSecret is true.
type GlobalVariable struct {
	database.BaseModel

	Key      string `json:"key" gorm:"column:key" sortable:"true"`
	Value    string `json:"-" gorm:"column:value"`
	IsSecret bool   `json:"isSecret" gorm:"column:is_secret"`
	// No gorm default tag: a default:true tag would make GORM omit false on
	// insert and the column default would silently win.
	AllEnvironments bool                      `json:"allEnvironments" gorm:"column:all_environments"`
	Environments    []environment.Environment `json:"-" gorm:"many2many:global_variable_environments;joinForeignKey:GlobalVariableID;joinReferences:EnvironmentID"`
	// When false the variable is only available for ${VAR} interpolation and
	// is not added to every container's environment.
	InjectIntoContainers bool `json:"injectIntoContainers" gorm:"column:inject_into_containers"`
}

func (GlobalVariable) TableName() string { return "global_variables" }
