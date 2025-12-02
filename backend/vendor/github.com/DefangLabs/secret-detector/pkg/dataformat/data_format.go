package dataformat

import (
	"path/filepath"
	"strings"
)

type DataFormat string

const (
	JSON       DataFormat = "json"
	YAML       DataFormat = "yaml"
	XML        DataFormat = "xml"
	INI        DataFormat = "ini"
	CNF        DataFormat = "cnf"
	CFG        DataFormat = "cfg"
	ENV        DataFormat = "env"
	Properties DataFormat = "properties"
	Command    DataFormat = "command"
)

func FromPath(path string) DataFormat {
	extension := strings.ToLower(filepath.Ext(path))
	switch extension {
	case ".json":
		return JSON
	case ".yaml", ".yml":
		return YAML
	case ".xml":
		return XML
	case ".ini":
		return INI
	case ".cnf", ".conf", ".cf":
		return CNF
	case ".cfg":
		return CFG
	case ".env":
		return ENV
	case ".properties":
		return Properties
	}
	return ""
}
