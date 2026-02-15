package http

import (
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"time"
)

func NewHTTPClientWithCertPool(certPool *x509.CertPool) *http.Client {
	return newHTTPClientInternal(10*time.Second, 5*time.Second, certPool)
}

func NewHTTPClientWithTimeoutAndCertPool(timeout time.Duration, certPool *x509.CertPool) *http.Client {
	return newHTTPClientInternal(timeout, 10*time.Second, certPool)
}

func newHTTPClientInternal(timeout, tlsHandshakeTimeout time.Duration, certPool *x509.CertPool) *http.Client {
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   tlsHandshakeTimeout,
		ExpectContinueTimeout: 1 * time.Second,
	}

	if certPool != nil {
		transport.TLSClientConfig = &tls.Config{
			MinVersion: tls.VersionTLS12,
			RootCAs:    certPool,
		}
	}

	return &http.Client{
		Transport: transport,
		Timeout:   timeout,
	}
}
