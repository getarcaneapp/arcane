package models

import "time"

// Device is a paired mobile (or other) client. Each device is bound to a
// single user and a single ApiKey row that carries the actual secret. The
// foreign key ApiKeyID is unique — exactly one ApiKey per Device.
type Device struct {
	UserID      string     `json:"userId" gorm:"column:user_id;not null;index"`
	ApiKeyID    string     `json:"apiKeyId" gorm:"column:api_key_id;not null;uniqueIndex"`
	Name        string     `json:"name" gorm:"column:name;not null"`
	DeviceID    string     `json:"deviceId" gorm:"column:device_id;not null"`
	AppVersion  string     `json:"appVersion" gorm:"column:app_version"`
	OsVersion   string     `json:"osVersion" gorm:"column:os_version"`
	DeviceModel string     `json:"deviceModel" gorm:"column:device_model"`
	LastSeenAt  *time.Time `json:"lastSeenAt,omitempty" gorm:"column:last_seen_at"`
	BaseModel
}

func (Device) TableName() string {
	return "devices"
}

// PairingSession is a short-lived row created by the web UI when a user
// requests a pairing code. It carries the short_code (manual entry) and
// qr_token (embedded in the QR deep link) — both are accepted by RedeemCode.
// On successful redemption, redeemed_at and redeemed_device_id are set and
// the row becomes inert.
type PairingSession struct {
	UserID           string     `json:"userId" gorm:"column:user_id;not null"`
	ShortCode        string     `json:"-" gorm:"column:short_code;uniqueIndex;not null"`
	QrToken          string     `json:"-" gorm:"column:qr_token;uniqueIndex;not null"`
	ExpiresAt        time.Time  `json:"expiresAt" gorm:"column:expires_at;not null;index"`
	RedeemedAt       *time.Time `json:"redeemedAt,omitempty" gorm:"column:redeemed_at"`
	RedeemedDeviceID *string    `json:"redeemedDeviceId,omitempty" gorm:"column:redeemed_device_id"`
	BaseModel
}

func (PairingSession) TableName() string {
	return "pairing_sessions"
}
