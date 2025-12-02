// Copyright 2010 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package smtp

import (
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/textproto"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/emersion/go-sasl"
)

// A Client represents a client connection to an SMTP server.
type Client struct {
	// keep a reference to the connection so it can be used to create a TLS
	// connection later
	conn       net.Conn
	text       *textproto.Conn
	serverName string
	lmtp       bool
	ext        map[string]string // supported extensions
	localName  string            // the name to use in HELO/EHLO/LHLO
	didGreet   bool              // whether we've received greeting from server
	greetError error             // the error from the greeting
	didHello   bool              // whether we've said HELO/EHLO/LHLO
	helloError error             // the error from the hello
	rcpts      []string          // recipients accumulated for the current session

	// Time to wait for command responses (this includes 3xx reply to DATA).
	CommandTimeout time.Duration
	// Time to wait for responses after final dot.
	SubmissionTimeout time.Duration

	// Logger for all network activity.
	DebugWriter io.Writer
}

// 30 seconds was chosen as it's the same duration as http.DefaultTransport's
// timeout.
var defaultDialer = net.Dialer{Timeout: 30 * time.Second}

// Dial returns a new Client connected to an SMTP server at addr. The addr must
// include a port, as in "mail.example.com:smtp".
//
// This function returns a plaintext connection. To enable TLS, use
// DialStartTLS.
func Dial(addr string) (*Client, error) {
	conn, err := defaultDialer.Dial("tcp", addr)
	if err != nil {
		return nil, err
	}
	client := NewClient(conn)
	client.serverName, _, _ = net.SplitHostPort(addr)
	return client, nil
}

// DialTLS returns a new Client connected to an SMTP server via TLS at addr.
// The addr must include a port, as in "mail.example.com:smtps".
//
// A nil tlsConfig is equivalent to a zero tls.Config.
func DialTLS(addr string, tlsConfig *tls.Config) (*Client, error) {
	tlsDialer := tls.Dialer{
		NetDialer: &defaultDialer,
		Config:    tlsConfig,
	}
	conn, err := tlsDialer.Dial("tcp", addr)
	if err != nil {
		return nil, err
	}
	client := NewClient(conn)
	client.serverName, _, _ = net.SplitHostPort(addr)
	return client, nil
}

// DialStartTLS returns a new Client connected to an SMTP server via STARTTLS
// at addr. The addr must include a port, as in "mail.example.com:smtp".
//
// A nil tlsConfig is equivalent to a zero tls.Config.
func DialStartTLS(addr string, tlsConfig *tls.Config) (*Client, error) {
	c, err := Dial(addr)
	if err != nil {
		return nil, err
	}
	if err := initStartTLS(c, tlsConfig); err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}

// NewClient returns a new Client using an existing connection and host as a
// server name to be used when authenticating.
func NewClient(conn net.Conn) *Client {
	c := &Client{
		localName: "localhost",
		// As recommended by RFC 5321. For DATA command reply (3xx one) RFC
		// recommends a slightly shorter timeout but we do not bother
		// differentiating these.
		CommandTimeout: 5 * time.Minute,
		// 10 minutes + 2 minute buffer in case the server is doing transparent
		// forwarding and also follows recommended timeouts.
		SubmissionTimeout: 12 * time.Minute,
	}

	c.setConn(conn)

	return c
}

// NewClientStartTLS creates a new Client and performs a STARTTLS command.
func NewClientStartTLS(conn net.Conn, tlsConfig *tls.Config) (*Client, error) {
	c := NewClient(conn)
	if err := initStartTLS(c, tlsConfig); err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}

func initStartTLS(c *Client, tlsConfig *tls.Config) error {
	if err := c.hello(); err != nil {
		return err
	}
	if ok, _ := c.Extension("STARTTLS"); !ok {
		return errors.New("smtp: server doesn't support STARTTLS")
	}
	if err := c.startTLS(tlsConfig); err != nil {
		return err
	}
	return nil
}

// NewClientLMTP returns a new LMTP Client (as defined in RFC 2033) using an
// existing connection and host as a server name to be used when authenticating.
func NewClientLMTP(conn net.Conn) *Client {
	c := NewClient(conn)
	c.lmtp = true
	return c
}

// setConn sets the underlying network connection for the client.
func (c *Client) setConn(conn net.Conn) {
	c.conn = conn

	var r io.Reader = conn
	var w io.Writer = conn

	r = &lineLimitReader{
		R: conn,
		// Doubled maximum line length per RFC 5321 (Section 4.5.3.1.6)
		LineLimit: 2000,
	}

	r = io.TeeReader(r, clientDebugWriter{c})
	w = io.MultiWriter(w, clientDebugWriter{c})

	rwc := struct {
		io.Reader
		io.Writer
		io.Closer
	}{
		Reader: r,
		Writer: w,
		Closer: conn,
	}
	c.text = textproto.NewConn(rwc)
}

// Close closes the connection.
func (c *Client) Close() error {
	return c.text.Close()
}

func (c *Client) greet() error {
	if c.didGreet {
		return c.greetError
	}

	// Initial greeting timeout. RFC 5321 recommends 5 minutes.
	c.conn.SetDeadline(time.Now().Add(c.CommandTimeout))
	defer c.conn.SetDeadline(time.Time{})

	c.didGreet = true
	_, _, err := c.readResponse(220)
	if err != nil {
		c.greetError = err
		c.text.Close()
	}

	return c.greetError
}

// hello runs a hello exchange if needed.
func (c *Client) hello() error {
	if c.didHello {
		return c.helloError
	}

	if err := c.greet(); err != nil {
		return err
	}

	c.didHello = true
	if err := c.ehlo(); err != nil {
		var smtpError *SMTPError
		if errors.As(err, &smtpError) && (smtpError.Code == 500 || smtpError.Code == 502) {
			// The server doesn't support EHLO, fallback to HELO
			c.helloError = c.helo()
		} else {
			c.helloError = err
		}
	}
	return c.helloError
}

// Hello sends a HELO or EHLO to the server as the given host name.
// Calling this method is only necessary if the client needs control
// over the host name used. The client will introduce itself as "localhost"
// automatically otherwise. If Hello is called, it must be called before
// any of the other methods.
//
// If server returns an error, it will be of type *SMTPError.
func (c *Client) Hello(localName string) error {
	if err := validateLine(localName); err != nil {
		return err
	}
	if c.didHello {
		return errors.New("smtp: Hello called after other methods")
	}
	c.localName = localName
	return c.hello()
}

func (c *Client) readResponse(expectCode int) (int, string, error) {
	code, msg, err := c.text.ReadResponse(expectCode)
	if protoErr, ok := err.(*textproto.Error); ok {
		err = toSMTPErr(protoErr)
	}
	return code, msg, err
}

// cmd is a convenience function that sends a command and returns the response
// textproto.Error returned by c.text.ReadResponse is converted into SMTPError.
func (c *Client) cmd(expectCode int, format string, args ...interface{}) (int, string, error) {
	c.conn.SetDeadline(time.Now().Add(c.CommandTimeout))
	defer c.conn.SetDeadline(time.Time{})

	id, err := c.text.Cmd(format, args...)
	if err != nil {
		return 0, "", err
	}
	c.text.StartResponse(id)
	defer c.text.EndResponse(id)

	return c.readResponse(expectCode)
}

// helo sends the HELO greeting to the server. It should be used only when the
// server does not support ehlo.
func (c *Client) helo() error {
	c.ext = nil
	_, _, err := c.cmd(250, "HELO %s", c.localName)
	return err
}

// ehlo sends the EHLO (extended hello) greeting to the server. It
// should be the preferred greeting for servers that support it.
func (c *Client) ehlo() error {
	cmd := "EHLO"
	if c.lmtp {
		cmd = "LHLO"
	}

	_, msg, err := c.cmd(250, "%s %s", cmd, c.localName)
	if err != nil {
		return err
	}
	ext := make(map[string]string)
	extList := strings.Split(msg, "\n")
	if len(extList) > 1 {
		extList = extList[1:]
		for _, line := range extList {
			args := strings.SplitN(line, " ", 2)
			if len(args) > 1 {
				ext[args[0]] = args[1]
			} else {
				ext[args[0]] = ""
			}
		}
	}
	c.ext = ext
	return err
}

// startTLS sends the STARTTLS command and encrypts all further communication.
// Only servers that advertise the STARTTLS extension support this function.
//
// A nil config is equivalent to a zero tls.Config.
//
// If server returns an error, it will be of type *SMTPError.
func (c *Client) startTLS(config *tls.Config) error {
	if err := c.hello(); err != nil {
		return err
	}
	_, _, err := c.cmd(220, "STARTTLS")
	if err != nil {
		return err
	}
	if config == nil {
		config = &tls.Config{}
	}
	if config.ServerName == "" && c.serverName != "" {
		// Make a copy to avoid polluting argument
		config = config.Clone()
		config.ServerName = c.serverName
	}
	if testHookStartTLS != nil {
		testHookStartTLS(config)
	}
	c.setConn(tls.Client(c.conn, config))
	c.didHello = false
	return nil
}

// TLSConnectionState returns the client's TLS connection state.
// The return values are their zero values if STARTTLS did
// not succeed.
func (c *Client) TLSConnectionState() (state tls.ConnectionState, ok bool) {
	tc, ok := c.conn.(*tls.Conn)
	if !ok {
		return
	}
	return tc.ConnectionState(), true
}

// Verify checks the validity of an email address on the server.
// If Verify returns nil, the address is valid. A non-nil return
// does not necessarily indicate an invalid address. Many servers
// will not verify addresses for security reasons.
//
// If server returns an error, it will be of type *SMTPError.
func (c *Client) Verify(addr string) error {
	if err := validateLine(addr); err != nil {
		return err
	}
	if err := c.hello(); err != nil {
		return err
	}
	_, _, err := c.cmd(250, "VRFY %s", addr)
	return err
}

// Auth authenticates a client using the provided authentication mechanism.
// Only servers that advertise the AUTH extension support this function.
//
// If server returns an error, it will be of type *SMTPError.
func (c *Client) Auth(a sasl.Client) error {
	if err := c.hello(); err != nil {
		return err
	}
	encoding := base64.StdEncoding
	mech, resp, err := a.Start()
	if err != nil {
		return err
	}
	var resp64 []byte
	if len(resp) > 0 {
		resp64 = make([]byte, encoding.EncodedLen(len(resp)))
		encoding.Encode(resp64, resp)
	} else if resp != nil {
		resp64 = []byte{'='}
	}
	code, msg64, err := c.cmd(0, strings.TrimSpace(fmt.Sprintf("AUTH %s %s", mech, resp64)))
	for err == nil {
		var msg []byte
		switch code {
		case 334:
			msg, err = encoding.DecodeString(msg64)
		case 235:
			// the last message isn't base64 because it isn't a challenge
			msg = []byte(msg64)
		default:
			err = toSMTPErr(&textproto.Error{Code: code, Msg: msg64})
		}
		if err == nil {
			if code == 334 {
				resp, err = a.Next(msg)
			} else {
				resp = nil
			}
		}
		if err != nil {
			// abort the AUTH
			c.cmd(501, "*")
			break
		}
		if resp == nil {
			break
		}
		resp64 = make([]byte, encoding.EncodedLen(len(resp)))
		encoding.Encode(resp64, resp)
		code, msg64, err = c.cmd(0, string(resp64))
	}
	return err
}

// Mail issues a MAIL command to the server using the provided email address.
// If the server supports the 8BITMIME extension, Mail adds the BODY=8BITMIME
// parameter.
// This initiates a mail transaction and is followed by one or more Rcpt calls.
//
// If opts is not nil, MAIL arguments provided in the structure will be added
// to the command. Handling of unsupported options depends on the extension.
//
// If server returns an error, it will be of type *SMTPError.
func (c *Client) Mail(from string, opts *MailOptions) error {
	if err := validateLine(from); err != nil {
		return err
	}
	if err := c.hello(); err != nil {
		return err
	}

	var sb strings.Builder
	// A high enough power of 2 than 510+14+26+11+9+9+39+500
	sb.Grow(2048)
	fmt.Fprintf(&sb, "MAIL FROM:<%s>", from)
	if _, ok := c.ext["8BITMIME"]; ok {
		sb.WriteString(" BODY=8BITMIME")
	}
	if _, ok := c.ext["SIZE"]; ok && opts != nil && opts.Size != 0 {
		fmt.Fprintf(&sb, " SIZE=%v", opts.Size)
	}
	if opts != nil && opts.RequireTLS {
		if _, ok := c.ext["REQUIRETLS"]; ok {
			sb.WriteString(" REQUIRETLS")
		} else {
			return errors.New("smtp: server does not support REQUIRETLS")
		}
	}
	if opts != nil && opts.UTF8 {
		if _, ok := c.ext["SMTPUTF8"]; ok {
			sb.WriteString(" SMTPUTF8")
		} else {
			return errors.New("smtp: server does not support SMTPUTF8")
		}
	}
	if _, ok := c.ext["DSN"]; ok && opts != nil {
		switch opts.Return {
		case DSNReturnFull, DSNReturnHeaders:
			fmt.Fprintf(&sb, " RET=%s", string(opts.Return))
		case "":
			// This space is intentionally left blank
		default:
			return errors.New("smtp: Unknown RET parameter value")
		}
		if opts.EnvelopeID != "" {
			if !isPrintableASCII(opts.EnvelopeID) {
				return errors.New("smtp: Malformed ENVID parameter value")
			}
			fmt.Fprintf(&sb, " ENVID=%s", encodeXtext(opts.EnvelopeID))
		}
	}
	if opts != nil && opts.Auth != nil {
		if _, ok := c.ext["AUTH"]; ok {
			fmt.Fprintf(&sb, " AUTH=%s", encodeXtext(*opts.Auth))
		}
		// We can safely discard parameter if server does not support AUTH.
	}
	_, _, err := c.cmd(250, "%s", sb.String())
	return err
}

// Rcpt issues a RCPT command to the server using the provided email address.
// A call to Rcpt must be preceded by a call to Mail and may be followed by
// a Data call or another Rcpt call.
//
// If opts is not nil, RCPT arguments provided in the structure will be added
// to the command. Handling of unsupported options depends on the extension.
//
// If server returns an error, it will be of type *SMTPError.
func (c *Client) Rcpt(to string, opts *RcptOptions) error {
	if err := validateLine(to); err != nil {
		return err
	}

	var sb strings.Builder
	// A high enough power of 2 than 510+29+501
	sb.Grow(2048)
	fmt.Fprintf(&sb, "RCPT TO:<%s>", to)
	if _, ok := c.ext["DSN"]; ok && opts != nil {
		if len(opts.Notify) != 0 {
			sb.WriteString(" NOTIFY=")
			if err := checkNotifySet(opts.Notify); err != nil {
				return errors.New("smtp: Malformed NOTIFY parameter value")
			}
			for i, v := range opts.Notify {
				if i != 0 {
					sb.WriteString(",")
				}
				sb.WriteString(string(v))
			}
		}
		if opts.OriginalRecipient != "" {
			var enc string
			switch opts.OriginalRecipientType {
			case DSNAddressTypeRFC822:
				if !isPrintableASCII(opts.OriginalRecipient) {
					return errors.New("smtp: Illegal address")
				}
				enc = encodeXtext(opts.OriginalRecipient)
			case DSNAddressTypeUTF8:
				if _, ok := c.ext["SMTPUTF8"]; ok {
					enc = encodeUTF8AddrUnitext(opts.OriginalRecipient)
				} else {
					enc = encodeUTF8AddrXtext(opts.OriginalRecipient)
				}
			default:
				return errors.New("smtp: Unknown address type")
			}
			fmt.Fprintf(&sb, " ORCPT=%s;%s", string(opts.OriginalRecipientType), enc)
		}
	}
	if _, ok := c.ext["RRVS"]; ok && opts != nil && !opts.RequireRecipientValidSince.IsZero() {
		sb.WriteString(fmt.Sprintf(" RRVS=%s", opts.RequireRecipientValidSince.Format(time.RFC3339)))
	}
	if _, ok := c.ext["DELIVERBY"]; ok && opts != nil && opts.DeliverBy != nil {
		if opts.DeliverBy.Mode == DeliverByReturn && opts.DeliverBy.Time < 1 {
			return errors.New("smtp: DELIVERBY mode must be greater than zero with return mode")
		}
		arg := fmt.Sprintf(" BY=%d;%s", int(opts.DeliverBy.Time.Seconds()), opts.DeliverBy.Mode)
		if opts.DeliverBy.Trace {
			arg += "T"
		}
		sb.WriteString(arg)
	}
	if _, ok := c.ext["MT-PRIORITY"]; ok && opts != nil && opts.MTPriority != nil {
		if *opts.MTPriority < -9 || *opts.MTPriority > 9 {
			return errors.New("smtp: MT-PRIORITY must be between -9 and 9")
		}
		sb.WriteString(fmt.Sprintf(" MT-PRIORITY=%d", *opts.MTPriority))
	}
	if _, _, err := c.cmd(25, "%s", sb.String()); err != nil {
		return err
	}
	c.rcpts = append(c.rcpts, to)
	return nil
}

// DataCommand is a pending DATA command. DataCommand is an io.WriteCloser.
// See Client.Data.
type DataCommand struct {
	client *Client
	wc     io.WriteCloser

	closeErr error
}

var _ io.WriteCloser = (*DataCommand)(nil)

// Write implements io.Writer.
func (cmd *DataCommand) Write(b []byte) (int, error) {
	return cmd.wc.Write(b)
}

// Close implements io.Closer.
func (cmd *DataCommand) Close() error {
	var err error
	if cmd.client.lmtp {
		_, err = cmd.CloseWithLMTPResponse()
	} else {
		_, err = cmd.CloseWithResponse()
	}
	return err
}

// CloseWithResponse is equivalent to Close, but also returns the server
// response. It cannot be called when the LMTP protocol is used.
//
// If server returns an error, it will be of type *SMTPError.
func (cmd *DataCommand) CloseWithResponse() (*DataResponse, error) {
	if cmd.client.lmtp {
		return nil, errors.New("smtp: CloseWithResponse used with an LMTP client")
	}

	if err := cmd.close(); err != nil {
		return nil, err
	}

	cmd.client.conn.SetDeadline(time.Now().Add(cmd.client.SubmissionTimeout))
	defer cmd.client.conn.SetDeadline(time.Time{})

	_, msg, err := cmd.client.readResponse(250)
	if err != nil {
		cmd.closeErr = err
		return nil, err
	}

	return &DataResponse{StatusText: msg}, nil
}

// CloseWithLMTPResponse is equivalent to Close, but also returns per-recipient
// server responses. It can only be called when the LMTP protocol is used.
//
// If server returns an error, it will be of type LMTPDataError.
func (cmd *DataCommand) CloseWithLMTPResponse() (map[string]*DataResponse, error) {
	if !cmd.client.lmtp {
		return nil, errors.New("smtp: CloseWithLMTPResponse used without an LMTP client")
	}

	if err := cmd.close(); err != nil {
		return nil, err
	}

	cmd.client.conn.SetDeadline(time.Now().Add(cmd.client.SubmissionTimeout))
	defer cmd.client.conn.SetDeadline(time.Time{})

	resp := make(map[string]*DataResponse, len(cmd.client.rcpts))
	lmtpErr := make(LMTPDataError, len(cmd.client.rcpts))
	for i := 0; i < len(cmd.client.rcpts); i++ {
		rcpt := cmd.client.rcpts[i]
		_, msg, err := cmd.client.readResponse(250)
		if err != nil {
			if smtpErr, ok := err.(*SMTPError); ok {
				lmtpErr[rcpt] = smtpErr
			} else {
				if len(lmtpErr) > 0 {
					return resp, errors.Join(err, lmtpErr)
				}
				return resp, err
			}
		} else {
			resp[rcpt] = &DataResponse{StatusText: msg}
		}
	}

	if len(lmtpErr) > 0 {
		return resp, lmtpErr
	}
	return resp, nil
}

func (cmd *DataCommand) close() error {
	if cmd.closeErr != nil {
		return cmd.closeErr
	}

	if err := cmd.wc.Close(); err != nil {
		cmd.closeErr = err
		return err
	}

	cmd.closeErr = errors.New("smtp: data writer closed twice")
	return nil
}

// DataResponse is the response returned by a DATA command. See
// DataCommand.CloseWithResponse.
type DataResponse struct {
	// StatusText is the status text returned by the server. It may contain
	// tracking information.
	StatusText string
}

// LMTPDataError is a collection of errors returned by an LMTP server for a
// DATA command. It holds per-recipient errors.
type LMTPDataError map[string]*SMTPError

// Error implements error.
func (lmtpErr LMTPDataError) Error() string {
	return errors.Join(lmtpErr.Unwrap()...).Error()
}

// Unwrap returns all per-recipient errors returned by the server.
func (lmtpErr LMTPDataError) Unwrap() []error {
	l := make([]error, 0, len(lmtpErr))
	for rcpt, smtpErr := range lmtpErr {
		l = append(l, fmt.Errorf("<%v>: %w", rcpt, smtpErr))
	}
	sort.Slice(l, func(i, j int) bool {
		return l[i].Error() < l[j].Error()
	})
	return l
}

// Data issues a DATA command to the server and returns a writer that
// can be used to write the mail headers and body. The caller should
// close the writer before calling any more methods on c. A call to
// Data must be preceded by one or more calls to Rcpt.
func (c *Client) Data() (*DataCommand, error) {
	_, _, err := c.cmd(354, "DATA")
	if err != nil {
		return nil, err
	}
	return &DataCommand{client: c, wc: c.text.DotWriter()}, nil
}

// SendMail will use an existing connection to send an email from
// address from, to addresses to, with message r.
//
// This function does not start TLS, nor does it perform authentication. Use
// DialStartTLS and Auth before-hand if desirable.
//
// The addresses in the to parameter are the SMTP RCPT addresses.
//
// The r parameter should be an RFC 822-style email with headers
// first, a blank line, and then the message body. The lines of r
// should be CRLF terminated. The r headers should usually include
// fields such as "From", "To", "Subject", and "Cc".  Sending "Bcc"
// messages is accomplished by including an email address in the to
// parameter but not including it in the r headers.
func (c *Client) SendMail(from string, to []string, r io.Reader) error {
	var err error

	if err = c.Mail(from, nil); err != nil {
		return err
	}
	for _, addr := range to {
		if err = c.Rcpt(addr, nil); err != nil {
			return err
		}
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	_, err = io.Copy(w, r)
	if err != nil {
		return err
	}
	return w.Close()
}

var testHookStartTLS func(*tls.Config) // nil, except for tests

func sendMail(addr string, implicitTLS bool, a sasl.Client, from string, to []string, r io.Reader) error {
	var (
		c   *Client
		err error
	)
	if implicitTLS {
		c, err = DialTLS(addr, nil)
	} else {
		c, err = DialStartTLS(addr, nil)
	}
	if err != nil {
		return err
	}
	defer c.Close()

	if a != nil {
		if ok, _ := c.Extension("AUTH"); !ok {
			return errors.New("smtp: server doesn't support AUTH")
		}
		if err = c.Auth(a); err != nil {
			return err
		}
	}

	if err := c.SendMail(from, to, r); err != nil {
		return err
	}

	return c.Quit()
}

// SendMail connects to the server at addr, switches to TLS, authenticates with
// the optional SASL client, and then sends an email from address from, to
// addresses to, with message r. The addr must include a port, as in
// "mail.example.com:smtp".
//
// The addresses in the to parameter are the SMTP RCPT addresses.
//
// The r parameter should be an RFC 822-style email with headers
// first, a blank line, and then the message body. The lines of r
// should be CRLF terminated. The r headers should usually include
// fields such as "From", "To", "Subject", and "Cc".  Sending "Bcc"
// messages is accomplished by including an email address in the to
// parameter but not including it in the r headers.
//
// SendMail is intended to be used for very simple use-cases. If you want to
// customize SendMail's behavior, use a Client instead.
//
// The SendMail function and the go-smtp package are low-level
// mechanisms and provide no support for DKIM signing (see go-msgauth), MIME
// attachments (see the mime/multipart package or the go-message package), or
// other mail functionality.
func SendMail(addr string, a sasl.Client, from string, to []string, r io.Reader) error {
	return sendMail(addr, false, a, from, to, r)
}

// SendMailTLS works like SendMail, but with implicit TLS.
func SendMailTLS(addr string, a sasl.Client, from string, to []string, r io.Reader) error {
	return sendMail(addr, true, a, from, to, r)
}

// Extension reports whether an extension is support by the server.
// The extension name is case-insensitive. If the extension is supported,
// Extension also returns a string that contains any parameters the
// server specifies for the extension.
func (c *Client) Extension(ext string) (bool, string) {
	if err := c.hello(); err != nil {
		return false, ""
	}
	ext = strings.ToUpper(ext)
	param, ok := c.ext[ext]
	return ok, param
}

// SupportsAuth checks whether an authentication mechanism is supported.
func (c *Client) SupportsAuth(mech string) bool {
	if err := c.hello(); err != nil {
		return false
	}
	mechs, ok := c.ext["AUTH"]
	if !ok {
		return false
	}
	for _, m := range strings.Split(mechs, " ") {
		if strings.EqualFold(m, mech) {
			return true
		}
	}
	return false
}

// MaxMessageSize returns the maximum message size accepted by the server.
// 0 means unlimited.
//
// If the server doesn't convey this information, ok = false is returned.
func (c *Client) MaxMessageSize() (size int, ok bool) {
	if err := c.hello(); err != nil {
		return 0, false
	}
	v := c.ext["SIZE"]
	if v == "" {
		return 0, false
	}
	size, err := strconv.Atoi(v)
	if err != nil || size < 0 {
		return 0, false
	}
	return size, true
}

// Reset sends the RSET command to the server, aborting the current mail
// transaction.
func (c *Client) Reset() error {
	if err := c.hello(); err != nil {
		return err
	}
	if _, _, err := c.cmd(250, "RSET"); err != nil {
		return err
	}

	// allow custom HELLO again
	c.didHello = false
	c.helloError = nil

	c.rcpts = nil
	return nil
}

// Noop sends the NOOP command to the server. It does nothing but check
// that the connection to the server is okay.
func (c *Client) Noop() error {
	if err := c.hello(); err != nil {
		return err
	}
	_, _, err := c.cmd(250, "NOOP")
	return err
}

// Quit sends the QUIT command and closes the connection to the server.
//
// If Quit fails the connection is not closed, Close should be used
// in this case.
func (c *Client) Quit() error {
	if err := c.hello(); err != nil {
		return err
	}
	_, _, err := c.cmd(221, "QUIT")
	if err != nil {
		return err
	}
	return c.Close()
}

func parseEnhancedCode(s string) (EnhancedCode, error) {
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return EnhancedCode{}, fmt.Errorf("wrong amount of enhanced code parts")
	}

	code := EnhancedCode{}
	for i, part := range parts {
		num, err := strconv.Atoi(part)
		if err != nil {
			return code, err
		}
		code[i] = num
	}
	return code, nil
}

// toSMTPErr converts textproto.Error into SMTPError, parsing
// enhanced status code if it is present.
func toSMTPErr(protoErr *textproto.Error) *SMTPError {
	smtpErr := &SMTPError{
		Code:    protoErr.Code,
		Message: protoErr.Msg,
	}

	parts := strings.SplitN(protoErr.Msg, " ", 2)
	if len(parts) != 2 {
		return smtpErr
	}

	enchCode, err := parseEnhancedCode(parts[0])
	if err != nil {
		return smtpErr
	}

	msg := parts[1]

	// Per RFC 2034, enhanced code should be prepended to each line.
	msg = strings.ReplaceAll(msg, "\n"+parts[0]+" ", "\n")

	smtpErr.EnhancedCode = enchCode
	smtpErr.Message = msg
	return smtpErr
}

type clientDebugWriter struct {
	c *Client
}

func (cdw clientDebugWriter) Write(b []byte) (int, error) {
	if cdw.c.DebugWriter == nil {
		return len(b), nil
	}
	return cdw.c.DebugWriter.Write(b)
}

// validateLine checks to see if a line has CR or LF.
func validateLine(line string) error {
	if strings.ContainsAny(line, "\n\r") {
		return errors.New("smtp: a line must not contain CR or LF")
	}
	return nil
}
