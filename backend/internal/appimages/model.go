package appimages

import "getarcane.dev/arcane/v3/internal/database"

type CustomImage struct {
	database.BaseModel

	MimeType string `json:"mimeType" gorm:"column:mime_type;not null"`
	Slot     string `json:"slot" gorm:"column:slot;not null;uniqueIndex:idx_custom_images_slot_size"`
	Data     []byte `json:"-" gorm:"column:data;type:blob;not null"`
	Size     int    `json:"size" gorm:"column:size;not null;uniqueIndex:idx_custom_images_slot_size"`
}

func (CustomImage) TableName() string {
	return "custom_images"
}
