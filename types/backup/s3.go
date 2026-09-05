package backup

import "time"

type S3Destination struct {
	ID               string     `json:"id"`
	Name             string     `json:"name"`
	Endpoint         string     `json:"endpoint,omitempty"`
	Bucket           string     `json:"bucket"`
	Region           string     `json:"region"`
	AccessKeyID      string     `json:"accessKeyId"`
	Prefix           string     `json:"prefix,omitempty"`
	UseSSL           bool       `json:"useSsl"`
	ForcePathStyle   bool       `json:"forcePathStyle"`
	SecretConfigured bool       `json:"secretConfigured"`
	CreatedAt        time.Time  `json:"createdAt"`
	UpdatedAt        *time.Time `json:"updatedAt,omitempty"`
}

// CreateS3Destination trims and NFC-normalizes tagged display fields.
type CreateS3Destination struct {
	Name            string `json:"name" unorm:"nfc" trim:"true" minLength:"1"`
	Endpoint        string `json:"endpoint,omitempty"`
	Bucket          string `json:"bucket"`
	Region          string `json:"region"`
	AccessKeyID     string `json:"accessKeyId"`
	SecretAccessKey string `json:"secretAccessKey"`
	Prefix          string `json:"prefix,omitempty"`
	UseSSL          bool   `json:"useSsl"`
	ForcePathStyle  bool   `json:"forcePathStyle"`
}

// UpdateS3Destination carries the same connection fields as create; an empty
// SecretAccessKey keeps the stored secret.
type UpdateS3Destination = CreateS3Destination

type S3DestinationSync struct {
	CreateS3Destination

	ID        string     `json:"id"`
	CreatedAt time.Time  `json:"createdAt"`
	UpdatedAt *time.Time `json:"updatedAt,omitempty"`
}

type S3DestinationSyncRequest struct {
	Destinations []S3DestinationSync `json:"destinations"`
}
