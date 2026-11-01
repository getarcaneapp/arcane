package passkey

import (
	"time"

	"getarcane.dev/arcane/v3/internal/database"
	"getarcane.dev/arcane/v3/internal/user"
)

type (
	// Passkey stores the WebAuthn credential record required to validate future
	// assertions. The raw attestation fields are retained so the credential can
	// be audited or revalidated if the WebAuthn library's verification policy
	// evolves.
	Passkey struct {
		database.BaseModel

		User *user.User `json:"-" gorm:"foreignKey:UserID;constraint:OnDelete:CASCADE"`

		LastUsedAt *time.Time `json:"lastUsedAt,omitempty" gorm:"column:last_used_at"`

		UserID string `json:"userId" gorm:"column:user_id;not null;index"`

		RPID                    string `json:"rpId" gorm:"column:rp_id;not null"`
		AttestationType         string `json:"attestationType,omitempty" gorm:"column:attestation_type"`
		AttestationFormat       string `json:"attestationFormat,omitempty" gorm:"column:attestation_format"`
		AuthenticatorAttachment string `json:"authenticatorAttachment,omitempty" gorm:"column:authenticator_attachment"`

		Name         string               `json:"name" gorm:"column:name;not null"`
		CredentialID []byte               `json:"-" gorm:"column:credential_id;not null"`
		PublicKey    []byte               `json:"-" gorm:"column:public_key;not null"`
		Transports   database.StringSlice `json:"transports,omitempty" gorm:"column:transports;type:text"`
		AAGUID       []byte               `json:"-" gorm:"column:aaguid"`

		AttestationClientDataJSON    []byte `json:"-" gorm:"column:attestation_client_data_json"`
		AttestationClientDataHash    []byte `json:"-" gorm:"column:attestation_client_data_hash"`
		AttestationAuthenticatorData []byte `json:"-" gorm:"column:attestation_authenticator_data"`
		AttestationObject            []byte `json:"-" gorm:"column:attestation_object"`

		AttestationPublicKeyAlgorithm int64  `json:"-" gorm:"column:attestation_public_key_algorithm"`
		SignCount                     uint32 `json:"signCount" gorm:"column:sign_count;not null;default:0"`
		BackupEligible                bool   `json:"backupEligible" gorm:"column:backup_eligible;not null;default:false"`
		BackupState                   bool   `json:"backupState" gorm:"column:backup_state;not null;default:false"`
		CloneWarning                  bool   `json:"cloneWarning" gorm:"column:clone_warning;not null;default:false"`
	}

	// PasskeyCeremony holds the opaque server-side state between WebAuthn begin
	// and finish calls. It is intentionally never serialized to the client.
	PasskeyCeremony struct {
		database.BaseModel

		ExpiresAt         time.Time  `json:"expiresAt" gorm:"column:expires_at;not null;index"`
		UserID            *string    `json:"userId,omitempty" gorm:"column:user_id;index"`
		SessionID         *string    `json:"sessionId,omitempty" gorm:"column:session_id;index"`
		AuthTransactionID *string    `json:"authTransactionId,omitempty" gorm:"column:auth_transaction_id;index"`
		ConsumedAt        *time.Time `json:"-" gorm:"column:consumed_at;index"`

		Purpose     string `json:"purpose" gorm:"column:purpose;not null;index"`
		RPID        string `json:"rpId" gorm:"column:rp_id;not null"`
		SessionData string `json:"-" gorm:"column:session_data;type:text;not null"`
	}

	// AuthTransaction links a primary authentication result to its pending
	// passkey MFA or step-up ceremony. The client receives only ID/secret values;
	// primary credentials and WebAuthn SessionData remain server-side.
	AuthTransaction struct {
		database.BaseModel

		ExpiresAt   time.Time  `json:"expiresAt" gorm:"column:expires_at;not null;index"`
		SessionID   *string    `json:"sessionId,omitempty" gorm:"column:session_id;index"`
		UserAgent   *string    `json:"-" gorm:"column:user_agent"`
		IPAddress   *string    `json:"-" gorm:"column:ip_address"`
		SecretHash  *string    `json:"-" gorm:"column:secret_hash;index"`
		CompletedAt *time.Time `json:"completedAt,omitempty" gorm:"column:completed_at"`

		Kind   string `json:"kind" gorm:"column:kind;not null;index"`
		UserID string `json:"userId" gorm:"column:user_id;not null;index"`
		Source string `json:"source" gorm:"column:source;not null"`
		Status string `json:"status" gorm:"column:status;not null;index"`
	}

	// PasskeyRecoveryCode stores only a one-way digest of a recovery code. The
	// plaintext is returned once during generation and is never recoverable.
	PasskeyRecoveryCode struct {
		database.BaseModel

		UsedAt *time.Time `json:"usedAt,omitempty" gorm:"column:used_at;index"`

		UserID   string `json:"userId" gorm:"column:user_id;not null;index"`
		CodeHash string `json:"-" gorm:"column:code_hash;not null;uniqueIndex"`
	}
)

func (Passkey) TableName() string {
	return "passkeys"
}

func (PasskeyCeremony) TableName() string {
	return "passkey_ceremonies"
}

func (AuthTransaction) TableName() string {
	return "auth_transactions"
}

func (PasskeyRecoveryCode) TableName() string {
	return "passkey_recovery_codes"
}
