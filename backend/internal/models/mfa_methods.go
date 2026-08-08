package models

const (
	// PasskeyMFAMethod and RecoveryCodeMFAMethod name the second factor a login
	// completed with, recorded on the session for auditing.
	PasskeyMFAMethod      = "passkey"
	RecoveryCodeMFAMethod = "recovery_code"
)
