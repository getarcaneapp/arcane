package cmdutil

import (
	"bytes"
	"cmp"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/getarcaneapp/arcane/cli/v3/internal/output"
)

const maxErrorBodyBytes = 4096

// HTTPStatusError represents a non-2xx HTTP response.
type HTTPStatusError struct {
	StatusCode int
	Body       string
}

func (e *HTTPStatusError) Error() string {
	body := strings.TrimSpace(e.Body)
	if body == "" {
		return fmt.Sprintf("request failed with status %d", e.StatusCode)
	}
	return fmt.Sprintf("request failed with status %d: %s", e.StatusCode, body)
}

// EnsureSuccessStatus returns an error for non-2xx responses.
func EnsureSuccessStatus(resp *http.Response) error {
	if resp == nil {
		return errors.New("nil HTTP response")
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
	return &HTTPStatusError{
		StatusCode: resp.StatusCode,
		Body:       string(body),
	}
}

// DecodeJSON decodes into out; callers must first enforce HTTP success with EnsureSuccessStatus.
func DecodeJSON(resp *http.Response, out any) error {
	if out == nil {
		return errors.New("nil decode target")
	}
	if err := json.UnmarshalRead(resp.Body, out); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}
	return nil
}

// ReadJSONBody enforces HTTP and API success while retaining every response field.
func ReadJSONBody(resp *http.Response) ([]byte, error) {
	if err := EnsureSuccessStatus(resp); err != nil {
		return nil, err
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}
	var status struct {
		Success *bool  `json:"success"`
		Error   string `json:"error"`
	}
	if json.Unmarshal(body, &status) == nil && status.Success != nil && !*status.Success {
		return nil, fmt.Errorf("API error: %s", cmp.Or(status.Error, "request was not successful"))
	}
	return body, nil
}

// PrintRawJSON pretty-prints a raw JSON document, falling back to the bytes as-is.
func PrintRawJSON(body []byte) error {
	value := jsontext.Value(bytes.Clone(body))
	if value.Indent(jsontext.WithIndent("  ")) == nil {
		_, err := fmt.Fprintln(output.Stdout(), value.String())
		return err
	}
	// Not valid JSON: echo the server's bytes verbatim rather than swallow them.
	_, err := fmt.Fprintln(output.Stdout(), string(body))
	return err
}

// PrintJSON prints indented JSON to stdout.
func PrintJSON(v any) error {
	b, err := json.Marshal(v, jsontext.WithIndent("  "))
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}
	_, err = fmt.Fprintln(output.Stdout(), string(b))
	return err
}
