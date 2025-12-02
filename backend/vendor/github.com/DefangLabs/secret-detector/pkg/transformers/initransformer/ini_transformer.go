// Package initransformer supports transformation from configuration file format (like .ini, .properties .cnf or .cfg)
package initransformer

import (
	"fmt"
	"strings"

	"github.com/DefangLabs/secret-detector/pkg/dataformat"

	"gopkg.in/ini.v1"

	"github.com/DefangLabs/secret-detector/pkg/secrets"
)

const (
	Name = "ini"
)

var supportedFormats = []dataformat.DataFormat{dataformat.INI, dataformat.CNF, dataformat.CFG, dataformat.ENV, dataformat.Properties}

func init() {
	secrets.GetTransformerFactory().Register(Name, NewTransformer)
}

type transformer struct {
}

func NewTransformer() secrets.Transformer {
	return &transformer{}
}

func (t *transformer) Transform(in string) (map[string]string, bool) {
	// Try parse twice, because nested values option and allow multiline options overlaps
	//   (when using both options together nested values are treated as multiline values)
	iniFile, err := ini.LoadSources(ini.LoadOptions{AllowNestedValues: true}, []byte(in))
	if err != nil {
		iniFile, err = ini.LoadSources(ini.LoadOptions{AllowPythonMultilineValues: true}, []byte(in))
		if err != nil {
			return nil, false
		}
	}

	iniMap := convertToMap(iniFile)
	if len(iniMap) <= 1 {
		if strings.Index(in, "=") >= len(in)-2 {
			// make sure that the file with a single key is not base64 strings might end with == that might mistakenly parse
			return nil, false

		}
	}
	return iniMap, true
}

func (t *transformer) SupportedFormats() []dataformat.DataFormat {
	return supportedFormats
}

func (t *transformer) SupportFiles() bool {
	return true
}

func convertToMap(iniFile *ini.File) map[string]string {
	iniMap := make(map[string]string)
	for _, section := range iniFile.Sections() {
		var path string
		if section.Name() != ini.DefaultSection {
			path = section.Name()
		}

		for _, key := range section.Keys() {
			path := concatPath(path, key.Name())
			if len(key.NestedValues()) == 0 {
				iniMap[path] = key.Value()
			}

			for _, nestedKeyValue := range key.NestedValues() {
				nestedKey, nestedValue := extractKeyAndValue(nestedKeyValue)
				iniMap[concatPath(path, nestedKey)] = nestedValue
			}
		}
	}

	return iniMap
}

func extractKeyAndValue(keyValue string) (key, value string) {
	separatorIndex := strings.IndexAny(keyValue, "=:")
	if separatorIndex == -1 {
		key = strings.TrimSpace(keyValue)
		return
	}

	key = strings.TrimSpace(keyValue[:separatorIndex])
	value = strings.TrimSpace(keyValue[separatorIndex+1:])
	return
}

func concatPath(part1, part2 string) string {
	if len(part1) == 0 {
		return part2
	}
	return fmt.Sprintf("%s.%s", part1, part2)
}
