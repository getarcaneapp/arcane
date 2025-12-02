package util

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"github.com/docker/buildx/store"
	"github.com/google/uuid"
	"github.com/pkg/errors"
)

func GenerateNodeName(builderName string, txn *store.Txn) (string, error) {
	randomName := func() (string, error) {
		u, err := uuid.NewRandom()
		if err != nil {
			return "", err
		}
		return generateName(fmt.Sprintf("buildkit-%s-", u)), nil
	}

	ng, err := txn.NodeGroupByName(builderName)
	if err != nil {
		return randomName()
	}

	var name string
	for range 6 {
		name, err = randomName()
		if err != nil {
			return "", err
		}
		exists := func(name string) bool {
			for _, n := range ng.Nodes {
				if n.Name == name {
					return true
				}
			}
			return false
		}(name)
		if exists {
			continue
		}
		return name, nil
	}

	return "", errors.Errorf("failed to generate random node name")
}

const (
	maxNameLength          = 63
	randomLength           = 5
	maxGeneratedNameLength = maxNameLength - randomLength
)

// generateName generates the name plus a random suffix of five alphanumerics
// when a name is requested. The string is guaranteed to not exceed the length
// of a standard Kubernetes name (63 characters).
//
// It's a simplified implementation of k8s.io/apiserver/pkg/storage/names:
// https://github.com/kubernetes/apiserver/blob/v0.29.2/pkg/storage/names/generate.go#L34-L54
func generateName(base string) string {
	if len(base) > maxGeneratedNameLength {
		base = base[:maxGeneratedNameLength]
	}
	return base + randomSuffix()
}

func randomSuffix() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err) // This shouldn't happen
	}
	return hex.EncodeToString(b)[:randomLength]
}
