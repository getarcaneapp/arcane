package event

import (
	"encoding/csv"
	"encoding/json/v2"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/getarcaneapp/arcane/types/v3"
	"go.getarcane.app/kit/pkg"
	"go.getarcane.app/kit/pkg/mapping"
)

var eventCSVHeader = []string{
	"id",
	"timestamp",
	"type",
	"severity",
	"audit",
	"title",
	"description",
	"resourceType",
	"resourceId",
	"resourceName",
	"userId",
	"username",
	"environmentId",
	"metadata",
}

func writeEventExportRow(w io.Writer, writer *csv.Writer, format string, row Event) error {
	if format == types.EventExportFormatNDJSON {
		dto, err := mapping.MapOne[Event, types.Event](row)
		if err != nil {
			return fmt.Errorf("failed to map event: %w", err)
		}
		if err = json.MarshalWrite(w, dto); err != nil {
			return fmt.Errorf("failed to encode event: %w", err)
		}
		_, err = io.WriteString(w, "\n")
		return err
	}
	record, err := eventCSVRecord(row)
	if err != nil {
		return fmt.Errorf("failed to encode event: %w", err)
	}
	return writer.Write(record)
}

// eventCSVRecord flattens an event and neutralizes spreadsheet formula prefixes.
func eventCSVRecord(row Event) ([]string, error) {
	var metadata string
	if row.Metadata != nil {
		raw, err := json.Marshal(map[string]any(row.Metadata))
		if err != nil {
			return nil, err
		}
		metadata = string(raw)
	}
	record := []string{
		row.ID,
		row.Timestamp.UTC().Format(time.RFC3339Nano),
		string(row.Type),
		string(row.Severity),
		strconv.FormatBool(row.Audit),
		row.Title,
		row.Description,
		kit.FromPtr(row.ResourceType),
		kit.FromPtr(row.ResourceID),
		kit.FromPtr(row.ResourceName),
		kit.FromPtr(row.UserID),
		kit.FromPtr(row.Username),
		kit.FromPtr(row.EnvironmentID),
		metadata,
	}
	for i, value := range record {
		record[i] = kit.Ternary(value != "" && strings.ContainsRune("=+-@\t\r", rune(value[0])), "'"+value, value)
	}
	return record, nil
}
