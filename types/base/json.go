package base

import (
	"database/sql/driver"
	"encoding/json"
)

// JsonObject is a map-based representation of a JSON object that can be stored
// in a database. It implements the sql.Valuer and sql.Scanner interfaces for
// seamless database integration.
//
// nolint:recvcheck
type JsonObject map[string]interface{}

// Value implements the driver.Valuer interface for database storage.
// It marshals the JsonObject to JSON bytes for database insertion.
// If the JsonObject is nil, it returns nil.
func (j JsonObject) Value() (driver.Value, error) {
	if j == nil {
		return nil, nil
	}
	return json.Marshal(j)
}

// Scan implements the sql.Scanner interface for database retrieval.
// It unmarshals JSON data from the database into the JsonObject.
// Supports scanning from []byte or string values.
// If the value is nil, the JsonObject is set to nil.
func (j *JsonObject) Scan(value interface{}) error {
	if value == nil {
		*j = nil
		return nil
	}
	switch v := value.(type) {
	case []byte:
		return json.Unmarshal(v, j)
	case string:
		return json.Unmarshal([]byte(v), j)
	default:
		return json.Unmarshal(nil, j)
	}
}
