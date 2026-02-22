// Package output provides formatted terminal output utilities for the CLI.
//
// This package offers consistent styling for success messages, errors, warnings,
// informational text, headers, key-value pairs, and tables. All output includes
// appropriate color coding for better readability in terminal environments.
//
// # Example Usage
//
//	output.Success("Operation completed")
//	output.Error("Something went wrong: %v", err)
//	output.KeyValue("Status", "Running")
//	output.Table([]string{"ID", "Name"}, rows)
package output

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/term"
	"github.com/mattn/go-runewidth"
)

var (
	arcanePurple = lipgloss.AdaptiveColor{
		Light: "#6d28d9",
		Dark:  "#a78bfa",
	}
	textPrimary = lipgloss.AdaptiveColor{
		Light: "#1f2937",
		Dark:  "#e5e7eb",
	}
	textMuted = lipgloss.AdaptiveColor{
		Light: "#64748b",
		Dark:  "#cbd5e1",
	}
	statusOnline = lipgloss.AdaptiveColor{
		Light: "#15803d",
		Dark:  "#4ade80",
	}
	statusOffline = lipgloss.AdaptiveColor{
		Light: "#b91c1c",
		Dark:  "#f87171",
	}
	statusWarn = lipgloss.AdaptiveColor{
		Light: "#b45309",
		Dark:  "#fbbf24",
	}
)

var (
	successStyle = lipgloss.NewStyle().Foreground(statusOnline)
	errorStyle   = lipgloss.NewStyle().Foreground(statusOffline)
	warnStyle    = lipgloss.NewStyle().Foreground(statusWarn)
	infoStyle    = lipgloss.NewStyle().Foreground(arcanePurple)
	headerStyle  = lipgloss.NewStyle().Bold(true).Foreground(arcanePurple)
	keyStyle     = lipgloss.NewStyle().Bold(true).Foreground(textPrimary)
	valueStyle   = lipgloss.NewStyle().Foreground(arcanePurple)

	// Keep purple as accent in headers, but avoid purple body cells for readability.
	columnStyle  = lipgloss.NewStyle().Foreground(textPrimary)
	columnHeader = lipgloss.NewStyle().Bold(true).Foreground(arcanePurple)

	statusOnlineStyle  = lipgloss.NewStyle().Foreground(statusOnline)
	statusOfflineStyle = lipgloss.NewStyle().Foreground(statusOffline)
	statusWarnStyle    = lipgloss.NewStyle().Foreground(statusWarn)
	statusMutedStyle   = lipgloss.NewStyle().Foreground(textMuted)
	enabledStyle       = lipgloss.NewStyle().Foreground(arcanePurple)
)

var ansiRegexp = regexp.MustCompile("\x1b\\[[0-9;]*[a-zA-Z]")

var colorEnabled = true

func shouldColor() bool {
	return colorEnabled && term.IsTerminal(os.Stdout.Fd())
}

func render(style lipgloss.Style, value string) string {
	if !shouldColor() {
		return value
	}
	return style.Render(value)
}

// SetColorEnabled controls whether CLI output should render ANSI colors.
func SetColorEnabled(enabled bool) {
	colorEnabled = enabled
}

// Success prints a success message in green.
// The message is prefixed with a newline for visual separation.
// Format specifiers and arguments work like fmt.Printf.
func Success(format string, a ...any) {
	msg := fmt.Sprintf(format, a...)
	fmt.Printf("\n%s\n", render(successStyle, msg))
}

// Error prints an error message in red.
// The message is prefixed with a newline for visual separation.
// Format specifiers and arguments work like fmt.Printf.
func Error(format string, a ...any) {
	msg := fmt.Sprintf(format, a...)
	fmt.Printf("\n%s\n", render(errorStyle, msg))
}

// Warning prints a warning message in yellow.
// The message is prefixed with a newline for visual separation.
// Format specifiers and arguments work like fmt.Printf.
func Warning(format string, a ...any) {
	msg := fmt.Sprintf(format, a...)
	fmt.Printf("\n%s\n", render(warnStyle, msg))
}

// Info prints an info message in cyan.
// The message is prefixed with a newline for visual separation.
// Format specifiers and arguments work like fmt.Printf.
func Info(format string, a ...any) {
	msg := fmt.Sprintf(format, a...)
	fmt.Printf("\n%s\n", render(infoStyle, msg))
}

// Header prints a header message in bold white.
// Use this to introduce sections of output. The message is prefixed
// with a newline for visual separation.
func Header(format string, a ...any) {
	msg := fmt.Sprintf(format, a...)
	fmt.Printf("\n%s\n", render(headerStyle, msg))
}

// Print prints a standard message without color formatting.
// Use this for regular output that doesn't need status indication.
func Print(format string, a ...any) {
	fmt.Printf(format+"\n", a...)
}

// KeyValue prints a key-value pair with the key in bold and value in blue.
// This is useful for displaying structured information like image details
// or configuration values.
func KeyValue(key string, value any) {
	keyText := key
	valueText := fmt.Sprint(value)
	fmt.Printf("%s: %v\n", render(keyStyle, keyText), render(valueStyle, valueText))
}

func stripAnsi(s string) string {
	if s == "" {
		return s
	}
	return ansiRegexp.ReplaceAllString(s, "")
}

func hasAnsi(s string) bool {
	if s == "" {
		return false
	}
	return ansiRegexp.MatchString(s)
}

// TintStatus applies semantic status coloring to a value.
func TintStatus(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || hasAnsi(trimmed) || !shouldColor() {
		return value
	}
	lower := strings.ToLower(trimmed)

	switch {
	case lower == "online" || lower == "running" || lower == "healthy" || lower == "active" || strings.HasPrefix(lower, "up"):
		return statusOnlineStyle.Render(trimmed)
	case lower == "offline" || lower == "stopped" || lower == "exited" || lower == "dead" || lower == "unhealthy" || lower == "failed" || strings.HasPrefix(lower, "down"):
		return statusOfflineStyle.Render(trimmed)
	case lower == "paused" || lower == "restarting" || lower == "starting" || lower == "created" || lower == "degraded":
		return statusWarnStyle.Render(trimmed)
	default:
		return statusMutedStyle.Render(trimmed)
	}
}

// TintEnabled applies tints for enabled/disabled values.
func TintEnabled(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || hasAnsi(trimmed) || !shouldColor() {
		return value
	}
	lower := strings.ToLower(trimmed)
	switch lower {
	case "true", "yes", "enabled", "on":
		return enabledStyle.Render(trimmed)
	case "false", "no", "disabled", "off":
		return statusMutedStyle.Render(trimmed)
	default:
		return value
	}
}

// TintYesNo applies tints for yes/no style values.
func TintYesNo(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || hasAnsi(trimmed) || !shouldColor() {
		return value
	}
	lower := strings.ToLower(trimmed)
	switch lower {
	case "true", "yes", "y", "in use":
		return statusOnlineStyle.Render(trimmed)
	case "false", "no", "n":
		return statusMutedStyle.Render(trimmed)
	default:
		return value
	}
}

// TintInsecure applies warning tints for insecure values.
func TintInsecure(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || hasAnsi(trimmed) || !shouldColor() {
		return value
	}
	lower := strings.ToLower(trimmed)
	switch lower {
	case "true", "yes", "y", "insecure":
		return statusWarnStyle.Render(trimmed)
	case "false", "no", "n":
		return statusMutedStyle.Render(trimmed)
	default:
		return value
	}
}

// Table prints a formatted table with headers and rows.
// Headers are displayed in bold cyan. The table is rendered with borders
// for a clean terminal appearance. Columns are automatically aligned.
func Table(headers []string, rows [][]string) {
	fmt.Println()

	n := len(headers)
	if n == 0 {
		return
	}

	rows = tintTableRows(headers, rows)

	widths := computeWidths(headers, rows)
	printHeader(headers, widths)
	for _, row := range rows {
		printRow(row, widths, n)
	}
}

func tintTableRows(headers []string, rows [][]string) [][]string {
	if len(rows) == 0 {
		return rows
	}
	if !shouldColor() {
		return rows
	}

	result := make([][]string, len(rows))
	for i, row := range rows {
		if len(row) == 0 {
			result[i] = row
			continue
		}
		styled := make([]string, len(row))
		copy(styled, row)
		for col := 0; col < len(row) && col < len(headers); col++ {
			header := strings.ToUpper(strings.TrimSpace(headers[col]))
			switch header {
			case "STATUS", "STATE":
				styled[col] = TintStatus(row[col])
			case "ENABLED":
				styled[col] = TintEnabled(row[col])
			case "IN USE":
				styled[col] = TintYesNo(row[col])
			case "INSECURE":
				styled[col] = TintInsecure(row[col])
			}
		}
		result[i] = styled
	}
	return result
}

func computeWidths(headers []string, rows [][]string) []int {
	n := len(headers)
	widths := make([]int, n)
	for i, h := range headers {
		widths[i] = runewidth.StringWidth(stripAnsi(h))
	}
	for _, row := range rows {
		for i := range n {
			var cell string
			if i < len(row) {
				cell = row[i]
			}
			lines := strings.SplitSeq(cell, "\n")
			for ln := range lines {
				w := runewidth.StringWidth(stripAnsi(ln))
				if w > widths[i] {
					widths[i] = w
				}
			}
		}
	}
	return widths
}

func printHeader(headers []string, widths []int) {
	sep := "  "
	n := len(headers)
	for i, h := range headers {
		visible := stripAnsi(h)
		colored := render(columnHeader, h)
		padLen := max(widths[i]-runewidth.StringWidth(visible), 0)
		if i < n-1 {
			fmt.Print(colored + strings.Repeat(" ", padLen) + sep)
		} else {
			fmt.Print(colored + strings.Repeat(" ", padLen))
		}
	}
	fmt.Println()
}

func printRow(row []string, widths []int, n int) {
	sep := "  "

	// Prepare lines per column
	cellLines := make([][]string, n)
	maxLines := 1
	for i := range n {
		var cell string
		if i < len(row) {
			cell = row[i]
		}
		lines := strings.Split(cell, "\n")
		cellLines[i] = lines
		if len(lines) > maxLines {
			maxLines = len(lines)
		}
	}

	for li := 0; li < maxLines; li++ {
		for i := range n {
			var val string
			if li < len(cellLines[i]) {
				val = cellLines[i][li]
			}

			var rendered string
			if i == 0 {
				rendered = render(columnStyle, val)
			} else {
				rendered = fmt.Sprint(val)
			}

			padLen := max(widths[i]-runewidth.StringWidth(stripAnsi(val)), 0)

			if i < n-1 {
				fmt.Print(rendered + strings.Repeat(" ", padLen) + sep)
			} else {
				fmt.Print(rendered + strings.Repeat(" ", padLen))
			}
		}
		fmt.Println()
	}
}
