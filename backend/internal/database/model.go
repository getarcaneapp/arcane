package database

import (
	"database/sql/driver"
	"encoding/json/v2"
	"fmt"
	"time"
	"uuid"

	"gorm.io/gorm"
)

type (
	BaseModel struct {
		CreatedAt time.Time  `json:"createdAt" gorm:"column:created_at" sortable:"true"`
		UpdatedAt *time.Time `json:"updatedAt,omitempty" gorm:"column:updated_at"`
		ID        string     `json:"id" gorm:"primaryKey;type:text"`
	}

	//nolint:recvcheck
	JSON map[string]any

	//nolint:recvcheck
	StringSlice []string
)

func (m *BaseModel) BeforeCreate(_ *gorm.DB) (err error) {
	if m.ID == "" {
		m.ID = uuid.New().String()
	}
	if m.CreatedAt.IsZero() {
		m.CreatedAt = time.Now()
	}
	return nil
}

func (m *BaseModel) BeforeUpdate(_ *gorm.DB) (err error) {
	m.UpdatedAt = new(time.Now())
	return nil
}

func JSONValue(isNil bool, value any) (driver.Value, error) {
	if isNil {
		return nil, nil
	}
	return json.Marshal(value)
}

func JSONScan[T any](dest *T, value any) error {
	if value == nil {
		var zero T
		*dest = zero
		return nil
	}
	switch v := value.(type) {
	case []byte:
		return json.Unmarshal(v, dest)
	case string:
		return json.Unmarshal([]byte(v), dest)
	default:
		return fmt.Errorf("unsupported scan type for %T: %T", *dest, value)
	}
}

func (j JSON) Value() (driver.Value, error) {
	return JSONValue(j == nil, j)
}

func (j *JSON) Scan(value any) error {
	return JSONScan(j, value)
}

func JSONFromMap[M ~map[string]V, V any](input M) JSON {
	if len(input) == 0 {
		return nil
	}
	out := make(JSON, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func (j JSON) ToStringMap() map[string]string {
	if len(j) == 0 {
		return nil
	}
	out := make(map[string]string, len(j))
	for key, value := range j {
		out[key] = fmt.Sprint(value)
	}
	return out
}

func (s StringSlice) Value() (driver.Value, error) {
	return JSONValue(s == nil, s)
}

func (s *StringSlice) Scan(value any) error {
	return JSONScan(s, value)
}
