package appimages

type CustomImage struct {
	MimeType string `json:"mimeType" gorm:"column:mime_type;not null"`
	Slot     string `json:"slot" gorm:"column:slot;primaryKey"`
	Data     []byte `json:"-" gorm:"column:data;type:blob;not null"`
	Size     int    `json:"size" gorm:"column:size;primaryKey;autoIncrement:false"`
}

func (CustomImage) TableName() string {
	return "custom_images"
}
