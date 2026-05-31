package models

import "time"

type FederatedTokenReplay struct {
	BaseModel

	TokenHash string    `json:"-" gorm:"column:token_hash;not null;uniqueIndex"`
	IssuerURL string    `json:"issuerUrl" gorm:"column:issuer_url;not null;index"`
	ExpiresAt time.Time `json:"expiresAt" gorm:"column:expires_at;not null;index"`
}

func (FederatedTokenReplay) TableName() string {
	return "federated_token_replays"
}
