//go:build !buildables

package config

type (
	BuildablesConfig struct {
		AutoLoginUsername string
		AutoLoginPassword string
	}
)
