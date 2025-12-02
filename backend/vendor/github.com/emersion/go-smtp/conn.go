package smtp

import (
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/textproto"
	"regexp"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/emersion/go-sasl"
)

// Number of errors we'll tolerate per connection before closing. Defaults to 3.
const errThreshold = 3

type Conn struct {
	conn   net.Conn
	text   *textproto.Conn
	server *Server
	helo   string

	// Number of errors witnessed on this connection
	errCount int

	session    Session
	locker     sync.Mutex
	binarymime bool

	lineLimitReader *lineLimitReader
	bdatPipe        *io.PipeWriter
	bdatStatus      *statusCollector // used for BDAT on LMTP
	dataResult      chan error
	bytesReceived   int64 // counts total size of chunks when BDAT is used

	fromReceived bool
	recipients   []string
	didAuth      bool
}

func newConn(c net.Conn, s *Server) *Conn {
	sc := &Conn{
		server: s,
		conn:   c,
	}

	sc.init()
	return sc
}

func (c *Conn) init() {
	c.lineLimitReader = &lineLimitReader{
		R:         c.conn,
		LineLimit: c.server.MaxLineLength,
	}
	rwc := struct {
		io.Reader
		io.Writer
		io.Closer
	}{
		Reader: c.lineLimitReader,
		Writer: c.conn,
		Closer: c.conn,
	}

	if c.server.Debug != nil {
		rwc = struct {
			io.Reader
			io.Writer
			io.Closer
		}{
			io.TeeReader(rwc.Reader, c.server.Debug),
			io.MultiWriter(rwc.Writer, c.server.Debug),
			rwc.Closer,
		}
	}

	c.text = textproto.NewConn(rwc)
}

// Commands are dispatched to the appropriate handler functions.
func (c *Conn) handle(cmd string, arg string) {
	// If panic happens during command handling - send 421 response
	// and close connection.
	defer func() {
		if err := recover(); err != nil {
			c.writeResponse(421, EnhancedCode{4, 0, 0}, "Internal server error")
			c.Close()

			stack := debug.Stack()
			c.server.ErrorLog.Printf("panic serving %v: %v\n%s", c.conn.RemoteAddr(), err, stack)
		}
	}()

	if cmd == "" {
		c.protocolError(500, EnhancedCode{5, 5, 2}, "Error: bad syntax")
		return
	}

	cmd = strings.ToUpper(cmd)
	switch cmd {
	case "SEND", "SOML", "SAML", "EXPN", "HELP", "TURN":
		// These commands are not implemented in any state
		c.writeResponse(502, EnhancedCode{5, 5, 1}, fmt.Sprintf("%v command not implemented", cmd))
	case "HELO", "EHLO", "LHLO":
		lmtp := cmd == "LHLO"
		enhanced := lmtp || cmd == "EHLO"
		if c.server.LMTP && !lmtp {
			c.writeResponse(500, EnhancedCode{5, 5, 1}, "This is a LMTP server, use LHLO")
			return
		}
		if !c.server.LMTP && lmtp {
			c.writeResponse(500, EnhancedCode{5, 5, 1}, "This is not a LMTP server")
			return
		}
		c.handleGreet(enhanced, arg)
	case "MAIL":
		c.handleMail(arg)
	case "RCPT":
		c.handleRcpt(arg)
	case "VRFY":
		c.writeResponse(252, EnhancedCode{2, 5, 0}, "Cannot VRFY user, but will accept message")
	case "NOOP":
		c.writeResponse(250, EnhancedCode{2, 0, 0}, "I have successfully done nothing")
	case "RSET": // Reset session
		c.reset()
		c.writeResponse(250, EnhancedCode{2, 0, 0}, "Session reset")
	case "BDAT":
		c.handleBdat(arg)
	case "DATA":
		c.handleData(arg)
	case "QUIT":
		c.writeResponse(221, EnhancedCode{2, 0, 0}, "Bye")
		c.Close()
	case "AUTH":
		c.handleAuth(arg)
	case "STARTTLS":
		c.handleStartTLS()
	default:
		msg := fmt.Sprintf("Syntax errors, %v command unrecognized", cmd)
		c.protocolError(500, EnhancedCode{5, 5, 2}, msg)
	}
}

func (c *Conn) Server() *Server {
	return c.server
}

func (c *Conn) Session() Session {
	c.locker.Lock()
	defer c.locker.Unlock()
	return c.session
}

func (c *Conn) setSession(session Session) {
	c.locker.Lock()
	defer c.locker.Unlock()
	c.session = session
}

func (c *Conn) Close() error {
	c.locker.Lock()
	defer c.locker.Unlock()

	if c.bdatPipe != nil {
		c.bdatPipe.CloseWithError(ErrDataReset)
		c.bdatPipe = nil
	}

	if c.session != nil {
		c.session.Logout()
		c.session = nil
	}

	return c.conn.Close()
}

// TLSConnectionState returns the connection's TLS connection state.
// Zero values are returned if the connection doesn't use TLS.
func (c *Conn) TLSConnectionState() (state tls.ConnectionState, ok bool) {
	tc, ok := c.conn.(*tls.Conn)
	if !ok {
		return
	}
	return tc.ConnectionState(), true
}

func (c *Conn) Hostname() string {
	return c.helo
}

func (c *Conn) Conn() net.Conn {
	return c.conn
}

func (c *Conn) authAllowed() bool {
	_, isTLS := c.TLSConnectionState()
	return isTLS || c.server.AllowInsecureAuth
}

// protocolError writes errors responses and closes the connection once too many
// have occurred.
func (c *Conn) protocolError(code int, ec EnhancedCode, msg string) {
	c.writeResponse(code, ec, msg)

	c.errCount++
	if c.errCount > errThreshold {
		c.writeResponse(500, EnhancedCode{5, 5, 1}, "Too many errors. Quiting now")
		c.Close()
	}
}

// GREET state -> waiting for HELO
func (c *Conn) handleGreet(enhanced bool, arg string) {
	domain, err := parseHelloArgument(arg)
	if err != nil {
		c.writeResponse(501, EnhancedCode{5, 5, 2}, "Domain/address argument required for HELO")
		return
	}
	// c.helo is populated before NewSession so
	// NewSession can access it via Conn.Hostname.
	c.helo = domain

	// RFC 5321: "An EHLO command MAY be issued by a client later in the session"
	if c.session != nil {
		// RFC 5321: "... the SMTP server MUST clear all buffers
		// and reset the state exactly as if a RSET command has been issued."
		c.reset()
	} else {
		sess, err := c.server.Backend.NewSession(c)
		if err != nil {
			c.helo = ""
			c.writeError(451, EnhancedCode{4, 0, 0}, err)
			return
		}

		c.setSession(sess)
	}

	if !enhanced {
		c.writeResponse(250, EnhancedCode{2, 0, 0}, fmt.Sprintf("Hello %s", domain))
		return
	}

	caps := []string{
		"PIPELINING",
		"8BITMIME",
		"ENHANCEDSTATUSCODES",
		"CHUNKING",
	}
	if _, isTLS := c.TLSConnectionState(); c.server.TLSConfig != nil && !isTLS {
		caps = append(caps, "STARTTLS")
	}
	if c.authAllowed() {
		mechs := c.authMechanisms()

		authCap := "AUTH"
		for _, name := range mechs {
			authCap += " " + name
		}

		if len(mechs) > 0 {
			caps = append(caps, authCap)
		}
	}
	if c.server.EnableSMTPUTF8 {
		caps = append(caps, "SMTPUTF8")
	}
	if _, isTLS := c.TLSConnectionState(); isTLS && c.server.EnableREQUIRETLS {
		caps = append(caps, "REQUIRETLS")
	}
	if c.server.EnableBINARYMIME {
		caps = append(caps, "BINARYMIME")
	}
	if c.server.EnableDSN {
		caps = append(caps, "DSN")
	}
	if c.server.MaxMessageBytes > 0 {
		caps = append(caps, fmt.Sprintf("SIZE %v", c.server.MaxMessageBytes))
	} else {
		caps = append(caps, "SIZE")
	}
	if c.server.MaxRecipients > 0 {
		caps = append(caps, fmt.Sprintf("LIMITS RCPTMAX=%v", c.server.MaxRecipients))
	}
	if c.server.EnableRRVS {
		caps = append(caps, "RRVS")
	}
	if c.server.EnableDELIVERBY {
		if c.server.MinimumDeliverByTime == 0 {
			caps = append(caps, "DELIVERBY")
		} else {
			caps = append(caps, fmt.Sprintf("DELIVERBY %d", int(c.server.MinimumDeliverByTime.Seconds())))
		}
	}
	if c.server.EnableMTPRIORITY {
		if c.server.MtPriorityProfile == PriorityUnspecified {
			caps = append(caps, "MT-PRIORITY")
		} else {
			caps = append(caps, fmt.Sprintf("MT-PRIORITY %s", c.server.MtPriorityProfile))
		}
	}

	args := []string{"Hello " + domain}
	args = append(args, caps...)
	c.writeResponse(250, NoEnhancedCode, args...)
}

// READY state -> waiting for MAIL
func (c *Conn) handleMail(arg string) {
	if c.helo == "" {
		c.writeResponse(502, EnhancedCode{5, 5, 1}, "Please introduce yourself first.")
		return
	}
	if c.bdatPipe != nil {
		c.writeResponse(502, EnhancedCode{5, 5, 1}, "MAIL not allowed during message transfer")
		return
	}

	arg, ok := cutPrefixFold(arg, "FROM:")
	if !ok {
		c.writeResponse(501, EnhancedCode{5, 5, 2}, "Was expecting MAIL arg syntax of FROM:<address>")
		return
	}

	p := parser{s: strings.TrimSpace(arg)}
	from, err := p.parseReversePath()
	if err != nil {
		c.writeResponse(501, EnhancedCode{5, 5, 2}, "Was expecting MAIL arg syntax of FROM:<address>")
		return
	}
	args, err := parseArgs(p.s)
	if err != nil {
		c.writeResponse(501, EnhancedCode{5, 5, 4}, "Unable to parse MAIL ESMTP parameters")
		return
	}

	opts := &MailOptions{}

	c.binarymime = false
	// This is where the Conn may put BODY=8BITMIME, but we already
	// read the DATA as bytes, so it does not effect our processing.
	for key, value := range args {
		switch key {
		case "SIZE":
			size, err := strconv.ParseUint(value, 10, 32)
			if err != nil {
				c.writeResponse(501, EnhancedCode{5, 5, 4}, "Unable to parse SIZE as an integer")
				return
			}

			if c.server.MaxMessageBytes > 0 && int64(size) > c.server.MaxMessageBytes {
				c.writeResponse(552, EnhancedCode{5, 3, 4}, "Max message size exceeded")
				return
			}

			opts.Size = int64(size)
		case "SMTPUTF8":
			if !c.server.EnableSMTPUTF8 {
				c.writeResponse(504, EnhancedCode{5, 5, 4}, "SMTPUTF8 is not implemented")
				return
			}
			opts.UTF8 = true
		case "REQUIRETLS":
			if !c.server.EnableREQUIRETLS {
				c.writeResponse(504, EnhancedCode{5, 5, 4}, "REQUIRETLS is not implemented")
				return
			}
			opts.RequireTLS = true
		case "BODY":
			value = strings.ToUpper(value)
			switch BodyType(value) {
			case BodyBinaryMIME:
				if !c.server.EnableBINARYMIME {
					c.writeResponse(504, EnhancedCode{5, 5, 4}, "BINARYMIME is not implemented")
					return
				}
				c.binarymime = true
			case Body7Bit, Body8BitMIME:
				// This space is intentionally left blank
			default:
				c.writeResponse(501, EnhancedCode{5, 5, 4}, "Unknown BODY value")
				return
			}
			opts.Body = BodyType(value)
		case "RET":
			if !c.server.EnableDSN {
				c.writeResponse(504, EnhancedCode{5, 5, 4}, "RET is not implemented")
				return
			}
			value = strings.ToUpper(value)
			switch DSNReturn(value) {
			case DSNReturnFull, DSNReturnHeaders:
				// This space is intentionally left blank
			default:
				c.writeResponse(501, EnhancedCode{5, 5, 4}, "Unknown RET value")
				return
			}
			opts.Return = DSNReturn(value)
		case "ENVID":
			if !c.server.EnableDSN {
				c.writeResponse(504, EnhancedCode{5, 5, 4}, "ENVID is not implemented")
				return
			}
			value, err := decodeXtext(value)
			if err != nil || value == "" || !isPrintableASCII(value) {
				c.writeResponse(501, EnhancedCode{5, 5, 4}, "Malformed ENVID parameter value")
				return
			}
			opts.EnvelopeID = value
		case "AUTH":
			value, err := decodeXtext(value)
			if err != nil || value == "" {
				c.writeResponse(500, EnhancedCode{5, 5, 4}, "Malformed AUTH parameter value")
				return
			}
			if value == "<>" {
				value = ""
			} else {
				p := parser{s: value}
				value, err = p.parseMailbox()
				if err != nil || p.s != "" {
					c.writeResponse(500, EnhancedCode{5, 5, 4}, "Malformed AUTH parameter mailbox")
					return
				}
			}
			opts.Auth = &value
		default:
			c.writeResponse(500, EnhancedCode{5, 5, 4}, "Unknown MAIL FROM argument")
			return
		}
	}

	if err := c.Session().Mail(from, opts); err != nil {
		c.writeError(451, EnhancedCode{4, 0, 0}, err)
		return
	}

	c.writeResponse(250, EnhancedCode{2, 0, 0}, fmt.Sprintf("Roger, accepting mail from <%v>", from))
	c.fromReceived = true
}

// This regexp matches 'hexchar' token defined in
// https://tools.ietf.org/html/rfc4954#section-8 however it is intentionally
// relaxed by requiring only '+' to be present.  It allows us to detect
// malformed values such as +A or +HH and report them appropriately.
var hexcharRe = regexp.MustCompile(`\+[0-9A-F]?[0-9A-F]?`)

func decodeXtext(val string) (string, error) {
	if !strings.Contains(val, "+") {
		return val, nil
	}

	var replaceErr error
	decoded := hexcharRe.ReplaceAllStringFunc(val, func(match string) string {
		if len(match) != 3 {
			replaceErr = errors.New("incomplete hexchar")
			return ""
		}
		char, err := strconv.ParseInt(match, 16, 8)
		if err != nil {
			replaceErr = err
			return ""
		}

		return string(rune(char))
	})
	if replaceErr != nil {
		return "", replaceErr
	}

	return decoded, nil
}

// This regexp matches 'EmbeddedUnicodeChar' token defined in
// https://datatracker.ietf.org/doc/html/rfc6533.html#section-3
// however it is intentionally relaxed by requiring only '\x{HEX}' to be
// present.  It also matches disallowed characters in QCHAR and QUCHAR defined
// in above.
// So it allows us to detect malformed values and report them appropriately.
var eUOrDCharRe = regexp.MustCompile(`\\x[{][0-9A-F]+[}]|[[:cntrl:] \\+=]`)

// Decodes the utf-8-addr-xtext or the utf-8-addr-unitext form.
func decodeUTF8AddrXtext(val string) (string, error) {
	var replaceErr error
	decoded := eUOrDCharRe.ReplaceAllStringFunc(val, func(match string) string {
		if len(match) == 1 {
			replaceErr = errors.New("disallowed character:" + match)
			return ""
		}

		hexpoint := match[3 : len(match)-1]
		char, err := strconv.ParseUint(hexpoint, 16, 21)
		if err != nil {
			replaceErr = err
			return ""
		}
		switch len(hexpoint) {
		case 2:
			switch {
			// all xtext-specials
			case 0x01 <= char && char <= 0x09 ||
				0x11 <= char && char <= 0x19 ||
				char == 0x10 || char == 0x20 ||
				char == 0x2B || char == 0x3D || char == 0x7F:
			// 2-digit forms
			case char == 0x5C || 0x80 <= char && char <= 0xFF:
				// This space is intentionally left blank
			default:
				replaceErr = errors.New("illegal hexpoint:" + hexpoint)
				return ""
			}
		// 3-digit forms
		case 3:
			switch {
			case 0x100 <= char && char <= 0xFFF:
				// This space is intentionally left blank
			default:
				replaceErr = errors.New("illegal hexpoint:" + hexpoint)
				return ""
			}
		// 4-digit forms excluding surrogate
		case 4:
			switch {
			case 0x1000 <= char && char <= 0xD7FF:
			case 0xE000 <= char && char <= 0xFFFF:
				// This space is intentionally left blank
			default:
				replaceErr = errors.New("illegal hexpoint:" + hexpoint)
				return ""
			}
		// 5-digit forms
		case 5:
			switch {
			case 0x1_0000 <= char && char <= 0xF_FFFF:
				// This space is intentionally left blank
			default:
				replaceErr = errors.New("illegal hexpoint:" + hexpoint)
				return ""
			}
		// 6-digit forms
		case 6:
			switch {
			case 0x10_0000 <= char && char <= 0x10_FFFF:
				// This space is intentionally left blank
			default:
				replaceErr = errors.New("illegal hexpoint:" + hexpoint)
				return ""
			}
		// the other invalid forms
		default:
			replaceErr = errors.New("illegal hexpoint:" + hexpoint)
			return ""
		}

		return string(rune(char))
	})
	if replaceErr != nil {
		return "", replaceErr
	}

	return decoded, nil
}

func decodeTypedAddress(val string) (DSNAddressType, string, error) {
	tv := strings.SplitN(val, ";", 2)
	if len(tv) != 2 || tv[0] == "" || tv[1] == "" {
		return "", "", errors.New("bad address")
	}
	aType, aAddr := strings.ToUpper(tv[0]), tv[1]

	var err error
	switch DSNAddressType(aType) {
	case DSNAddressTypeRFC822:
		aAddr, err = decodeXtext(aAddr)
		if err == nil && !isPrintableASCII(aAddr) {
			err = errors.New("illegal address:" + aAddr)
		}
	case DSNAddressTypeUTF8:
		aAddr, err = decodeUTF8AddrXtext(aAddr)
	default:
		err = errors.New("unknown address type:" + aType)
	}
	if err != nil {
		return "", "", err
	}

	return DSNAddressType(aType), aAddr, nil
}

func encodeXtext(raw string) string {
	var out strings.Builder
	out.Grow(len(raw))

	for _, ch := range raw {
		switch {
		case ch >= '!' && ch <= '~' && ch != '+' && ch != '=':
			// printable non-space US-ASCII except '+' and '='
			out.WriteRune(ch)
		default:
			out.WriteRune('+')
			out.WriteString(strings.ToUpper(strconv.FormatInt(int64(ch), 16)))
		}
	}
	return out.String()
}

// Encodes raw string to the utf-8-addr-xtext form in RFC 6533.
func encodeUTF8AddrXtext(raw string) string {
	var out strings.Builder
	out.Grow(len(raw))

	for _, ch := range raw {
		switch {
		case ch >= '!' && ch <= '~' && ch != '+' && ch != '=':
			// printable non-space US-ASCII except '+' and '='
			out.WriteRune(ch)
		default:
			out.WriteRune('\\')
			out.WriteRune('x')
			out.WriteRune('{')
			out.WriteString(strings.ToUpper(strconv.FormatInt(int64(ch), 16)))
			out.WriteRune('}')
		}
	}
	return out.String()
}

// Encodes raw string to the utf-8-addr-unitext form in RFC 6533.
func encodeUTF8AddrUnitext(raw string) string {
	var out strings.Builder
	out.Grow(len(raw))

	for _, ch := range raw {
		switch {
		case ch >= '!' && ch <= '~' && ch != '+' && ch != '=':
			// printable non-space US-ASCII except '+' and '='
			out.WriteRune(ch)
		case ch <= '\x7F':
			// other ASCII: CTLs, space and specials
			out.WriteRune('\\')
			out.WriteRune('x')
			out.WriteRune('{')
			out.WriteString(strings.ToUpper(strconv.FormatInt(int64(ch), 16)))
			out.WriteRune('}')
		default:
			// UTF-8 non-ASCII
			out.WriteRune(ch)
		}
	}
	return out.String()
}

func isPrintableASCII(val string) bool {
	for _, ch := range val {
		if ch < ' ' || '~' < ch {
			return false
		}
	}
	return true
}

// MAIL state -> waiting for RCPTs followed by DATA
func (c *Conn) handleRcpt(arg string) {
	if !c.fromReceived {
		c.writeResponse(502, EnhancedCode{5, 5, 1}, "Missing MAIL FROM command.")
		return
	}
	if c.bdatPipe != nil {
		c.writeResponse(502, EnhancedCode{5, 5, 1}, "RCPT not allowed during message transfer")
		return
	}

	arg, ok := cutPrefixFold(arg, "TO:")
	if !ok {
		c.writeResponse(501, EnhancedCode{5, 5, 2}, "Was expecting RCPT arg syntax of TO:<address>")
		return
	}

	p := parser{s: strings.TrimSpace(arg)}
	recipient, err := p.parsePath()
	if err != nil {
		c.writeResponse(501, EnhancedCode{5, 5, 2}, "Was expecting RCPT arg syntax of TO:<address>")
		return
	}

	if c.server.MaxRecipients > 0 && len(c.recipients) >= c.server.MaxRecipients {
		c.writeResponse(452, EnhancedCode{4, 5, 3}, fmt.Sprintf("Maximum limit of %v recipients reached", c.server.MaxRecipients))
		return
	}

	args, err := parseArgs(p.s)
	if err != nil {
		c.writeResponse(501, EnhancedCode{5, 5, 4}, "Unable to parse RCPT ESMTP parameters")
		return
	}

	opts := &RcptOptions{}

	for key, value := range args {
		switch key {
		case "NOTIFY":
			if !c.server.EnableDSN {
				c.writeResponse(504, EnhancedCode{5, 5, 4}, "NOTIFY is not implemented")
				return
			}
			notify := []DSNNotify{}
			for _, val := range strings.Split(value, ",") {
				notify = append(notify, DSNNotify(strings.ToUpper(val)))
			}
			if err := checkNotifySet(notify); err != nil {
				c.writeResponse(501, EnhancedCode{5, 5, 4}, "Malformed NOTIFY parameter value")
				return
			}
			opts.Notify = notify
		case "ORCPT":
			if !c.server.EnableDSN {
				c.writeResponse(504, EnhancedCode{5, 5, 4}, "ORCPT is not implemented")
				return
			}
			aType, aAddr, err := decodeTypedAddress(value)
			if err != nil || aAddr == "" {
				c.writeResponse(501, EnhancedCode{5, 5, 4}, "Malformed ORCPT parameter value")
				return
			}
			opts.OriginalRecipientType = aType
			opts.OriginalRecipient = aAddr
		case "RRVS":
			if !c.server.EnableRRVS {
				c.writeResponse(504, EnhancedCode{5, 5, 4}, "RRVS is not implemented")
				return
			}
			value, _, _ = strings.Cut(value, ";") // discard the no-support action
			rrvsTime, err := time.Parse(time.RFC3339, value)
			if err != nil {
				c.writeResponse(501, EnhancedCode{5, 5, 4}, "Malformed RRVS parameter value")
				return
			}
			opts.RequireRecipientValidSince = rrvsTime
		case "BY":
			if !c.server.EnableDELIVERBY {
				c.writeResponse(504, EnhancedCode{5, 5, 4}, "DELIVERBY is not implemented")
				return
			}
			deliverBy := parseDeliverByArgument(value)
			if deliverBy == nil {
				c.writeResponse(501, EnhancedCode{5, 5, 4}, "Malformed BY parameter value")
				return
			}
			if c.server.MinimumDeliverByTime != 0 &&
				deliverBy.Mode == DeliverByReturn &&
				deliverBy.Time < c.server.MinimumDeliverByTime {
				c.writeResponse(501, EnhancedCode{5, 5, 4}, "BY parameter is below server minimum")
				return
			}
			opts.DeliverBy = deliverBy
		case "MT-PRIORITY":
			if !c.server.EnableMTPRIORITY {
				c.writeResponse(504, EnhancedCode{5, 5, 4}, "MT-PRIORITY is not implemented")
				return
			}
			mtPriority, err := strconv.Atoi(value)
			if err != nil {
				c.writeResponse(501, EnhancedCode{5, 5, 4}, "Malformed MT-PRIORITY parameter value")
				return
			}
			if mtPriority < -9 || mtPriority > 9 {
				c.writeResponse(501, EnhancedCode{5, 5, 4}, "MT-PRIORITY is outside valid range")
				return
			}
			opts.MTPriority = &mtPriority
		default:
			c.writeResponse(500, EnhancedCode{5, 5, 4}, "Unknown RCPT TO argument")
			return
		}
	}

	if err := c.Session().Rcpt(recipient, opts); err != nil {
		c.writeError(451, EnhancedCode{4, 0, 0}, err)
		return
	}
	c.recipients = append(c.recipients, recipient)
	c.writeResponse(250, EnhancedCode{2, 0, 0}, fmt.Sprintf("I'll make sure <%v> gets this", recipient))
}

func checkNotifySet(values []DSNNotify) error {
	if len(values) == 0 {
		return errors.New("Malformed NOTIFY parameter value")
	}

	seen := map[DSNNotify]struct{}{}
	for _, val := range values {
		switch val {
		case DSNNotifyNever, DSNNotifyDelayed, DSNNotifyFailure, DSNNotifySuccess:
			if _, ok := seen[val]; ok {
				return errors.New("Malformed NOTIFY parameter value")
			}
		default:
			return errors.New("Malformed NOTIFY parameter value")
		}
		seen[val] = struct{}{}
	}
	if _, ok := seen[DSNNotifyNever]; ok && len(seen) > 1 {
		return errors.New("Malformed NOTIFY parameter value")
	}

	return nil
}

func (c *Conn) handleAuth(arg string) {
	if c.helo == "" {
		c.writeResponse(502, EnhancedCode{5, 5, 1}, "Please introduce yourself first.")
		return
	}
	if c.didAuth {
		c.writeResponse(503, EnhancedCode{5, 5, 1}, "Already authenticated")
		return
	}

	parts := strings.Fields(arg)
	if len(parts) == 0 {
		c.writeResponse(502, EnhancedCode{5, 5, 4}, "Missing parameter")
		return
	}

	if !c.authAllowed() {
		c.writeResponse(523, EnhancedCode{5, 7, 10}, "TLS is required")
		return
	}

	mechanism := strings.ToUpper(parts[0])

	// Parse client initial response if there is one
	var ir []byte
	if len(parts) > 1 {
		var err error
		ir, err = decodeSASLResponse(parts[1])
		if err != nil {
			c.writeResponse(454, EnhancedCode{4, 7, 0}, "Invalid base64 data")
			return
		}
	}

	sasl, err := c.auth(mechanism)
	if err != nil {
		c.writeError(454, EnhancedCode{4, 7, 0}, err)
		return
	}

	response := ir
	for {
		challenge, done, err := sasl.Next(response)
		if err != nil {
			c.writeError(454, EnhancedCode{4, 7, 0}, err)
			return
		}

		if done {
			break
		}

		encoded := ""
		if len(challenge) > 0 {
			encoded = base64.StdEncoding.EncodeToString(challenge)
		}
		c.writeResponse(334, NoEnhancedCode, encoded)

		encoded, err = c.readLine()
		if err != nil {
			return // TODO: error handling
		}

		if encoded == "*" {
			// https://tools.ietf.org/html/rfc4954#page-4
			c.writeResponse(501, EnhancedCode{5, 0, 0}, "Negotiation cancelled")
			return
		}

		response, err = decodeSASLResponse(encoded)
		if err != nil {
			c.writeResponse(454, EnhancedCode{4, 7, 0}, "Invalid base64 data")
			return
		}
	}

	c.writeResponse(235, EnhancedCode{2, 0, 0}, "Authentication succeeded")
	c.didAuth = true
}

func decodeSASLResponse(s string) ([]byte, error) {
	if s == "=" {
		return []byte{}, nil
	}
	return base64.StdEncoding.DecodeString(s)
}

func (c *Conn) authMechanisms() []string {
	if authSession, ok := c.Session().(AuthSession); ok {
		return authSession.AuthMechanisms()
	}
	return nil
}

func (c *Conn) auth(mech string) (sasl.Server, error) {
	if authSession, ok := c.Session().(AuthSession); ok {
		return authSession.Auth(mech)
	}
	return nil, ErrAuthUnknownMechanism
}

func (c *Conn) handleStartTLS() {
	if _, isTLS := c.TLSConnectionState(); isTLS {
		c.writeResponse(502, EnhancedCode{5, 5, 1}, "Already running in TLS")
		return
	}

	if c.server.TLSConfig == nil {
		c.writeResponse(502, EnhancedCode{5, 5, 1}, "TLS not supported")
		return
	}

	c.writeResponse(220, EnhancedCode{2, 0, 0}, "Ready to start TLS")

	// Upgrade to TLS
	tlsConn := tls.Server(c.conn, c.server.TLSConfig)

	if err := tlsConn.Handshake(); err != nil {
		c.writeResponse(550, EnhancedCode{5, 0, 0}, "Handshake error")
		return
	}

	c.conn = tlsConn
	c.init()

	// Reset all state and close the previous Session.
	// This is different from just calling reset() since we want the Backend to
	// be able to see the information about TLS connection in the
	// ConnectionState object passed to it.
	if session := c.Session(); session != nil {
		session.Logout()
		c.setSession(nil)
	}
	c.helo = ""
	c.didAuth = false
	c.reset()
}

// DATA
func (c *Conn) handleData(arg string) {
	if arg != "" {
		c.writeResponse(501, EnhancedCode{5, 5, 4}, "DATA command should not have any arguments")
		return
	}
	if c.bdatPipe != nil {
		c.writeResponse(502, EnhancedCode{5, 5, 1}, "DATA not allowed during message transfer")
		return
	}
	if c.binarymime {
		c.writeResponse(502, EnhancedCode{5, 5, 1}, "DATA not allowed for BINARYMIME messages")
		return
	}

	if !c.fromReceived || len(c.recipients) == 0 {
		c.writeResponse(502, EnhancedCode{5, 5, 1}, "Missing RCPT TO command.")
		return
	}

	// We have recipients, go to accept data
	c.writeResponse(354, NoEnhancedCode, "Go ahead. End your data with <CR><LF>.<CR><LF>")

	defer c.reset()

	if c.server.LMTP {
		c.handleDataLMTP()
		return
	}

	r := newDataReader(c)
	code, enhancedCode, msg := dataErrorToStatus(c.Session().Data(r))
	r.limited = false
	io.Copy(ioutil.Discard, r) // Make sure all the data has been consumed
	c.writeResponse(code, enhancedCode, msg)
}

func (c *Conn) handleBdat(arg string) {
	args := strings.Fields(arg)
	if len(args) == 0 {
		c.writeResponse(501, EnhancedCode{5, 5, 4}, "Missing chunk size argument")
		return
	}
	if len(args) > 2 {
		c.writeResponse(501, EnhancedCode{5, 5, 4}, "Too many arguments")
		return
	}

	if !c.fromReceived || len(c.recipients) == 0 {
		c.writeResponse(502, EnhancedCode{5, 5, 1}, "Missing RCPT TO command.")
		return
	}

	last := false
	if len(args) == 2 {
		if !strings.EqualFold(args[1], "LAST") {
			c.writeResponse(501, EnhancedCode{5, 5, 4}, "Unknown BDAT argument")
			return
		}
		last = true
	}

	// ParseUint instead of Atoi so we will not accept negative values.
	size, err := strconv.ParseUint(args[0], 10, 32)
	if err != nil {
		c.writeResponse(501, EnhancedCode{5, 5, 4}, "Malformed size argument")
		return
	}

	if c.server.MaxMessageBytes != 0 && c.bytesReceived+int64(size) > c.server.MaxMessageBytes {
		c.writeResponse(552, EnhancedCode{5, 3, 4}, "Max message size exceeded")

		// Discard chunk itself without passing it to backend.
		io.Copy(ioutil.Discard, io.LimitReader(c.text.R, int64(size)))

		c.reset()
		return
	}

	if c.bdatStatus == nil && c.server.LMTP {
		c.bdatStatus = c.createStatusCollector()
	}

	if c.bdatPipe == nil {
		var r *io.PipeReader
		r, c.bdatPipe = io.Pipe()

		c.dataResult = make(chan error, 1)

		go func() {
			defer func() {
				if err := recover(); err != nil {
					c.handlePanic(err, c.bdatStatus)

					c.dataResult <- errPanic
					r.CloseWithError(errPanic)
				}
			}()

			var err error
			if !c.server.LMTP {
				err = c.Session().Data(r)
			} else {
				lmtpSession, ok := c.Session().(LMTPSession)
				if !ok {
					err = c.Session().Data(r)
					for _, rcpt := range c.recipients {
						c.bdatStatus.SetStatus(rcpt, err)
					}
				} else {
					err = lmtpSession.LMTPData(r, c.bdatStatus)
				}
			}

			c.dataResult <- err
			r.CloseWithError(err)
		}()
	}

	c.lineLimitReader.LineLimit = 0

	chunk := io.LimitReader(c.text.R, int64(size))
	_, err = io.Copy(c.bdatPipe, chunk)
	if err != nil {
		// Backend might return an error early using CloseWithError without consuming
		// the whole chunk.
		io.Copy(ioutil.Discard, chunk)

		c.writeResponse(dataErrorToStatus(err))

		if err == errPanic {
			c.Close()
		}

		c.reset()
		c.lineLimitReader.LineLimit = c.server.MaxLineLength
		return
	}

	c.bytesReceived += int64(size)

	if last {
		c.lineLimitReader.LineLimit = c.server.MaxLineLength

		c.bdatPipe.Close()

		err := <-c.dataResult

		if c.server.LMTP {
			c.bdatStatus.fillRemaining(err)
			for i, rcpt := range c.recipients {
				code, enchCode, msg := dataErrorToStatus(<-c.bdatStatus.status[i])
				c.writeResponse(code, enchCode, "<"+rcpt+"> "+msg)
			}
		} else {
			c.writeResponse(dataErrorToStatus(err))
		}

		if err == errPanic {
			c.Close()
			return
		}

		c.reset()
	} else {
		c.writeResponse(250, EnhancedCode{2, 0, 0}, "Continue")
	}
}

// ErrDataReset is returned by Reader pased to Data function if client does not
// send another BDAT command and instead closes connection or issues RSET command.
var ErrDataReset = errors.New("smtp: message transmission aborted")

var errPanic = &SMTPError{
	Code:         421,
	EnhancedCode: EnhancedCode{4, 0, 0},
	Message:      "Internal server error",
}

func (c *Conn) handlePanic(err interface{}, status *statusCollector) {
	if status != nil {
		status.fillRemaining(errPanic)
	}

	stack := debug.Stack()
	c.server.ErrorLog.Printf("panic serving %v: %v\n%s", c.conn.RemoteAddr(), err, stack)
}

func (c *Conn) createStatusCollector() *statusCollector {
	rcptCounts := make(map[string]int, len(c.recipients))

	status := &statusCollector{
		statusMap: make(map[string]chan error, len(c.recipients)),
		status:    make([]chan error, 0, len(c.recipients)),
	}
	for _, rcpt := range c.recipients {
		rcptCounts[rcpt]++
	}
	// Create channels with buffer sizes necessary to fit all
	// statuses for a single recipient to avoid deadlocks.
	for rcpt, count := range rcptCounts {
		status.statusMap[rcpt] = make(chan error, count)
	}
	for _, rcpt := range c.recipients {
		status.status = append(status.status, status.statusMap[rcpt])
	}

	return status
}

type statusCollector struct {
	// Contains map from recipient to list of channels that are used for that
	// recipient.
	statusMap map[string]chan error

	// Contains channels from statusMap, in the same
	// order as Conn.recipients.
	status []chan error
}

// fillRemaining sets status for all recipients SetStatus was not called for before.
func (s *statusCollector) fillRemaining(err error) {
	// Amount of times certain recipient was specified is indicated by the channel
	// buffer size, so once we fill it, we can be confident that we sent
	// at least as much statuses as needed. Extra statuses will be ignored anyway.
chLoop:
	for _, ch := range s.statusMap {
		for {
			select {
			case ch <- err:
			default:
				continue chLoop
			}
		}
	}
}

func (s *statusCollector) SetStatus(rcptTo string, err error) {
	ch := s.statusMap[rcptTo]
	if ch == nil {
		panic("SetStatus is called for recipient that was not specified before")
	}

	select {
	case ch <- err:
	default:
		// There enough buffer space to fit all statuses at once, if this is
		// not the case - backend is doing something wrong.
		panic("SetStatus is called more times than particular recipient was specified")
	}
}

func (c *Conn) handleDataLMTP() {
	r := newDataReader(c)
	status := c.createStatusCollector()

	done := make(chan bool, 1)

	lmtpSession, ok := c.Session().(LMTPSession)
	if !ok {
		// Fallback to using a single status for all recipients.
		err := c.Session().Data(r)
		io.Copy(ioutil.Discard, r) // Make sure all the data has been consumed
		for _, rcpt := range c.recipients {
			status.SetStatus(rcpt, err)
		}
		done <- true
	} else {
		go func() {
			defer func() {
				if err := recover(); err != nil {
					status.fillRemaining(&SMTPError{
						Code:         421,
						EnhancedCode: EnhancedCode{4, 0, 0},
						Message:      "Internal server error",
					})

					stack := debug.Stack()
					c.server.ErrorLog.Printf("panic serving %v: %v\n%s", c.conn.RemoteAddr(), err, stack)
					done <- false
				}
			}()

			status.fillRemaining(lmtpSession.LMTPData(r, status))
			io.Copy(ioutil.Discard, r) // Make sure all the data has been consumed
			done <- true
		}()
	}

	for i, rcpt := range c.recipients {
		code, enchCode, msg := dataErrorToStatus(<-status.status[i])
		c.writeResponse(code, enchCode, "<"+rcpt+"> "+msg)
	}

	// If done gets false, the panic occured in LMTPData and the connection
	// should be closed.
	if !<-done {
		c.Close()
	}
}

func dataErrorToStatus(err error) (code int, enchCode EnhancedCode, msg string) {
	if err != nil {
		if smtperr, ok := err.(*SMTPError); ok {
			return smtperr.Code, smtperr.EnhancedCode, smtperr.Message
		} else {
			return 554, EnhancedCode{5, 0, 0}, "Error: transaction failed: " + err.Error()
		}
	}

	return 250, EnhancedCode{2, 0, 0}, "OK: queued"
}

func (c *Conn) Reject() {
	c.writeResponse(421, EnhancedCode{4, 4, 5}, "Too busy. Try again later.")
	c.Close()
}

func (c *Conn) greet() {
	protocol := "ESMTP"
	if c.server.LMTP {
		protocol = "LMTP"
	}
	c.writeResponse(220, NoEnhancedCode, fmt.Sprintf("%v %s Service Ready", c.server.Domain, protocol))
}

func (c *Conn) writeResponse(code int, enhCode EnhancedCode, text ...string) {
	// TODO: error handling
	if c.server.WriteTimeout != 0 {
		c.conn.SetWriteDeadline(time.Now().Add(c.server.WriteTimeout))
	}

	// All responses must include an enhanced code, if it is missing - use
	// a generic code X.0.0.
	if enhCode == EnhancedCodeNotSet {
		cat := code / 100
		switch cat {
		case 2, 4, 5:
			enhCode = EnhancedCode{cat, 0, 0}
		default:
			enhCode = NoEnhancedCode
		}
	}

	// transform each single line with \n, into separate lines
	text = strings.Split(strings.Join(text, "\n"), "\n")

	lastLineIndex := len(text) - 1
	for i := 0; i < lastLineIndex; i++ {
		c.text.PrintfLine("%d-%v", code, text[i])
	}
	if enhCode == NoEnhancedCode {
		c.text.PrintfLine("%d %v", code, text[lastLineIndex])
	} else {
		c.text.PrintfLine("%d %v.%v.%v %v", code, enhCode[0], enhCode[1], enhCode[2], text[lastLineIndex])
	}
}

func (c *Conn) writeError(code int, enhCode EnhancedCode, err error) {
	if smtpErr, ok := err.(*SMTPError); ok {
		c.writeResponse(smtpErr.Code, smtpErr.EnhancedCode, smtpErr.Message)
	} else {
		c.writeResponse(code, enhCode, err.Error())
	}
}

// Reads a line of input
func (c *Conn) readLine() (string, error) {
	if c.server.ReadTimeout != 0 {
		if err := c.conn.SetReadDeadline(time.Now().Add(c.server.ReadTimeout)); err != nil {
			return "", err
		}
	}

	return c.text.ReadLine()
}

func (c *Conn) reset() {
	c.locker.Lock()
	defer c.locker.Unlock()

	if c.bdatPipe != nil {
		c.bdatPipe.CloseWithError(ErrDataReset)
		c.bdatPipe = nil
	}
	c.bdatStatus = nil
	c.bytesReceived = 0

	if c.session != nil {
		c.session.Reset()
	}

	c.fromReceived = false
	c.recipients = nil
}
