// Package smtp implements the Simple Mail Transfer Protocol as defined in RFC 5321.
//
// It also implements the following extensions:
//
//   - 8BITMIME (RFC 1652)
//   - ENHANCEDSTATUSCODES (RFC 2034)
//   - AUTH (RFC 2554)
//   - DELIVERBY (RFC 2852)
//   - CHUNKING (RFC 3030)
//   - BINARYMIME (RFC 3030)
//   - STARTTLS (RFC 3207)
//   - DSN (RFC 3461, RFC 6533)
//   - SMTPUTF8 (RFC 6531)
//   - MT-PRIORITY (RFC 6710)
//   - RRVS (RFC 7293)
//   - REQUIRETLS (RFC 8689)
//
// LMTP (RFC 2033) is also supported.
//
// Additional extensions may be handled by other packages.
package smtp

import (
	"time"
)

type BodyType string

const (
	Body7Bit       BodyType = "7BIT"
	Body8BitMIME   BodyType = "8BITMIME"
	BodyBinaryMIME BodyType = "BINARYMIME"
)

type DSNReturn string

const (
	DSNReturnFull    DSNReturn = "FULL"
	DSNReturnHeaders DSNReturn = "HDRS"
)

// MailOptions contains parameters for the MAIL command.
type MailOptions struct {
	// Value of BODY= argument, 7BIT, 8BITMIME or BINARYMIME.
	Body BodyType

	// Size of the body. Can be 0 if not specified by client.
	Size int64

	// TLS is required for the message transmission.
	//
	// The message should be rejected if it can't be transmitted
	// with TLS.
	RequireTLS bool

	// The message envelope or message header contains UTF-8-encoded strings.
	// This flag is set by SMTPUTF8-aware (RFC 6531) client.
	UTF8 bool

	// Value of RET= argument, FULL or HDRS.
	Return DSNReturn

	// Envelope identifier set by the client.
	EnvelopeID string

	// The authorization identity asserted by the message sender in decoded
	// form with angle brackets stripped.
	//
	// nil value indicates missing AUTH, non-nil empty string indicates
	// AUTH=<>.
	//
	// Defined in RFC 4954.
	Auth *string
}

type DSNNotify string

const (
	DSNNotifyNever   DSNNotify = "NEVER"
	DSNNotifyDelayed DSNNotify = "DELAY"
	DSNNotifyFailure DSNNotify = "FAILURE"
	DSNNotifySuccess DSNNotify = "SUCCESS"
)

type DSNAddressType string

const (
	DSNAddressTypeRFC822 DSNAddressType = "RFC822"
	DSNAddressTypeUTF8   DSNAddressType = "UTF-8"
)

type DeliverByMode string

const (
	DeliverByNotify DeliverByMode = "N"
	DeliverByReturn DeliverByMode = "R"
)

type DeliverByOptions struct {
	Time  time.Duration
	Mode  DeliverByMode
	Trace bool
}

type PriorityProfile string

const (
	PriorityUnspecified PriorityProfile = ""
	PriorityMIXER       PriorityProfile = "MIXER"
	PrioritySTANAG4406  PriorityProfile = "STANAG4406"
	PriorityNSEP        PriorityProfile = "NSEP"
)

// RcptOptions contains parameters for the RCPT command.
type RcptOptions struct {
	// Value of NOTIFY= argument, NEVER or a combination of either of
	// DELAY, FAILURE, SUCCESS.
	Notify []DSNNotify

	// Original recipient set by client.
	OriginalRecipientType DSNAddressType
	OriginalRecipient     string

	// Time value of the RRVS= argument
	// or the zero time if unset.
	RequireRecipientValidSince time.Time

	// Value of BY= argument or nil if unset.
	DeliverBy *DeliverByOptions

	// Value of MT-PRIORITY= or nil if unset.
	MTPriority *int
}
