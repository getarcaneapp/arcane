package registry

import (
	"net/http"
)

// Client provides helper methods for Docker/OCI registries.
type Client struct {
	http *http.Client
}

func NewClient() *Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = http.ProxyFromEnvironment

	return &Client{http: &http.Client{Transport: transport}}
}
