package dockerinfo

import "github.com/docker/docker/api/types/system"

type Info struct {
	// Success indicates if the Docker daemon information was successfully retrieved.
	//
	// Required: true
	Success bool `json:"success"`

	// APIVersion is the API version of the Docker daemon.
	//
	// Required: true
	APIVersion string `json:"apiVersion"`

	// GitCommit is the Git commit hash of the Docker daemon.
	//
	// Required: true
	GitCommit string `json:"gitCommit"`

	// GoVersion is the Go version used to build the Docker daemon.
	//
	// Required: true
	GoVersion string `json:"goVersion"`

	// Os is the operating system the Docker daemon is running on.
	//
	// Required: true
	Os string `json:"os"`

	// Arch is the architecture the Docker daemon is running on.
	//
	// Required: true
	Arch string `json:"arch"`

	// BuildTime is the build time of the Docker daemon.
	//
	// Required: true
	BuildTime string `json:"buildTime"`

	// Embedded system.Info from the Docker daemon.
	//
	// Required: true
	system.Info
}
