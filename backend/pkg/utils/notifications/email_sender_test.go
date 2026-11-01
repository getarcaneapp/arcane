package notifications

import (
	"bufio"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"mime"
	"mime/multipart"
	"net"
	"net/mail"
	"net/textproto"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	kit "kit.getarcane.app"
)

const (
	smtpTestHost        = "localhost"
	smtpTestConnTimeout = 2 * time.Second
)

type smtpTestServer struct {
	listener           net.Listener
	tlsConfig          *tls.Config
	clientTLSConfig    *tls.Config
	done               chan error
	authMechanismUsed  string
	message            string
	authMechanisms     []string
	commands           []string
	mu                 sync.Mutex
	supportStartTLS    bool
	authBeforeTLS      bool
	authAfterTLS       bool
	startTLSNegotiated bool
}

func TestSendEmailStartTLSRequiresTLSBeforeAuth(t *testing.T) {
	server := newSMTPTestServer(t, true, []string{"PLAIN"})
	defer server.Close()

	config := EmailConfig{
		SMTPHost:     smtpTestHost,
		SMTPPort:     server.Port(),
		SMTPUsername: "user",
		SMTPPassword: "password",
		FromAddress:  "from@example.com",
		ToAddresses:  []string{"to@example.com"},
		TLSMode:      EmailTLSModeStartTLS,
	}

	err := sendEmail(
		t.Context(),
		config,
		"Arcane STARTTLS Test",
		"<p>Test</p>",
		"",
		smtpBuildOptions{tlsConfig: server.ClientTLSConfig()},
	)
	require.NoError(t, err)
	require.NoError(t, server.Wait())

	assert.True(t, server.StartTLSNegotiated(), "expected STARTTLS to be negotiated")
	assert.True(t, server.AuthAfterTLS(), "expected AUTH to happen after STARTTLS")
	assert.False(t, server.AuthBeforeTLS(), "expected no AUTH attempt before STARTTLS")
	assertCommandOrder(t, server.Commands(), "EHLO", "STARTTLS", "EHLO", "AUTH", "MAIL", "RCPT", "DATA", "QUIT")
}

func TestSendEmailStartTLSFailsWhenServerDoesNotSupportIt(t *testing.T) {
	server := newSMTPTestServer(t, false, []string{"PLAIN"})
	defer server.Close()

	config := EmailConfig{
		SMTPHost:     smtpTestHost,
		SMTPPort:     server.Port(),
		SMTPUsername: "user",
		SMTPPassword: "password",
		FromAddress:  "from@example.com",
		ToAddresses:  []string{"to@example.com"},
		TLSMode:      EmailTLSModeStartTLS,
	}

	err := SendEmail(t.Context(), config, "Arcane STARTTLS Test", "<p>Test</p>", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "STARTTLS")
	assert.NotContains(t, err.Error(), "unencrypted connection")
	require.NoError(t, server.Wait())

	assert.False(t, server.StartTLSNegotiated(), "did not expect STARTTLS to be negotiated")
	assert.False(t, server.AuthBeforeTLS(), "did not expect plaintext AUTH attempt")
	assert.False(t, server.AuthAfterTLS(), "did not expect AUTH after failed STARTTLS setup")
}

func TestSendEmailAuthLoginAgainstExchangeStyleServer(t *testing.T) {
	server := newSMTPTestServer(t, true, []string{"LOGIN"})
	defer server.Close()

	config := EmailConfig{
		SMTPHost:     smtpTestHost,
		SMTPPort:     server.Port(),
		SMTPUsername: "user",
		SMTPPassword: "password",
		FromAddress:  "from@example.com",
		ToAddresses:  []string{"to@example.com"},
		TLSMode:      EmailTLSModeStartTLS,
		AuthMode:     EmailAuthModeLogin,
	}

	err := sendEmail(
		t.Context(),
		config,
		"Arcane AUTH LOGIN Test",
		"<p>Test</p>",
		"",
		smtpBuildOptions{tlsConfig: server.ClientTLSConfig()},
	)
	require.NoError(t, err)
	require.NoError(t, server.Wait())

	assert.True(t, server.StartTLSNegotiated(), "expected STARTTLS to be negotiated")
	assert.Equal(t, "LOGIN", server.AuthMechanismUsed(), "expected AUTH LOGIN to be used")
	assertCommandOrder(t, server.Commands(), "EHLO", "STARTTLS", "EHLO", "AUTH", "MAIL", "RCPT", "DATA", "QUIT")
}

func TestSendEmailStalePasswordWithoutUsernameSkipsAuth(t *testing.T) {
	server := newSMTPTestServer(t, false, nil)
	defer server.Close()

	config := EmailConfig{
		SMTPHost:     smtpTestHost,
		SMTPPort:     server.Port(),
		SMTPPassword: "stale-password",
		FromAddress:  "from@example.com",
		ToAddresses:  []string{"to@example.com"},
		TLSMode:      EmailTLSModeNone,
		AuthMode:     EmailAuthModeAuto,
	}

	err := sendEmail(t.Context(), config, "Arcane No-Auth Test", "<p>Test</p>", "", smtpBuildOptions{})
	require.NoError(t, err)
	require.NoError(t, server.Wait())

	assert.NotContains(t, server.Commands(), "AUTH", "expected no AUTH attempt without complete credentials")
}

func TestSendEmailWithTextBodySendsMultipartAlternative(t *testing.T) {
	server := newSMTPTestServer(t, false, nil)
	defer server.Close()

	config := EmailConfig{
		SMTPHost:    smtpTestHost,
		SMTPPort:    server.Port(),
		FromAddress: "from@example.com",
		ToAddresses: []string{"to@example.com"},
		TLSMode:     EmailTLSModeNone,
	}

	err := sendEmail(t.Context(), config, "Arcane Multipart Test", "<p>HTML body</p>", "Plain body", smtpBuildOptions{})
	require.NoError(t, err)
	require.NoError(t, server.Wait())

	msg, err := mail.ReadMessage(strings.NewReader(server.Message()))
	require.NoError(t, err)

	mediaType, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	require.NoError(t, err)
	require.Equal(t, "multipart/alternative", mediaType)

	parts := map[string]string{}
	reader := multipart.NewReader(msg.Body, params["boundary"])
	for {
		part, partErr := reader.NextPart()
		if errors.Is(partErr, io.EOF) {
			break
		}
		require.NoError(t, partErr)

		partType, _, typeErr := mime.ParseMediaType(part.Header.Get("Content-Type"))
		require.NoError(t, typeErr)

		body, readErr := io.ReadAll(part)
		require.NoError(t, readErr)
		parts[partType] = strings.TrimSpace(string(body))
	}

	assert.Equal(t, map[string]string{
		"text/plain": "Plain body",
		"text/html":  "<p>HTML body</p>",
	}, parts)
}

func newSMTPTestServer(t *testing.T, supportStartTLS bool, authMechanisms []string) *smtpTestServer {
	t.Helper()

	serverCert, certPool := generateServerCertificate(t, smtpTestHost)

	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	require.NoError(t, err)

	server := &smtpTestServer{
		listener:        listener,
		tlsConfig:       &tls.Config{Certificates: []tls.Certificate{serverCert}, MinVersion: tls.VersionTLS12},
		clientTLSConfig: &tls.Config{RootCAs: certPool, ServerName: smtpTestHost, MinVersion: tls.VersionTLS12},
		supportStartTLS: supportStartTLS,
		authMechanisms:  authMechanisms,
		done:            make(chan error, 1),
	}

	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			server.done <- acceptErr
			return
		}

		server.done <- server.handleConnection(conn)
	}()

	return server
}

func (s *smtpTestServer) Port() int {
	return s.listener.Addr().(*net.TCPAddr).Port
}

func (s *smtpTestServer) Close() {
	_ = s.listener.Close()
}

func (s *smtpTestServer) ClientTLSConfig() *tls.Config {
	return s.clientTLSConfig.Clone()
}

func (s *smtpTestServer) Wait() error {
	select {
	case err := <-s.done:
		return kit.Ternary(err != nil && (isUseOfClosedNetworkConn(err) || isConnectionTimeout(err)), nil, err)
	case <-time.After(4 * time.Second):
		return errors.New("timed out waiting for SMTP test server")
	}
}

func (s *smtpTestServer) Commands() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]string, len(s.commands))
	copy(out, s.commands)
	return out
}

func (s *smtpTestServer) AuthBeforeTLS() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.authBeforeTLS
}

func (s *smtpTestServer) AuthAfterTLS() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.authAfterTLS
}

func (s *smtpTestServer) StartTLSNegotiated() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.startTLSNegotiated
}

func (s *smtpTestServer) AuthMechanismUsed() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.authMechanismUsed
}

func (s *smtpTestServer) ehloLines() []string {
	lines := []string{"arcane smtp test server"}
	if s.supportStartTLS {
		lines = append(lines, "STARTTLS")
	}
	if len(s.authMechanisms) > 0 {
		lines = append(lines, "AUTH "+strings.Join(s.authMechanisms, " "))
	}
	return lines
}

func (s *smtpTestServer) ehloLinesAfterTLS() []string {
	lines := []string{"arcane smtp test server"}
	if len(s.authMechanisms) > 0 {
		lines = append(lines, "AUTH "+strings.Join(s.authMechanisms, " "))
	}
	return lines
}

func (s *smtpTestServer) handleConnection(conn net.Conn) error {
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(smtpTestConnTimeout))

	reader := textproto.NewReader(bufio.NewReader(conn))
	writer := textproto.NewWriter(bufio.NewWriter(conn))
	tlsActive := false

	if err := writeSMTPResponse(writer, 220, false, "arcane smtp test server"); err != nil {
		return err
	}

	for {
		line, err := reader.ReadLine()
		if err != nil {
			return kit.Ternary(errors.Is(err, io.EOF), nil, err)
		}

		verb, rest, _ := strings.Cut(line, " ")
		verb = strings.ToUpper(strings.TrimSpace(verb))
		s.recordCommand(verb, tlsActive)

		switch verb {
		case "EHLO", "HELO":
			var lines []string
			if tlsActive {
				lines = s.ehloLinesAfterTLS()
			} else {
				lines = s.ehloLines()
			}
			if writeSMTPMultiLineResponseErr := writeSMTPMultiLineResponse(writer, 250, lines); writeSMTPMultiLineResponseErr != nil {
				return writeSMTPMultiLineResponseErr
			}
		case "STARTTLS":
			if !s.supportStartTLS {
				if writeSMTPResponseErr := writeSMTPResponse(writer, 502, false, "STARTTLS not supported"); writeSMTPResponseErr != nil {
					return writeSMTPResponseErr
				}
				continue
			}

			if writeSMTPResponseErr2 := writeSMTPResponse(writer, 220, false, "ready to start TLS"); writeSMTPResponseErr2 != nil {
				return writeSMTPResponseErr2
			}

			tlsConn := tls.Server(conn, s.tlsConfig)
			if handshakeErr := tlsConn.Handshake(); handshakeErr != nil {
				return handshakeErr
			}

			conn = tlsConn
			_ = conn.SetDeadline(time.Now().Add(smtpTestConnTimeout))
			reader = textproto.NewReader(bufio.NewReader(conn))
			writer = textproto.NewWriter(bufio.NewWriter(conn))
			tlsActive = true
			s.markStartTLSNegotiated()
		default:
			quit, commandErr := s.handleCommand(reader, writer, verb, rest)
			if commandErr != nil {
				return commandErr
			}
			if quit {
				return nil
			}
		}
	}
}

func (s *smtpTestServer) handleCommand(reader *textproto.Reader, writer *textproto.Writer, verb, rest string) (bool, error) {
	switch verb {
	case "AUTH":
		mechanism, _, _ := strings.Cut(strings.TrimSpace(rest), " ")
		mechanism = strings.ToUpper(mechanism)
		s.recordAuthMechanism(mechanism)
		if handleAuthErr := s.handleAuth(reader, writer, mechanism); handleAuthErr != nil {
			return false, handleAuthErr
		}
	case "NOOP", "RSET":
		if writeSMTPResponseErr3 := writeSMTPResponse(writer, 250, false, "2.0.0 OK"); writeSMTPResponseErr3 != nil {
			return false, writeSMTPResponseErr3
		}
	case "MAIL":
		if writeSMTPResponseErr4 := writeSMTPResponse(writer, 250, false, "2.1.0 Sender OK"); writeSMTPResponseErr4 != nil {
			return false, writeSMTPResponseErr4
		}
	case "RCPT":
		if writeSMTPResponseErr5 := writeSMTPResponse(writer, 250, false, "2.1.5 Recipient OK"); writeSMTPResponseErr5 != nil {
			return false, writeSMTPResponseErr5
		}
	case "DATA":
		if writeSMTPResponseErr6 := writeSMTPResponse(writer, 354, false, "End data with <CR><LF>.<CR><LF>"); writeSMTPResponseErr6 != nil {
			return false, writeSMTPResponseErr6
		}
		dataLines, dataErr := reader.ReadDotLines()
		if dataErr != nil {
			return false, dataErr
		}
		s.recordMessage(strings.Join(dataLines, "\r\n"))
		if writeSMTPResponseErr7 := writeSMTPResponse(writer, 250, false, "2.0.0 queued"); writeSMTPResponseErr7 != nil {
			return false, writeSMTPResponseErr7
		}
	case "QUIT":
		if writeSMTPResponseErr8 := writeSMTPResponse(writer, 221, false, "2.0.0 bye"); writeSMTPResponseErr8 != nil {
			return false, writeSMTPResponseErr8
		}
		return true, nil
	default:
		if writeSMTPResponseErr9 := writeSMTPResponse(writer, 502, false, "command not implemented"); writeSMTPResponseErr9 != nil {
			return false, writeSMTPResponseErr9
		}
	}
	return false, nil
}

func (s *smtpTestServer) handleAuth(reader *textproto.Reader, writer *textproto.Writer, mechanism string) error {
	if mechanism == "LOGIN" {
		if err := writeSMTPResponse(writer, 334, false, base64.StdEncoding.EncodeToString([]byte("Username:"))); err != nil {
			return err
		}
		if _, err := reader.ReadLine(); err != nil {
			return err
		}
		if err := writeSMTPResponse(writer, 334, false, base64.StdEncoding.EncodeToString([]byte("Password:"))); err != nil {
			return err
		}
		if _, err := reader.ReadLine(); err != nil {
			return err
		}
	}
	return writeSMTPResponse(writer, 235, false, "2.7.0 Authentication successful")
}

func (s *smtpTestServer) recordCommand(command string, tlsActive bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.commands = append(s.commands, command)
	if command == "AUTH" {
		if tlsActive {
			s.authAfterTLS = true
		} else {
			s.authBeforeTLS = true
		}
	}
}

func (s *smtpTestServer) recordAuthMechanism(mechanism string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.authMechanismUsed = mechanism
}

func (s *smtpTestServer) recordMessage(message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.message = message
}

func (s *smtpTestServer) Message() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.message
}

func (s *smtpTestServer) markStartTLSNegotiated() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.startTLSNegotiated = true
}

func writeSMTPMultiLineResponse(writer *textproto.Writer, code int, lines []string) error {
	for i, line := range lines {
		if err := writeSMTPResponse(writer, code, i < len(lines)-1, line); err != nil {
			return err
		}
	}
	return nil
}

func writeSMTPResponse(writer *textproto.Writer, code int, continued bool, message string) error {
	line := fmt.Sprintf("%d %s", code, message)
	if continued {
		line = fmt.Sprintf("%d-%s", code, message)
	}
	if err := writer.PrintfLine("%s", line); err != nil {
		return err
	}
	return writer.W.Flush()
}

func assertCommandOrder(t *testing.T, commands []string, expected ...string) {
	t.Helper()

	start := 0
	for _, want := range expected {
		found := false
		for i := start; i < len(commands); i++ {
			if commands[i] == want {
				start = i + 1
				found = true
				break
			}
		}
		require.Truef(t, found, "expected command %q in order within %v", want, commands)
	}
}

func generateServerCertificate(t *testing.T, host string) (tls.Certificate, *x509.CertPool) {
	t.Helper()

	caKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	caTemplate := &x509.Certificate{
		SerialNumber:          mustSerialNumber(t),
		Subject:               pkix.Name{CommonName: "Arcane SMTP Test CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	require.NoError(t, err)

	serverKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	serverTemplate := &x509.Certificate{
		SerialNumber: mustSerialNumber(t),
		Subject:      pkix.Name{CommonName: strings.TrimSuffix(host, ".")},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{strings.TrimSuffix(host, "."), host},
	}

	serverDER, err := x509.CreateCertificate(rand.Reader, serverTemplate, caTemplate, &serverKey.PublicKey, caKey)
	require.NoError(t, err)

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: serverDER})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(serverKey)})
	serverCert, err := tls.X509KeyPair(certPEM, keyPEM)
	require.NoError(t, err)

	certPool := x509.NewCertPool()
	require.True(t, certPool.AppendCertsFromPEM(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})))

	return serverCert, certPool
}

func mustSerialNumber(t *testing.T) *big.Int {
	t.Helper()

	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	require.NoError(t, err)
	return serialNumber
}

func isUseOfClosedNetworkConn(err error) bool {
	return err != nil && strings.Contains(err.Error(), "use of closed network connection")
}

func isConnectionTimeout(err error) bool {
	var netErr net.Error
	return err != nil && errors.As(err, &netErr) && netErr.Timeout()
}
