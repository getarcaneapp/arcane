package xmltransformer

import (
	"fmt"
)

type uniqueKeyGenerator interface {
	Gen(key string) string
	GenAttribute(key, attr string) string
}

type uniqueKeyGeneratorImpl struct {
	keyCounters map[string]int
}

func NewUniqueKeyGenerator() uniqueKeyGenerator {
	return &uniqueKeyGeneratorImpl{
		keyCounters: make(map[string]int),
	}
}

func (u *uniqueKeyGeneratorImpl) Gen(key string) string {
	count, _ := u.keyCounters[key]
	u.keyCounters[key]++
	if count > 0 {
		return fmt.Sprintf("%v~%v", key, count)
	}
	return key
}

func (u *uniqueKeyGeneratorImpl) GenAttribute(key, attr string) string {
	attrPath := fmt.Sprintf("%v[%v]", key, attr)
	count, _ := u.keyCounters[attrPath]
	u.keyCounters[attrPath]++
	if count > 0 {
		return fmt.Sprintf("%v[%v~%v]", key, attr, count)
	}
	return attrPath
}
