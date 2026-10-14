package base

type EnvironmentRef struct {
	ID   string `json:"id" doc:"Environment ID"`
	Name string `json:"name" doc:"Environment display name"`
}

type EnvironmentFailure struct {
	Environment EnvironmentRef `json:"environment" doc:"Environment that failed"`
	Message     string         `json:"message" doc:"Why the environment's results are missing"`
}

//nolint:recvcheck // Rows read the tag by value and set it through a pointer.
type EnvironmentTag struct {
	EnvironmentID   string `json:"environmentId,omitempty" doc:"Environment the row came from in a multi-environment list"`
	EnvironmentName string `json:"environmentName,omitempty" doc:"Display name of the environment the row came from"`
}

func (t EnvironmentTag) Environment() EnvironmentRef {
	return EnvironmentRef{ID: t.EnvironmentID, Name: t.EnvironmentName}
}

func (t *EnvironmentTag) SetEnvironment(ref EnvironmentRef) {
	t.EnvironmentID, t.EnvironmentName = ref.ID, ref.Name
}
