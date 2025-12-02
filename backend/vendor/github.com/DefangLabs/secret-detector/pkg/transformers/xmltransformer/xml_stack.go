package xmltransformer

import (
	"encoding/xml"
)

type xmlStack interface {
	Push(v xml.StartElement)
	Pop() stackElement
	Peek() stackElement
}

type xmlStackImpl struct {
	stack  []*stackElementImpl
	keyGen uniqueKeyGenerator
}

func newXMLStack() xmlStack {
	return &xmlStackImpl{
		keyGen: NewUniqueKeyGenerator(),
	}
}

func (s *xmlStackImpl) Push(v xml.StartElement) {
	element := &stackElementImpl{
		name: v.Name.Local,
	}
	s.stack = append(s.stack, element)

	element.path = s.keyGen.Gen(s.currPath())
	element.attr = make(map[string]string, len(v.Attr))
	for _, attr := range v.Attr {
		attrPath := s.keyGen.GenAttribute(element.path, attr.Name.Local)
		element.attr[attrPath] = attr.Value
	}
}

func (s *xmlStackImpl) Pop() stackElement {
	l := len(s.stack)
	if l == 0 {
		return nil
	}

	res := s.stack[l-1]
	s.stack = s.stack[:l-1]
	return res
}

func (s *xmlStackImpl) Peek() stackElement {
	l := len(s.stack)
	if l == 0 {
		return nil
	}

	return s.stack[l-1]
}

func (s *xmlStackImpl) currPath() (path string) {
	for _, e := range s.stack {
		if len(path) != 0 {
			path += "."
		}
		path += e.name
	}
	return path
}

type stackElement interface {
	Path() string
	Attributes() map[string]string
	CharData() []string
	AppendCharData(charData string)
}

type stackElementImpl struct {
	name     string
	path     string
	attr     map[string]string
	charData []string
}

func (e *stackElementImpl) Path() string {
	return e.path
}

func (e *stackElementImpl) Attributes() map[string]string {
	return e.attr
}

func (e *stackElementImpl) CharData() []string {
	return e.charData
}

func (e *stackElementImpl) AppendCharData(charData string) {
	e.charData = append(e.charData, charData)
}
