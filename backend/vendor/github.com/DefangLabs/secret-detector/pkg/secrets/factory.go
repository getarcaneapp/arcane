package secrets

import (
	"fmt"
	"strings"
	"sync"
)

var (
	once                sync.Once
	detectorsFactory    *detectorFactory
	transformersFactory *transformerFactory
)

func GetDetectorFactory() DetectorFactory {
	initFactories()
	return detectorsFactory
}

func GetTransformerFactory() TransformerFactory {
	initFactories()
	return transformersFactory
}

func initFactories() {
	once.Do(func() {
		detectorsFactory = &detectorFactory{make(map[string]func(config ...string) Detector)}
		transformersFactory = &transformerFactory{make(map[string]func() Transformer)}
	})
}

type DetectorFactory interface {
	Register(name string, initMethod func(config ...string) Detector) error
	Create(names []string, detectorConfigs map[string][]string) (detectors []Detector, missing []string)
}

type detectorFactory struct {
	factoryMethodsMap map[string]func(config ...string) Detector
}

func (f *detectorFactory) Register(name string, initMethod func(config ...string) Detector) error {
	name = strings.ToLower(name)
	if _, exist := f.factoryMethodsMap[name]; exist {
		return fmt.Errorf("detecor '%s' already registered", name)
	}

	f.factoryMethodsMap[name] = initMethod
	return nil
}

func (f *detectorFactory) Create(names []string, detectorConfigs map[string][]string) (detectors []Detector, missing []string) {
	for _, name := range names {
		config := detectorConfigs[name]
		d := f.create(name, config...)
		if d != nil {
			detectors = append(detectors, d)
		} else {
			missing = append(missing, name)
		}
	}
	return
}

func (f *detectorFactory) create(name string, config ...string) Detector {
	name = strings.ToLower(name)
	if factoryMethod, exist := f.factoryMethodsMap[name]; exist {
		return factoryMethod(config...)
	}
	return nil
}

type TransformerFactory interface {
	Register(name string, initMethod func() Transformer) error
	Create(names []string) (transformers []Transformer, missing []string)
}

type transformerFactory struct {
	factoryMethodsMap map[string]func() Transformer
}

func (f *transformerFactory) Register(name string, initMethod func() Transformer) error {
	name = strings.ToLower(name)
	if _, exist := f.factoryMethodsMap[name]; exist {
		return fmt.Errorf("transformer '%s' already registered", name)
	}

	f.factoryMethodsMap[name] = initMethod
	return nil
}

func (f *transformerFactory) Create(names []string) (transformers []Transformer, missing []string) {
	for _, name := range names {
		t := f.create(name)
		if t != nil {
			transformers = append(transformers, t)
		} else {
			missing = append(missing, name)
		}
	}
	return
}

func (f *transformerFactory) create(name string) Transformer {
	name = strings.ToLower(name)
	if factoryMethod, exist := f.factoryMethodsMap[name]; exist {
		return factoryMethod()
	}
	return nil
}
