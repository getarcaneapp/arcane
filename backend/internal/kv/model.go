package kv

import "time"

// KVEntry stores lightweight application state as arbitrary key/value pairs.
type KVEntry struct {
	CreatedAt time.Time  `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt *time.Time `gorm:"column:updated_at;autoUpdateTime"`
	Key       string     `gorm:"column:key;primaryKey"`
	Value     string     `gorm:"column:value"`
}

func (KVEntry) TableName() string {
	return "kv"
}
