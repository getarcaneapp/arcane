package dto

type CreateEnvironmentDto struct {
	ApiUrl         string  `json:"apiUrl" binding:"required,url"`
	Name           *string `json:"name,omitempty"`
	Enabled        *bool   `json:"enabled,omitempty"`
	AccessToken    *string `json:"accessToken,omitempty"`
	BootstrapToken *string `json:"bootstrapToken,omitempty"`
}

type UpdateEnvironmentDto struct {
	ApiUrl         *string `json:"apiUrl,omitempty" binding:"omitempty,url"`
	Name           *string `json:"name,omitempty"`
	Enabled        *bool   `json:"enabled,omitempty"`
	AccessToken    *string `json:"accessToken,omitempty"`
	BootstrapToken *string `json:"bootstrapToken,omitempty"`
}

type TestConnectionDto struct {
	Status  string  `json:"status"`
	Message *string `json:"message,omitempty"`
}

type EnvironmentDto struct {
	ID      string `json:"id"`
	Name    string `json:"name,omitempty"`
	ApiUrl  string `json:"apiUrl"`
	Status  string `json:"status"`
	Enabled bool   `json:"enabled"`
}

type AgentPairRequestDto struct {
	Rotate *bool `json:"rotate,omitempty"`
}

type AgentPairResponseDto struct {
	Token string `json:"token"`
}
