package common

import (
	"errors"
	"io"
	"testing"
)

func TestRedeployAfterSyncFailedErrorZeroValue(t *testing.T) {
	err := &RedeployAfterSyncFailedError{}

	if got := err.Error(); got != "redeploy failed" {
		t.Fatalf("Error() = %q, want %q", got, "redeploy failed")
	}

	if errors.Is(err, io.EOF) {
		t.Fatal("errors.Is matched an unrelated error")
	}
}

func TestRedeployAfterSyncFailedErrorNilReceiver(t *testing.T) {
	var err *RedeployAfterSyncFailedError

	if got := err.Error(); got != "redeploy failed" {
		t.Fatalf("Error() = %q, want %q", got, "redeploy failed")
	}

	if got := err.Unwrap(); got != nil {
		t.Fatalf("Unwrap() = %v, want nil", got)
	}
}

func TestRedeployAfterSyncFailedErrorWrapsErr(t *testing.T) {
	cause := io.EOF
	err := &RedeployAfterSyncFailedError{Err: cause}

	if got := err.Error(); got != "redeploy failed: EOF" {
		t.Fatalf("Error() = %q, want %q", got, "redeploy failed: EOF")
	}

	if !errors.Is(err, cause) {
		t.Fatal("errors.Is did not match wrapped error")
	}
}
