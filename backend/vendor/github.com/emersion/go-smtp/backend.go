package smtp

import (
	"io"

	"github.com/emersion/go-sasl"
)

var (
	ErrAuthFailed = &SMTPError{
		Code:         535,
		EnhancedCode: EnhancedCode{5, 7, 8},
		Message:      "Authentication failed",
	}
	ErrAuthRequired = &SMTPError{
		Code:         502,
		EnhancedCode: EnhancedCode{5, 7, 0},
		Message:      "Please authenticate first",
	}
	ErrAuthUnsupported = &SMTPError{
		Code:         502,
		EnhancedCode: EnhancedCode{5, 7, 0},
		Message:      "Authentication not supported",
	}
	ErrAuthUnknownMechanism = &SMTPError{
		Code:         504,
		EnhancedCode: EnhancedCode{5, 7, 4},
		Message:      "Unsupported authentication mechanism",
	}
)

// A SMTP server backend.
type Backend interface {
	NewSession(c *Conn) (Session, error)
}

// BackendFunc is an adapter to allow the use of an ordinary function as a
// Backend.
type BackendFunc func(c *Conn) (Session, error)

var _ Backend = (BackendFunc)(nil)

// NewSession calls f(c).
func (f BackendFunc) NewSession(c *Conn) (Session, error) {
	return f(c)
}

// Session is used by servers to respond to an SMTP client.
//
// The methods are called when the remote client issues the matching command.
type Session interface {
	// Discard currently processed message.
	Reset()

	// Free all resources associated with session.
	Logout() error

	// Set return path for currently processed message.
	Mail(from string, opts *MailOptions) error
	// Add recipient for currently processed message.
	Rcpt(to string, opts *RcptOptions) error
	// Set currently processed message contents and send it.
	//
	// r must be consumed before Data returns.
	Data(r io.Reader) error
}

// LMTPSession is an add-on interface for Session. It can be implemented by
// LMTP servers to provide extra functionality.
type LMTPSession interface {
	Session

	// LMTPData is the LMTP-specific version of Data method.
	// It can be optionally implemented by the backend to provide
	// per-recipient status information when it is used over LMTP
	// protocol.
	//
	// LMTPData implementation sets status information using passed
	// StatusCollector by calling SetStatus once per each AddRcpt
	// call, even if AddRcpt was called multiple times with
	// the same argument. SetStatus must not be called after
	// LMTPData returns.
	//
	// Return value of LMTPData itself is used as a status for
	// recipients that got no status set before using StatusCollector.
	LMTPData(r io.Reader, status StatusCollector) error
}

// StatusCollector allows a backend to provide per-recipient status
// information.
type StatusCollector interface {
	SetStatus(rcptTo string, err error)
}

// AuthSession is an add-on interface for Session. It provides support for the
// AUTH extension.
type AuthSession interface {
	Session

	AuthMechanisms() []string
	Auth(mech string) (sasl.Server, error)
}
