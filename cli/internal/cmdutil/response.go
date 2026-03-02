package cmdutil

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
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
		return fmt.Errorf("nil HTTP response")
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

// DecodeJSON decodes JSON into out and enforces a successful HTTP status.
func DecodeJSON[T any](resp *http.Response, out *T) error {
	if err := EnsureSuccessStatus(resp); err != nil {
		return err
	}
	if out == nil {
		return fmt.Errorf("nil decode target")
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}
	return nil
}

// PrintJSON prints indented JSON to stdout.
func PrintJSON(v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}
	fmt.Println(string(b))
	return nil
}
