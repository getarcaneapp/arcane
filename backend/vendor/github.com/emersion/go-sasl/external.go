package sasl

import (
	"bytes"
	"errors"
)

// The EXTERNAL mechanism name.
const External = "EXTERNAL"

type externalClient struct {
	Identity string
}

func (a *externalClient) Start() (mech string, ir []byte, err error) {
	mech = External
	ir = []byte(a.Identity)
	return
}

func (a *externalClient) Next(challenge []byte) (response []byte, err error) {
	return nil, ErrUnexpectedServerChallenge
}

// An implementation of the EXTERNAL authentication mechanism, as described in
// RFC 4422. Authorization identity may be left blank to indicate that the
// client is requesting to act as the identity associated with the
// authentication credentials.
func NewExternalClient(identity string) Client {
	return &externalClient{identity}
}

// ExternalAuthenticator authenticates users with the EXTERNAL mechanism. If
// the identity is left blank, it indicates that it is the same as the one used
// in the external credentials. If identity is not empty and the server doesn't
// support it, an error must be returned.
type ExternalAuthenticator func(identity string) error

type externalServer struct {
	done         bool
	authenticate ExternalAuthenticator
}

func (a *externalServer) Next(response []byte) (challenge []byte, done bool, err error) {
	if a.done {
		return nil, false, ErrUnexpectedClientResponse
	}

	// No initial response, send an empty challenge
	if response == nil {
		return []byte{}, false, nil
	}

	a.done = true

	if bytes.Contains(response, []byte("\x00")) {
		return nil, false, errors.New("sasl: identity contains a NUL character")
	}

	return nil, true, a.authenticate(string(response))
}

// NewExternalServer creates a server implementation of the EXTERNAL
// authentication mechanism, as described in RFC 4422.
func NewExternalServer(authenticator ExternalAuthenticator) Server {
	return &externalServer{authenticate: authenticator}
}
