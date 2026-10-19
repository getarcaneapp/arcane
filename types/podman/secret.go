package podman

import "time"

type SecretDriver struct {
	Name    string            `json:"name"`
	Options map[string]string `json:"options,omitempty"`
}

type SecretSpec struct {
	Name   string            `json:"name"`
	Driver SecretDriver      `json:"driver"`
	Labels map[string]string `json:"labels,omitempty"`
}

type Secret struct {
	ID        string     `json:"id"`
	CreatedAt time.Time  `json:"createdAt"`
	UpdatedAt time.Time  `json:"updatedAt"`
	Spec      SecretSpec `json:"spec"`
}

type SecretCreateRequest struct {
	Name          string            `json:"name" minLength:"1" doc:"Secret name"`
	Data          string            `json:"data" minLength:"1" doc:"Secret value"`
	Driver        string            `json:"driver,omitempty" doc:"Secret driver"`
	DriverOptions map[string]string `json:"driverOptions,omitempty" doc:"Secret driver options"`
	Labels        map[string]string `json:"labels,omitempty" doc:"Secret labels"`
}

type SecretCreateResponse struct {
	ID string `json:"id"`
}
