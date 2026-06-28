package models

// UserAvatar represents the raw profile picture data for a user.
// Stored separately from the main User struct to prevent
// loading up to 2MB of binary data on every user query.
type UserAvatar struct {
	UserID   string `json:"userId" gorm:"column:user_id;primaryKey"`
	Data     []byte `json:"-" gorm:"column:data;type:blob;not null"`
	MimeType string `json:"mimeType" gorm:"column:mime_type;not null"`
}

func (UserAvatar) TableName() string {
	return "user_avatars"
}
