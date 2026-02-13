package registry

import (
	"crypto/tls"
	"crypto/x509"
	"net/http"
)

// Client provides helper methods for Docker/OCI registries.
type Client struct {
	http *http.Client
}

func NewClient(certPool *x509.CertPool) *Client {
	return NewClientWithInsecure(certPool, false)
}

func NewClientWithInsecure(certPool *x509.CertPool, insecure bool) *Client {
	transport := cloneTransportInternal(nil)
	transport.Proxy = http.ProxyFromEnvironment

	if certPool != nil || insecure {
		tlsCfg := &tls.Config{
			MinVersion:         tls.VersionTLS12,
			InsecureSkipVerify: insecure, // #nosec G402 - this is optional and controlled by explicit insecure-registry setting
		}
		if certPool != nil {
			tlsCfg.RootCAs = certPool
		}
		transport.TLSClientConfig = tlsCfg
	}

	return &Client{http: &http.Client{Transport: transport}}
}

func cloneTransportInternal(rt http.RoundTripper) *http.Transport {
	if rt != nil {
		if transport, ok := rt.(*http.Transport); ok {
			return transport.Clone()
		}
	}

	if defaultTransport, ok := http.DefaultTransport.(*http.Transport); ok {
		return defaultTransport.Clone()
	}

	return &http.Transport{}
}
