package s3

import (
	"strings"

	"github.com/getarcaneapp/arcane/types/v3"

	"github.com/getarcaneapp/arcane/backend/v3/internal/database"
)

type S3Destination struct {
	database.BaseModel

	Name            string `json:"name" gorm:"column:name;type:text;not null;uniqueIndex" sortable:"true"`
	Endpoint        string `json:"endpoint,omitempty" gorm:"column:endpoint;type:text" sortable:"true"`
	Bucket          string `json:"bucket" gorm:"column:bucket;type:text;not null" sortable:"true"`
	Region          string `json:"region" gorm:"column:region;type:text;not null" sortable:"true"`
	AccessKeyID     string `json:"accessKeyId" gorm:"column:access_key_id;type:text;not null" sortable:"true"`
	SecretAccessKey string `json:"-" gorm:"column:secret_access_key;type:text;not null"`
	Prefix          string `json:"prefix,omitempty" gorm:"column:prefix;type:text" sortable:"true"`
	UseSSL          bool   `json:"useSsl" gorm:"column:use_ssl;not null" sortable:"true"`
	ForcePathStyle  bool   `json:"forcePathStyle" gorm:"column:force_path_style;not null" sortable:"true"`
}

func (S3Destination) TableName() string {
	return "s3_destinations"
}

func (d S3Destination) ToDTO() types.S3Destination {
	return types.S3Destination{
		ID:               d.ID,
		Name:             d.Name,
		Endpoint:         d.Endpoint,
		Bucket:           d.Bucket,
		Region:           d.Region,
		AccessKeyID:      d.AccessKeyID,
		Prefix:           d.Prefix,
		UseSSL:           d.UseSSL,
		ForcePathStyle:   d.ForcePathStyle,
		SecretConfigured: strings.TrimSpace(d.SecretAccessKey) != "",
		CreatedAt:        d.CreatedAt,
		UpdatedAt:        d.UpdatedAt,
	}
}

func (d S3Destination) ToSync(secretAccessKey string) types.S3Destination {
	return types.S3Destination{
		ID:              d.ID,
		Name:            d.Name,
		Endpoint:        d.Endpoint,
		Bucket:          d.Bucket,
		Region:          d.Region,
		AccessKeyID:     d.AccessKeyID,
		SecretAccessKey: secretAccessKey,
		Prefix:          d.Prefix,
		UseSSL:          d.UseSSL,
		ForcePathStyle:  d.ForcePathStyle,
		CreatedAt:       d.CreatedAt,
		UpdatedAt:       d.UpdatedAt,
	}
}
