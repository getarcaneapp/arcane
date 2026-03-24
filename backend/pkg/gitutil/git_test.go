package git

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	gossh "golang.org/x/crypto/ssh"
)

func TestGetKnownHostsPath(t *testing.T) {
	t.Run("returns SSH_KNOWN_HOSTS env var when set", func(t *testing.T) {
		customPath := "/custom/path/known_hosts"
		t.Setenv("SSH_KNOWN_HOSTS", customPath)

		result := getKnownHostsPath()
		if result != customPath {
			t.Errorf("expected %s, got %s", customPath, result)
		}
	})

	t.Run("returns default path when env var not set", func(t *testing.T) {
		t.Setenv("SSH_KNOWN_HOSTS", "")

		result := getKnownHostsPath()
		homeDir, _ := os.UserHomeDir()
		expected := filepath.Join(homeDir, ".ssh", "known_hosts")

		if result != expected {
			t.Errorf("expected %s, got %s", expected, result)
		}
	})
}

func TestGetSSHHostKeyCallback(t *testing.T) {
	client := NewClient("")

	t.Run("skip mode returns InsecureIgnoreHostKey", func(t *testing.T) {
		callback, err := client.getSSHHostKeyCallback(SSHHostKeyVerificationSkip)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if callback == nil {
			t.Fatal("expected non-nil callback")
		}
		// InsecureIgnoreHostKey always returns nil
		err = callback("example.com:22", &net.TCPAddr{}, nil)
		if err != nil {
			t.Errorf("skip mode should not return error, got: %v", err)
		}
	})

	t.Run("empty mode defaults to accept_new", func(t *testing.T) {
		tmpDir := t.TempDir()
		knownHostsPath := filepath.Join(tmpDir, "known_hosts")
		t.Setenv("SSH_KNOWN_HOSTS", knownHostsPath)

		callback, err := client.getSSHHostKeyCallback("")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if callback == nil {
			t.Fatal("expected non-nil callback")
		}
	})

	t.Run("accept_new mode creates callback", func(t *testing.T) {
		tmpDir := t.TempDir()
		knownHostsPath := filepath.Join(tmpDir, "known_hosts")
		t.Setenv("SSH_KNOWN_HOSTS", knownHostsPath)

		callback, err := client.getSSHHostKeyCallback(SSHHostKeyVerificationAcceptNew)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if callback == nil {
			t.Fatal("expected non-nil callback")
		}
	})
}

func TestAddHostKey(t *testing.T) {
	t.Run("adds host key to file", func(t *testing.T) {
		tmpDir := t.TempDir()
		knownHostsPath := filepath.Join(tmpDir, "known_hosts")

		// Generate a test key
		key := generateTestPublicKey(t)

		err := addHostKey(knownHostsPath, "example.com:22", key)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Verify file was created and contains content
		content, err := os.ReadFile(knownHostsPath)
		if err != nil {
			t.Fatalf("failed to read known_hosts: %v", err)
		}
		if len(content) == 0 {
			t.Error("expected non-empty known_hosts file")
		}
	})

	t.Run("concurrent writes don't corrupt file", func(t *testing.T) {
		tmpDir := t.TempDir()
		knownHostsPath := filepath.Join(tmpDir, "known_hosts")
		t.Setenv("SSH_KNOWN_HOSTS", knownHostsPath)

		key := generateTestPublicKey(t)
		var wg sync.WaitGroup
		errChan := make(chan error, 10)

		// Simulate concurrent writes
		for i := range 10 {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				hostname := "host" + string(rune('0'+idx)) + ".example.com:22"
				if err := addHostKey(knownHostsPath, hostname, key); err != nil {
					errChan <- err
				}
			}(i)
		}

		wg.Wait()
		close(errChan)

		for err := range errChan {
			t.Errorf("concurrent write error: %v", err)
		}

		// Verify file exists and has content
		content, err := os.ReadFile(knownHostsPath)
		if err != nil {
			t.Fatalf("failed to read known_hosts: %v", err)
		}
		if len(content) == 0 {
			t.Error("expected non-empty known_hosts file after concurrent writes")
		}
	})
}

func TestCreateAcceptNewHostKeyCallback(t *testing.T) {
	t.Run("creates known_hosts directory and file", func(t *testing.T) {
		tmpDir := t.TempDir()
		knownHostsPath := filepath.Join(tmpDir, "subdir", "known_hosts")
		t.Setenv("SSH_KNOWN_HOSTS", knownHostsPath)

		client := NewClient("")
		callback, err := client.createAcceptNewHostKeyCallback()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if callback == nil {
			t.Fatal("expected non-nil callback")
		}

		// Verify directory was created
		if _, err := os.Stat(filepath.Dir(knownHostsPath)); os.IsNotExist(err) {
			t.Error("expected known_hosts directory to be created")
		}
	})

	t.Run("callback adds new host keys", func(t *testing.T) {
		tmpDir := t.TempDir()
		knownHostsPath := filepath.Join(tmpDir, "known_hosts")
		t.Setenv("SSH_KNOWN_HOSTS", knownHostsPath)

		client := NewClient("")
		callback, err := client.createAcceptNewHostKeyCallback()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		key := generateTestPublicKey(t)
		addr := &net.TCPAddr{IP: net.ParseIP("192.168.1.1"), Port: 22}

		err = callback("192.168.1.1:22", addr, key)
		if err != nil {
			t.Errorf("callback returned error: %v", err)
		}

		// Verify host was added to file
		content, err := os.ReadFile(knownHostsPath)
		if err != nil {
			t.Fatalf("failed to read known_hosts: %v", err)
		}
		if len(content) == 0 {
			t.Error("expected host key to be added to known_hosts")
		}
	})

	t.Run("callback accepts known host", func(t *testing.T) {
		tmpDir := t.TempDir()
		knownHostsPath := filepath.Join(tmpDir, "known_hosts")
		t.Setenv("SSH_KNOWN_HOSTS", knownHostsPath)

		client := NewClient("")
		callback, err := client.createAcceptNewHostKeyCallback()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		key := generateTestPublicKey(t)
		addr := &net.TCPAddr{IP: net.ParseIP("192.168.1.1"), Port: 22}

		// First call adds the key
		err = callback("192.168.1.1:22", addr, key)
		if err != nil {
			t.Fatalf("first callback returned error: %v", err)
		}

		// Second call should recognize the known host
		err = callback("192.168.1.1:22", addr, key)
		if err != nil {
			t.Errorf("second callback returned error for known host: %v", err)
		}
	})

	t.Run("callback detects host key mismatch", func(t *testing.T) {
		tmpDir := t.TempDir()
		knownHostsPath := filepath.Join(tmpDir, "known_hosts")
		t.Setenv("SSH_KNOWN_HOSTS", knownHostsPath)

		client := NewClient("")
		callback, err := client.createAcceptNewHostKeyCallback()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		key1 := generateTestPublicKey(t)
		key2 := generateTestPublicKeyVariant(t)
		addr := &net.TCPAddr{IP: net.ParseIP("192.168.1.1"), Port: 22}

		// First call adds key1
		err = callback("192.168.1.1:22", addr, key1)
		if err != nil {
			t.Fatalf("first callback returned error: %v", err)
		}

		// Second call with different key for same host should fail
		err = callback("192.168.1.1:22", addr, key2)
		if err == nil {
			t.Error("expected error for host key mismatch, got nil")
		} else if !strings.Contains(err.Error(), "host key mismatch") {
			t.Errorf("expected host key mismatch error, got: %v", err)
		}
	})
}

func TestValidatePath(t *testing.T) {
	t.Run("allows valid paths", func(t *testing.T) {
		tmpDir := t.TempDir()
		err := ValidatePath(tmpDir, "subdir/file.txt")
		if err != nil {
			t.Errorf("expected valid path to be allowed: %v", err)
		}
	})

	t.Run("rejects path traversal", func(t *testing.T) {
		tmpDir := t.TempDir()
		err := ValidatePath(tmpDir, "../../../etc/passwd")
		if err == nil {
			t.Error("expected path traversal to be rejected")
		}
	})

	t.Run("rejects absolute path escape", func(t *testing.T) {
		tmpDir := t.TempDir()
		err := ValidatePath(tmpDir, "foo/../../..")
		if err == nil {
			t.Error("expected path escape to be rejected")
		}
	})
}

func TestNewClient(t *testing.T) {
	t.Run("creates client with work dir", func(t *testing.T) {
		client := NewClient("/tmp/test")
		if client.workDir != "/tmp/test" {
			t.Errorf("expected workDir /tmp/test, got %s", client.workDir)
		}
	})

	t.Run("creates client with empty work dir", func(t *testing.T) {
		client := NewClient("")
		if client.workDir != "" {
			t.Errorf("expected empty workDir, got %s", client.workDir)
		}
	})
}

// generateTestPublicKey creates a test ED25519 public key for testing
func generateTestPublicKey(t *testing.T) gossh.PublicKey {
	t.Helper()

	// Use a fixed ED25519 public key for deterministic tests
	// This is a valid ED25519 public key format
	pubKeyBytes := []byte{
		0x00, 0x00, 0x00, 0x0b, // key type length (11)
		's', 's', 'h', '-', 'e', 'd', '2', '5', '5', '1', '9', // "ssh-ed25519"
		0x00, 0x00, 0x00, 0x20, // key length (32)
		// 32 bytes of key data
		0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
		0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10,
		0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18,
		0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f, 0x20,
	}

	key, err := gossh.ParsePublicKey(pubKeyBytes)
	if err != nil {
		t.Fatalf("failed to parse test public key: %v", err)
	}
	return key
}

// generateTestPublicKeyVariant creates a different test ED25519 public key
func generateTestPublicKeyVariant(t *testing.T) gossh.PublicKey {
	t.Helper()

	pubKeyBytes := []byte{
		0x00, 0x00, 0x00, 0x0b, // key type length (11)
		's', 's', 'h', '-', 'e', 'd', '2', '5', '5', '1', '9', // "ssh-ed25519"
		0x00, 0x00, 0x00, 0x20, // key length (32)
		// 32 bytes of different key data
		0xFF, 0xFE, 0xFD, 0xFC, 0xFB, 0xFA, 0xF9, 0xF8,
		0xF7, 0xF6, 0xF5, 0xF4, 0xF3, 0xF2, 0xF1, 0xF0,
		0xEF, 0xEE, 0xED, 0xEC, 0xEB, 0xEA, 0xE9, 0xE8,
		0xE7, 0xE6, 0xE5, 0xE4, 0xE3, 0xE2, 0xE1, 0xE0,
	}

	key, err := gossh.ParsePublicKey(pubKeyBytes)
	if err != nil {
		t.Fatalf("failed to parse test public key variant: %v", err)
	}
	return key
}
