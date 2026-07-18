package models

// GlobalVariable is a manager-level key/value variable materialized into each
// environment's .env.global file. Value holds ciphertext when IsSecret is true.
type GlobalVariable struct {
	BaseModel

	Key      string `json:"key" gorm:"column:key" sortable:"true"`
	Value    string `json:"-" gorm:"column:value"`
	IsSecret bool   `json:"isSecret" gorm:"column:is_secret"`
	// No gorm default tag: a default:true tag would make GORM omit false on
	// insert and the column default would silently win.
	AllEnvironments bool          `json:"allEnvironments" gorm:"column:all_environments"`
	Environments    []Environment `json:"-" gorm:"many2many:global_variable_environments;joinForeignKey:GlobalVariableID;joinReferences:EnvironmentID"`
}

func (GlobalVariable) TableName() string { return "global_variables" }
