package jsontransformer

import (
	"encoding/json"

	"github.com/DefangLabs/secret-detector/pkg/dataformat"

	"github.com/DefangLabs/secret-detector/pkg/secrets"
	"github.com/DefangLabs/secret-detector/pkg/transformers/helpers"
)

const (
	Name = "json"
)

var supportedFormats = []dataformat.DataFormat{dataformat.JSON}

func init() {
	secrets.GetTransformerFactory().Register(Name, NewTransformer)
}

type transformer struct {
}

func NewTransformer() secrets.Transformer {
	return &transformer{}
}

func (t *transformer) Transform(in string) (map[string]string, bool) {
	var jsonMap map[string]interface{}
	if err := json.Unmarshal([]byte(in), &jsonMap); err != nil {
		return nil, false
	}
	return helpers.Flatten(jsonMap), true
}

func (t *transformer) SupportedFormats() []dataformat.DataFormat {
	return supportedFormats
}

func (t *transformer) SupportFiles() bool {
	return true
}
