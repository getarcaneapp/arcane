package cmdutil

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"emperror.dev/errors"
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

// DecodeJSON decodes JSON into out and enforces a successful HTTP status.
func DecodeJSON[T any](resp *http.Response, out *T) error {
	if err := EnsureSuccessStatus(resp); err != nil {
		return err
	}
	if out == nil {
		return errors.New("nil decode target")
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return errors.WrapIf(err, "failed to parse response")
	}
	return nil
}

// ReadJSONBody enforces a successful HTTP status and returns the raw response body.
// Use it when a command needs both to decode a response and to echo it verbatim,
// so that fields the CLI does not model — such as the counts and groups objects
// on the container, volume, network and gitops-sync list endpoints — survive
// --json output instead of being dropped by a re-marshal.
func ReadJSONBody(resp *http.Response) ([]byte, error) {
	if err := EnsureSuccessStatus(resp); err != nil {
		return nil, err
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.WrapIf(err, "failed to read response")
	}
	return body, nil
}

// PrintRawJSON pretty-prints a raw JSON document, falling back to the bytes as-is.
func PrintRawJSON(body []byte) error {
	var buf bytes.Buffer
	if json.Indent(&buf, body, "", "  ") == nil {
		fmt.Println(buf.String())
		return nil
	}
	// Not valid JSON: echo the server's bytes verbatim rather than swallow them.
	fmt.Println(string(body))
	return nil
}

// PrintJSON prints indented JSON to stdout.
func PrintJSON(v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return errors.WrapIf(err, "failed to marshal JSON")
	}
	fmt.Println(string(b))
	return nil
}
