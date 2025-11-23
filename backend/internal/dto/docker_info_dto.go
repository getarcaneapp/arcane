package dto

import "github.com/docker/docker/api/types/system"

type DockerInfoDto struct {
	Success    bool   `json:"success"`
	APIVersion string `json:"apiVersion"`
	GitCommit  string `json:"gitCommit"`
	GoVersion  string `json:"goVersion"`
	Os         string `json:"os"`
	Arch       string `json:"arch"`
	BuildTime  string `json:"buildTime"`
	system.Info
}
