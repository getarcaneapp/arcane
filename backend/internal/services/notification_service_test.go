package services

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type smtpTestServer struct {
	ln       net.Listener
	received chan string
	closeWg  sync.WaitGroup
}

func startSMTPTestServer(t *testing.T) (*smtpTestServer, string) {
	t.Helper()

	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)

	s := &smtpTestServer{
		ln:       ln,
		received: make(chan string, 1),
	}

	s.closeWg.Add(1)
	go func() {
		defer s.closeWg.Done()
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		r := bufio.NewReader(conn)
		w := bufio.NewWriter(conn)

		writeLine := func(line string) {
			_, _ = w.WriteString(line + "\r\n")
			_ = w.Flush()
		}

		writeLine("220 localhost ESMTP")

		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			cmd := strings.TrimSpace(line)
			upper := strings.ToUpper(cmd)

			switch {
			case strings.HasPrefix(upper, "EHLO"):
				// Minimal EHLO response with terminator.
				_, _ = w.WriteString("250-localhost\r\n")
				_, _ = w.WriteString("250 OK\r\n")
				_ = w.Flush()
			case strings.HasPrefix(upper, "HELO"):
				writeLine("250 OK")
			case strings.HasPrefix(upper, "MAIL FROM"):
				writeLine("250 OK")
			case strings.HasPrefix(upper, "RCPT TO"):
				writeLine("250 OK")
			case strings.HasPrefix(upper, "DATA"):
				writeLine("354 End data with <CR><LF>.<CR><LF>")

				var msg strings.Builder
				for {
					l, err := r.ReadString('\n')
					if err != nil {
						return
					}
					// DATA terminator is a line with a single dot.
					trimmed := strings.TrimRight(l, "\r\n")
					if trimmed == "." {
						break
					}
					msg.WriteString(l)
				}

				s.received <- msg.String()
				writeLine("250 OK")
			case strings.HasPrefix(upper, "QUIT"):
				writeLine("221 Bye")
				return
			default:
				// Accept anything else.
				writeLine("250 OK")
			}
		}
	}()

	t.Cleanup(func() {
		_ = ln.Close()
		s.closeWg.Wait()
	})

	addr := ln.Addr().String()
	return s, addr
}

func splitHeadersAndBody(raw string) (headers string, body string) {
	// SMTP uses CRLF, but we support LF too to keep parsing resilient.
	if parts := strings.SplitN(raw, "\r\n\r\n", 2); len(parts) == 2 {
		return parts[0], parts[1]
	}
	if parts := strings.SplitN(raw, "\n\n", 2); len(parts) == 2 {
		return parts[0], parts[1]
	}
	return raw, ""
}

func TestEnsureSMTPUseHTML(t *testing.T) {
	got, err := ensureSMTPUseHTML("smtp://u:p@localhost:25/?fromaddress=a%40b.com&toaddresses=c%40d.com", true)
	require.NoError(t, err)
	require.Contains(t, got, "usehtml=Yes")

	got2, err := ensureSMTPUseHTML("discord://token@123", true)
	require.NoError(t, err)
	require.Equal(t, "discord://token@123", got2)
}

func TestSendShoutrrrNotification_SMTP_HTML_SetsMultipartHeader(t *testing.T) {
	s, addr := startSMTPTestServer(t)
	_, port, err := net.SplitHostPort(addr)
	require.NoError(t, err)

	urlStr := fmt.Sprintf(
		"smtp://user:pass@127.0.0.1:%s/?auth=None&usestarttls=No&fromaddress=arcane%%40example.com&fromname=Arcane&toaddresses=rcpt%%40example.com&subject=ignored",
		port,
	)

	svc := &NotificationService{}
	err = svc.sendShoutrrrNotification(context.Background(), urlStr, "<html><body>Hello</body></html>", "Test Email from Arcane", true)
	require.NoError(t, err)

	select {
	case raw := <-s.received:
		headers, body := splitHeadersAndBody(raw)
		require.Contains(t, headers, "MIME-version: 1.0")
		require.Contains(t, headers, "Subject: Test Email from Arcane")
		require.Contains(t, headers, "Content-Type: multipart/alternative; boundary=")
		require.Contains(t, body, "Content-Type: text/html; charset=\"UTF-8\"")
		require.Contains(t, body, "<html>")
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for SMTP message")
	}
}

func TestSendShoutrrrNotification_SMTP_Plain_SetsPlainHeader(t *testing.T) {
	s, addr := startSMTPTestServer(t)
	_, port, err := net.SplitHostPort(addr)
	require.NoError(t, err)

	urlStr := fmt.Sprintf(
		"smtp://user:pass@127.0.0.1:%s/?auth=None&usestarttls=No&fromaddress=arcane%%40example.com&fromname=Arcane&toaddresses=rcpt%%40example.com&subject=ignored",
		port,
	)

	svc := &NotificationService{}
	err = svc.sendShoutrrrNotification(context.Background(), urlStr, "hello", "Plain Subject", false)
	require.NoError(t, err)

	select {
	case raw := <-s.received:
		headers, body := splitHeadersAndBody(raw)
		require.Contains(t, headers, "Subject: Plain Subject")
		require.Contains(t, headers, "Content-Type: text/plain; charset=\"UTF-8\"")
		require.NotContains(t, headers, "multipart/alternative")
		require.Contains(t, body, "hello")
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for SMTP message")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestSendShoutrrrNotification_Discord_TitleParamIsApplied(t *testing.T) {
	origTransport := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = origTransport })

	hit := make(chan struct{}, 1)

	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		defer func() { hit <- struct{}{} }()
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "discord.com", r.URL.Host)
		require.Equal(t, "/api/webhooks/123456/token", r.URL.Path)

		b, err := io.ReadAll(r.Body)
		require.NoError(t, err)

		var payload map[string]any
		require.NoError(t, json.Unmarshal(b, &payload))
		embeds, ok := payload["embeds"].([]any)
		require.True(t, ok)
		require.NotEmpty(t, embeds)
		first, ok := embeds[0].(map[string]any)
		require.True(t, ok)
		require.Equal(t, "My Title", first["title"])

		resp := &http.Response{
			StatusCode: http.StatusNoContent,
			Status:     "204 No Content",
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("")),
			Request:    r,
		}
		return resp, nil
	})

	svc := &NotificationService{}
	err := svc.sendShoutrrrNotification(context.Background(), "discord://token@123456", "hello", "My Title", false)
	require.NoError(t, err)

	select {
	case <-hit:
		// ok
	case <-time.After(2 * time.Second):
		t.Fatal("expected HTTP request")
	}
}

func TestSendShoutrrrNotification_Providers(t *testing.T) {
	origTransport := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = origTransport })

	// We use a local test server for services that allow overriding the host (like Gotify, ntfy).
	// For others with hardcoded hosts (Slack, Discord, Telegram, Pushbullet, Pushover),
	// we rely on mocking http.DefaultTransport.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// This will be overridden per test case if needed, but we need a default
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{}"))
	}))
	defer ts.Close()

	tsURL, _ := url.Parse(ts.URL)
	tsHost := tsURL.Host

	tests := []struct {
		name       string
		url        string
		message    string
		title      string
		isHTML     bool
		expectHost string
		expectPath string
		verifyBody func(t *testing.T, body []byte)
	}{
		{
			name:       "Slack",
			url:        "slack://T00000000/B00000000/XXXXXXXXXXXXXXXXXXXXXXXX",
			message:    "hello slack",
			title:      "Slack Title",
			expectHost: "hooks.slack.com",
			expectPath: "/services/T00000000/B00000000/XXXXXXXXXXXXXXXXXXXXXXXX",
			verifyBody: func(t *testing.T, body []byte) {
				var payload map[string]any
				require.NoError(t, json.Unmarshal(body, &payload))
				require.Equal(t, "Slack Title", payload["text"])
				atts := payload["attachments"].([]any)
				require.NotEmpty(t, atts)
				first := atts[0].(map[string]any)
				require.Equal(t, "hello slack", first["text"])
			},
		},
		{
			name:       "Telegram",
			url:        "telegram://123456789:ABCdefGHIjklMNOpqrSTUvwxYZ@telegram?chats=123456",
			message:    "hello telegram",
			title:      "Telegram Title",
			expectHost: "api.telegram.org",
			expectPath: "/bot123456789:ABCdefGHIjklMNOpqrSTUvwxYZ/sendMessage",
			verifyBody: func(t *testing.T, body []byte) {
				s := string(body)
				require.Contains(t, s, "123456")
				require.Contains(t, s, "Telegram Title")
				require.Contains(t, s, "hello telegram")
			},
		},
		{
			name:       "Gotify",
			url:        fmt.Sprintf("gotify://%s/A12345678901234?disabletls=yes", tsHost),
			message:    "hello gotify",
			title:      "Gotify Title",
			expectHost: tsHost,
			expectPath: "/message",
			verifyBody: func(t *testing.T, body []byte) {
				var payload map[string]any
				require.NoError(t, json.Unmarshal(body, &payload))
				require.Equal(t, "Gotify Title", payload["title"])
				require.Equal(t, "hello gotify", payload["message"])
			},
		},
		{
			name:       "ntfy",
			url:        fmt.Sprintf("ntfy://%s/topic", tsHost),
			message:    "hello ntfy",
			title:      "ntfy Title",
			expectHost: tsHost,
			expectPath: "/topic",
			verifyBody: func(t *testing.T, body []byte) {
				require.Equal(t, "hello ntfy", string(body))
			},
		},
		{
			name:       "Pushbullet",
			url:        "pushbullet://o.XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX/channel",
			message:    "hello pushbullet",
			title:      "Pushbullet Title",
			expectHost: "api.pushbullet.com",
			expectPath: "/v2/pushes",
			verifyBody: func(t *testing.T, body []byte) {
				var payload map[string]any
				require.NoError(t, json.Unmarshal(body, &payload))
				require.Equal(t, "Pushbullet Title", payload["title"])
				require.Equal(t, "hello pushbullet", payload["body"])
			},
		},
		{
			name:       "Pushover",
			url:        "pushover://shoutrrr:azG789012345678901234567890123@uG789012345678901234567890123",
			message:    "hello pushover",
			title:      "Pushover Title",
			expectHost: "api.pushover.net",
			expectPath: "/1/messages.json",
			verifyBody: func(t *testing.T, body []byte) {
				s := string(body)
				require.Contains(t, s, "azG789012345678901234567890123")
				require.Contains(t, s, "uG789012345678901234567890123")
				require.Contains(t, s, "Pushover+Title")
				require.Contains(t, s, "hello+pushover")
			},
		},
		{
			name:       "GenericWebhook",
			url:        fmt.Sprintf("generic://%s/webhook", tsHost),
			message:    "hello generic",
			title:      "Generic Title",
			expectHost: tsHost,
			expectPath: "/webhook",
			verifyBody: func(t *testing.T, body []byte) {
				require.Equal(t, "hello generic", string(body))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hit := make(chan struct{}, 1)
			handler := func(r *http.Request) (*http.Response, error) {
				defer func() { hit <- struct{}{} }()
				// For server handlers, Host is in r.Host, not r.URL.Host
				host := r.URL.Host
				if host == "" {
					host = r.Host
				}
				require.Equal(t, tt.expectHost, host)
				require.Equal(t, tt.expectPath, r.URL.Path)

				if tt.verifyBody != nil {
					b, err := io.ReadAll(r.Body)
					require.NoError(t, err)
					tt.verifyBody(t, b)
				}

				body := "{}"
				if tt.name == "Slack" {
					body = "ok"
				}

				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(body)),
				}, nil
			}

			// For services that use http.DefaultTransport
			http.DefaultTransport = roundTripFunc(handler)

			// For services that we point to our test server (Gotify, ntfy)
			// We need to update the test server's handler
			ts.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				resp, err := handler(r)
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				defer resp.Body.Close()
				for k, v := range resp.Header {
					w.Header()[k] = v
				}
				w.WriteHeader(resp.StatusCode)
				_, _ = io.Copy(w, resp.Body)
			})

			svc := &NotificationService{}
			err := svc.sendShoutrrrNotification(context.Background(), tt.url, tt.message, tt.title, tt.isHTML)
			require.NoError(t, err)

			select {
			case <-hit:
				// ok
			case <-time.After(2 * time.Second):
				t.Fatal("expected HTTP request")
			}
		})
	}
}
