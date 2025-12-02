package xmltransformer

import (
	"encoding/xml"
	"fmt"
	"strings"

	"github.com/DefangLabs/secret-detector/pkg/dataformat"

	"github.com/DefangLabs/secret-detector/pkg/secrets"
)

const (
	Name = "xml"
)

var supportedFormats = []dataformat.DataFormat{dataformat.XML}

func init() {
	secrets.GetTransformerFactory().Register(Name, NewTransformer)
}

type transformer struct {
}

func NewTransformer() secrets.Transformer {
	return &transformer{}
}

func (t *transformer) Transform(in string) (map[string]string, bool) {
	if !validateXML(in) {
		return nil, false
	}
	return t.xmlToMap(in), true
}

func (t *transformer) SupportedFormats() []dataformat.DataFormat {
	return supportedFormats
}

func (t *transformer) SupportFiles() bool {
	return true
}

func (t *transformer) xmlToMap(in string) map[string]string {
	res := make(map[string]string)
	s := newXMLStack()
	decoder := xml.NewDecoder(strings.NewReader(in))
	for {
		tokenInterface, _ := decoder.Token()
		if tokenInterface == nil {
			break
		}

		switch token := tokenInterface.(type) {
		case xml.StartElement:
			s.Push(token)
		case xml.CharData:
			charData := strings.TrimSpace(string(token))
			if len(charData) > 0 {
				if element := s.Peek(); element != nil {
					element.AppendCharData(charData)
				}
			}
		case xml.EndElement:
			element := s.Pop()
			for i, charData := range element.CharData() {
				if i == 0 {
					res[element.Path()] = charData
				} else {
					res[fmt.Sprintf("%v (ln. %v)", element.Path(), i+1)] = charData
				}
			}
			for k, v := range element.Attributes() {
				res[k] = v
			}
		case xml.Comment, xml.ProcInst, xml.Directive:
			// ignored
		}
	}
	return res
}

func validateXML(in string) bool {
	return xml.Unmarshal([]byte(in), new(interface{})) == nil
}
