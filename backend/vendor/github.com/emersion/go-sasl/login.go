package sasl

import (
	"bytes"
)

// The LOGIN mechanism name.
const Login = "LOGIN"

var expectedChallenge = []byte("Password:")

type loginClient struct {
	Username string
	Password string
}

func (a *loginClient) Start() (mech string, ir []byte, err error) {
	mech = "LOGIN"
	ir = []byte(a.Username)
	return
}

func (a *loginClient) Next(challenge []byte) (response []byte, err error) {
	if bytes.Compare(challenge, expectedChallenge) != 0 {
		return nil, ErrUnexpectedServerChallenge
	} else {
		return []byte(a.Password), nil
	}
}

// A client implementation of the LOGIN authentication mechanism for SMTP,
// as described in http://www.iana.org/go/draft-murchison-sasl-login
//
// It is considered obsolete, and should not be used when other mechanisms are
// available. For plaintext password authentication use PLAIN mechanism.
func NewLoginClient(username, password string) Client {
	return &loginClient{username, password}
}
