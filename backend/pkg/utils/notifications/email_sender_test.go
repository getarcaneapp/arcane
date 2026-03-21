package notifications

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/textproto"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const smtpTestHost = "localhost."
const smtpTestConnTimeout = 2 * time.Second

type smtpTestServer struct {
	listener           net.Listener
	tlsConfig          *tls.Config
	supportStartTLS    bool
	done               chan error
	mu                 sync.Mutex
	commands           []string
	authBeforeTLS      bool
	authAfterTLS       bool
	startTLSNegotiated bool
}

func TestBuildSMTPURLInternal(t *testing.T) {
	tests := []struct {
		name    string
		config  models.EmailConfig
		wantURL string
	}{
		{
			name: "Basic SMTP no auth, no TLS",
			config: models.EmailConfig{
				SMTPHost:    "smtp.example.com",
				SMTPPort:    25,
				FromAddress: "from@example.com",
				ToAddresses: []string{"to@example.com"},
				TLSMode:     models.EmailTLSModeNone,
			},
			wantURL: "smtp://smtp.example.com:25/?auth=None&clienthost=localhost&encryption=None&fromaddress=from%40example.com&timeout=10s&toaddresses=to%40example.com&usehtml=Yes&usestarttls=No",
		},
		{
			name: "SMTP with auth and starttls",
			config: models.EmailConfig{
				SMTPHost:     "smtp.example.com",
				SMTPPort:     587,
				SMTPUsername: "user",
				SMTPPassword: "password",
				FromAddress:  "from@example.com",
				ToAddresses:  []string{"to1@example.com", "to2@example.com"},
				TLSMode:      models.EmailTLSModeStartTLS,
			},
			wantURL: "smtp://user:password@smtp.example.com:587/?auth=Plain&clienthost=localhost&encryption=Auto&fromaddress=from%40example.com&requirestarttls=Yes&timeout=10s&toaddresses=to1%40example.com%2Cto2%40example.com&usehtml=Yes&usestarttls=Yes",
		},
		{
			name: "SMTP with SSL/TLS and special characters in credentials",
			config: models.EmailConfig{
				SMTPHost:     "smtp.example.com",
				SMTPPort:     465,
				SMTPUsername: "user@example.com",
				SMTPPassword: "pass/word!",
				FromAddress:  "from@example.com",
				ToAddresses:  []string{"to@example.com"},
				TLSMode:      models.EmailTLSModeSSL,
			},
			wantURL: "smtp://user%40example.com:pass%2Fword%21@smtp.example.com:465/?auth=Plain&clienthost=localhost&encryption=ImplicitTLS&fromaddress=from%40example.com&timeout=10s&toaddresses=to%40example.com&usehtml=Yes&usestarttls=No",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotURL, err := buildSMTPURLInternal(tt.config, smtpBuildOptions{})
			require.NoError(t, err)
			assert.Equal(t, tt.wantURL, gotURL)
		})
	}
}

func TestSendEmailStartTLSRequiresTLSBeforeAuth(t *testing.T) {
	server := newSMTPTestServerInternal(t, true)
	defer server.Close()

	config := models.EmailConfig{
		SMTPHost:     smtpTestHost,
		SMTPPort:     server.Port(),
		SMTPUsername: "user",
		SMTPPassword: "password",
		FromAddress:  "from@example.com",
		ToAddresses:  []string{"to@example.com"},
		TLSMode:      models.EmailTLSModeStartTLS,
	}

	err := sendEmailInternal(
		context.Background(),
		config,
		"Arcane STARTTLS Test",
		"<p>Test</p>",
		smtpBuildOptions{skipTLSVerify: true},
	)
	require.NoError(t, err)
	require.NoError(t, server.Wait())

	assert.True(t, server.StartTLSNegotiated(), "expected STARTTLS to be negotiated")
	assert.True(t, server.AuthAfterTLS(), "expected AUTH to happen after STARTTLS")
	assert.False(t, server.AuthBeforeTLS(), "expected no AUTH attempt before STARTTLS")
	assertCommandOrderInternal(t, server.Commands(), "EHLO", "STARTTLS", "EHLO", "AUTH", "MAIL", "RCPT", "DATA", "QUIT")
}

func TestSendEmailStartTLSFailsBeforePlainAuthFallback(t *testing.T) {
	server := newSMTPTestServerInternal(t, false)
	defer server.Close()

	config := models.EmailConfig{
		SMTPHost:     smtpTestHost,
		SMTPPort:     server.Port(),
		SMTPUsername: "user",
		SMTPPassword: "password",
		FromAddress:  "from@example.com",
		ToAddresses:  []string{"to@example.com"},
		TLSMode:      models.EmailTLSModeStartTLS,
	}

	err := SendEmail(context.Background(), config, "Arcane STARTTLS Test", "<p>Test</p>")
	require.Error(t, err)
	assert.ErrorContains(t, err, "error enabling StartTLS")
	assert.NotContains(t, err.Error(), "unencrypted connection")
	require.NoError(t, server.Wait())

	assert.False(t, server.StartTLSNegotiated(), "did not expect STARTTLS to be negotiated")
	assert.False(t, server.AuthBeforeTLS(), "did not expect plaintext AUTH attempt")
	assert.False(t, server.AuthAfterTLS(), "did not expect AUTH after failed STARTTLS setup")
}

func newSMTPTestServerInternal(t *testing.T, supportStartTLS bool) *smtpTestServer {
	t.Helper()

	serverCert := generateServerCertificateInternal(t, smtpTestHost)

	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	require.NoError(t, err)

	server := &smtpTestServer{
		listener:        listener,
		tlsConfig:       &tls.Config{Certificates: []tls.Certificate{serverCert}, MinVersion: tls.VersionTLS12},
		supportStartTLS: supportStartTLS,
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

func (s *smtpTestServer) Wait() error {
	select {
	case err := <-s.done:
		if err != nil && (isUseOfClosedNetworkConnInternal(err) || isConnectionTimeoutInternal(err)) {
			return nil
		}
		return err
	case <-time.After(4 * time.Second):
		return fmt.Errorf("timed out waiting for SMTP test server")
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

func (s *smtpTestServer) handleConnection(conn net.Conn) error {
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(smtpTestConnTimeout))

	reader := textproto.NewReader(bufio.NewReader(conn))
	writer := textproto.NewWriter(bufio.NewWriter(conn))
	tlsActive := false

	if err := writeSMTPResponseInternal(writer, 220, false, "arcane smtp test server"); err != nil {
		return err
	}

	for {
		line, err := reader.ReadLine()
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}

		verb, _, _ := strings.Cut(line, " ")
		verb = strings.ToUpper(strings.TrimSpace(verb))
		s.recordCommand(verb, tlsActive)

		switch verb {
		case "EHLO", "HELO":
			if s.supportStartTLS && !tlsActive {
				if err := writeSMTPMultiLineResponseInternal(writer, 250, []string{
					"arcane smtp test server",
					"STARTTLS",
					"AUTH PLAIN",
				}); err != nil {
					return err
				}
				continue
			}

			if err := writeSMTPMultiLineResponseInternal(writer, 250, []string{
				"arcane smtp test server",
				"AUTH PLAIN",
			}); err != nil {
				return err
			}
		case "STARTTLS":
			if !s.supportStartTLS {
				if err := writeSMTPResponseInternal(writer, 502, false, "STARTTLS not supported"); err != nil {
					return err
				}
				continue
			}

			if err := writeSMTPResponseInternal(writer, 220, false, "ready to start TLS"); err != nil {
				return err
			}

			tlsConn := tls.Server(conn, s.tlsConfig)
			if err := tlsConn.Handshake(); err != nil {
				return err
			}

			conn = tlsConn
			_ = conn.SetDeadline(time.Now().Add(smtpTestConnTimeout))
			reader = textproto.NewReader(bufio.NewReader(conn))
			writer = textproto.NewWriter(bufio.NewWriter(conn))
			tlsActive = true
			s.markStartTLSNegotiated()
		case "AUTH":
			if err := writeSMTPResponseInternal(writer, 235, false, "2.7.0 Authentication successful"); err != nil {
				return err
			}
		case "MAIL":
			if err := writeSMTPResponseInternal(writer, 250, false, "2.1.0 Sender OK"); err != nil {
				return err
			}
		case "RCPT":
			if err := writeSMTPResponseInternal(writer, 250, false, "2.1.5 Recipient OK"); err != nil {
				return err
			}
		case "DATA":
			if err := writeSMTPResponseInternal(writer, 354, false, "End data with <CR><LF>.<CR><LF>"); err != nil {
				return err
			}
			for {
				dataLine, dataErr := reader.ReadLine()
				if dataErr != nil {
					return dataErr
				}
				if dataLine == "." {
					break
				}
			}
			if err := writeSMTPResponseInternal(writer, 250, false, "2.0.0 queued"); err != nil {
				return err
			}
		case "QUIT":
			if err := writeSMTPResponseInternal(writer, 221, false, "2.0.0 bye"); err != nil {
				return err
			}
			return nil
		default:
			if err := writeSMTPResponseInternal(writer, 502, false, "command not implemented"); err != nil {
				return err
			}
		}
	}
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

func (s *smtpTestServer) markStartTLSNegotiated() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.startTLSNegotiated = true
}

func writeSMTPMultiLineResponseInternal(writer *textproto.Writer, code int, lines []string) error {
	for i, line := range lines {
		if err := writeSMTPResponseInternal(writer, code, i < len(lines)-1, line); err != nil {
			return err
		}
	}
	return nil
}

func writeSMTPResponseInternal(writer *textproto.Writer, code int, continued bool, message string) error {
	line := fmt.Sprintf("%d %s", code, message)
	if continued {
		line = fmt.Sprintf("%d-%s", code, message)
	}
	if err := writer.PrintfLine("%s", line); err != nil {
		return err
	}
	return writer.W.Flush()
}

func assertCommandOrderInternal(t *testing.T, commands []string, expected ...string) {
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

func generateServerCertificateInternal(t *testing.T, host string) tls.Certificate {
	t.Helper()

	caKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	caTemplate := &x509.Certificate{
		SerialNumber:          mustSerialNumberInternal(t),
		Subject:               pkix.Name{CommonName: "Arcane SMTP Test CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	_, err = x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	require.NoError(t, err)

	serverKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	serverTemplate := &x509.Certificate{
		SerialNumber: mustSerialNumberInternal(t),
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

	return serverCert
}

func mustSerialNumberInternal(t *testing.T) *big.Int {
	t.Helper()

	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	require.NoError(t, err)
	return serialNumber
}

func isUseOfClosedNetworkConnInternal(err error) bool {
	return err != nil && strings.Contains(err.Error(), "use of closed network connection")
}

func isConnectionTimeoutInternal(err error) bool {
	var netErr net.Error
	return err != nil && errors.As(err, &netErr) && netErr.Timeout()
}
