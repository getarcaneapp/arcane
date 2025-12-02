package remoteutil

import (
	"net/url"
	"slices"

	"github.com/pkg/errors"
)

var schemes = []string{
	"docker-container",
	"kube-pod",
	"npipe",
	"ssh",
	"tcp",
	"unix",
}

func IsValidEndpoint(ep string) error {
	endpoint, err := url.Parse(ep)
	if err != nil {
		return errors.Wrapf(err, "failed to parse endpoint %s", ep)
	}
	if _, ok := slices.BinarySearch(schemes, endpoint.Scheme); !ok {
		return errors.Errorf("unrecognized url scheme %s", endpoint.Scheme)
	}
	return nil
}
