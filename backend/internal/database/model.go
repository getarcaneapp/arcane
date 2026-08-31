package database

import (
	"database/sql/driver"
	"encoding/json/v2"
	"time"
	"uuid"

	"emperror.dev/errors"
	"gorm.io/gorm"
)

type BaseModel struct {
	ID        string     `json:"id" gorm:"primaryKey;type:text"`
	CreatedAt time.Time  `json:"createdAt" gorm:"column:created_at" sortable:"true"`
	UpdatedAt *time.Time `json:"updatedAt,omitempty" gorm:"column:updated_at"`
}

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

// JSONValue marshals a JSON TEXT column value. isNil stores SQL NULL instead
// of the JSON literal "null" for nil maps/slices.
func JSONValue(isNil bool, value any) (driver.Value, error) {
	if isNil {
		return nil, nil
	}
	return json.Marshal(value)
}

// JSONScan unmarshals a JSON TEXT column into dest, treating NULL as the zero value.
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
		return errors.Errorf("unsupported scan type for %T: %T", *dest, value)
	}
}

//nolint:recvcheck
type JSON map[string]any

func (j JSON) Value() (driver.Value, error) {
	return JSONValue(j == nil, j)
}

func (j *JSON) Scan(value any) error {
	return JSONScan(j, value)
}

//nolint:recvcheck
type StringSlice []string

func (s StringSlice) Value() (driver.Value, error) {
	return JSONValue(s == nil, s)
}

func (s *StringSlice) Scan(value any) error {
	return JSONScan(s, value)
}
