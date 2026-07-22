package meta

type JobMetadata struct {
	ID             string
	Name           string
	Description    string
	Category       string
	SettingsKey    string
	EnabledKey     string
	ManagerOnly    bool
	IsContinuous   bool
	CanRunManually bool
	Prerequisites  []JobPrerequisiteMetadata
}

type JobPrerequisiteMetadata struct {
	SettingKey  string
	Label       string
	SettingsURL string
}
