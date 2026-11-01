// Package output provides styled messages and terminal tables for the CLI.
package output

import (
	"fmt"
	"image/color"
	"regexp"
	"slices"
	"strings"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"
	"github.com/charmbracelet/x/term"
	"github.com/mattn/go-runewidth"
	kit "kit.getarcane.app"
)

const (
	// ansiReset closes a styled run re-applied after truncation.
	ansiReset = "\x1b[0m"
)

var (
	arcanePurple, textPrimary, textMuted, statusOnline, statusOffline, statusWarn                  color.Color
	successStyle, warnStyle, infoStyle, headerStyle, keyStyle, valueStyle                          lipgloss.Style
	statusOnlineStyle, statusOfflineStyle, statusWarnStyle, statusMutedStyle, enabledStyle         lipgloss.Style
	tableHeader, tableCell, tableOddRow, tableEvenRow, tableBorder, tablePlainCell, tablePlainHead lipgloss.Style

	ansiRegexp = regexp.MustCompile("\x1b\\[[0-9;]*[a-zA-Z]")

	tableWhitespaceReplacer = strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ", "\t", " ")

	colorEnabled = true
)

func init() { setTheme(true) }

func setTheme(dark bool) {
	c := lipgloss.LightDark(dark)
	arcanePurple = c(lipgloss.Color("#6d28d9"), lipgloss.Color("#a78bfa"))
	textPrimary = c(lipgloss.Color("#1f2937"), lipgloss.Color("#e5e7eb"))
	textMuted = c(lipgloss.Color("#64748b"), lipgloss.Color("#cbd5e1"))
	statusOnline = c(lipgloss.Color("#15803d"), lipgloss.Color("#4ade80"))
	statusOffline = c(lipgloss.Color("#b91c1c"), lipgloss.Color("#f87171"))
	statusWarn = c(lipgloss.Color("#b45309"), lipgloss.Color("#fbbf24"))
	successStyle = lipgloss.NewStyle().Foreground(statusOnline)
	warnStyle = lipgloss.NewStyle().Foreground(statusWarn)
	infoStyle = lipgloss.NewStyle().Foreground(arcanePurple)
	headerStyle = infoStyle.Bold(true)
	keyStyle = lipgloss.NewStyle().Foreground(textMuted)
	valueStyle = lipgloss.NewStyle().Foreground(textPrimary)
	statusOnlineStyle, statusOfflineStyle, statusWarnStyle = successStyle, lipgloss.NewStyle().Foreground(statusOffline), warnStyle
	statusMutedStyle = keyStyle
	enabledStyle = infoStyle
	tableCell = lipgloss.NewStyle().Padding(0, 1)
	tableHeader = tableCell.Foreground(arcanePurple).Bold(true).Align(lipgloss.Left)
	tableOddRow, tableEvenRow = tableCell.Foreground(textPrimary), tableCell.Foreground(textPrimary)
	tableBorder = lipgloss.NewStyle().Foreground(arcanePurple)
	tablePlainCell, tablePlainHead = tableCell, tableCell
}

// Success prints a success message in green.
// Format specifiers work like fmt.Printf; a leading newline separates messages.
//
//nolint:goprintffuncname // printf-style output helper; *f rename across call sites tracked separately
func Success(format string, a ...any) {
	msg := fmt.Sprintf(format, a...)
	defer SuspendProgress()()
	_, _ = fmt.Fprintf(humanWriter(), "\n  %s\n", renderFor(humanWriter(), successStyle, ("✓ "+msg)))
}

// Warning prints a warning message in yellow.
// Format specifiers work like fmt.Printf; a leading newline separates messages.
//
//nolint:goprintffuncname // printf-style output helper; *f rename across call sites tracked separately
func Warning(format string, a ...any) {
	msg := fmt.Sprintf(format, a...)
	defer SuspendProgress()()
	_, _ = fmt.Fprintf(Stderr(), "\n  %s\n", renderFor(Stderr(), warnStyle, msg))
}

// Info prints an info message in purple.
// Format specifiers work like fmt.Printf; a leading newline separates messages.
//
//nolint:goprintffuncname // printf-style output helper; *f rename across call sites tracked separately
func Info(format string, a ...any) {
	msg := fmt.Sprintf(format, a...)
	defer SuspendProgress()()
	_, _ = fmt.Fprintf(humanWriter(), "\n  %s\n", renderFor(humanWriter(), infoStyle, msg))
}

// Header prints a purple section heading.
// A leading newline separates the heading from previous output.
//
//nolint:goprintffuncname // printf-style output helper; *f rename across call sites tracked separately
func Header(format string, a ...any) {
	msg := fmt.Sprintf(format, a...)
	defer SuspendProgress()()
	_, _ = fmt.Fprintf(humanWriter(), "\n  %s\n", renderFor(humanWriter(), headerStyle, msg))
}

// KeyValue prints a key-value pair with the muted aligned labels and neutral values.
// Use it for structured details and configuration values.
func KeyValue(key string, value any) {
	defer SuspendProgress()()
	_, _ = fmt.Fprintf(humanWriter(), "  %s %s\n", renderFor(humanWriter(), keyStyle, fmt.Sprintf("%-18s", key)), renderFor(humanWriter(), valueStyle, fmt.Sprint(value)))
}

// Showing prints a pagination summary in the form "Showing: shown/total label".
func Showing(shown int, total int64, label string) {
	defer SuspendProgress()()
	_, _ = fmt.Fprintf(humanWriter(), "\n  Showing: %d/%d %s\n", shown, total, label)
}

// TintStatus applies semantic status coloring to a value.
func TintStatus(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || ansiRegexp.MatchString(trimmed) || !colorsEnabled() || !IsTerminal(Stdout()) {
		return value
	}
	lower := strings.ToLower(trimmed)

	switch {
	case lower == "ok" || lower == "succeeded" || lower == "success" || lower == "online" || lower == "running" || lower == "healthy" || lower == "active" || strings.HasPrefix(lower, "up"):
		return statusOnlineStyle.Render(trimmed)
	case lower == "fail" || lower == "error" || lower == "offline" || lower == "stopped" || lower == "exited" ||
		lower == "dead" || lower == "unhealthy" || lower == "failed" || strings.HasPrefix(lower, "down"):
		return statusOfflineStyle.Render(trimmed)
	case lower == "warn" || lower == "warning" || lower == "paused" || lower == "restarting" || lower == "starting" || lower == "created" || lower == "degraded":
		return statusWarnStyle.Render(trimmed)
	default:
		return statusMutedStyle.Render(trimmed)
	}
}

// TintEnabled applies tints for enabled/disabled values.
func TintEnabled(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || ansiRegexp.MatchString(trimmed) || !colorsEnabled() || !IsTerminal(Stdout()) {
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
	if trimmed == "" || ansiRegexp.MatchString(trimmed) || !colorsEnabled() || !IsTerminal(Stdout()) {
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
	if trimmed == "" || ansiRegexp.MatchString(trimmed) || !colorsEnabled() || !IsTerminal(Stdout()) {
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
// Rendering uses Lip Gloss table styles with zebra-striped rows.
func Table(headers []string, rows [][]string) error {
	defer SuspendProgress()()
	if _, spacingErr := fmt.Fprintln(humanWriter()); spacingErr != nil {
		return fmt.Errorf("failed to print table: %w", spacingErr)
	}

	n := len(headers)
	if n == 0 {
		return nil
	}

	displayRows := make([][]string, len(rows))
	for i, row := range rows {
		displayRows[i] = make([]string, n)
		copy(displayRows[i], row)
	}
	rows = displayRows
	if colorsEnabled() && IsTerminal(Stdout()) {
		for _, row := range rows {
			for col, header := range headers {
				switch strings.ToUpper(strings.TrimSpace(header)) {
				case "STATUS", "STATE":
					row[col] = TintStatus(row[col])
				case "ENABLED":
					row[col] = TintEnabled(row[col])
				case "IN USE":
					row[col] = TintYesNo(row[col])
				case "INSECURE":
					row[col] = TintInsecure(row[col])
				}
			}
		}
	}
	headers, rows = fitTableToTerminal(headers, rows)

	t := table.New().Border(lipgloss.NormalBorder()).Headers(headers...)

	if colorsEnabled() && IsTerminal(Stdout()) {
		t = t.BorderStyle(tableBorder).
			StyleFunc(func(row, col int) lipgloss.Style {
				switch {
				case row == table.HeaderRow:
					return tableHeader
				case row%2 == 0:
					return tableEvenRow
				default:
					return tableOddRow
				}
			})
	} else {
		t = t.StyleFunc(func(row, col int) lipgloss.Style {
			return kit.Ternary(row == table.HeaderRow, tablePlainHead, tablePlainCell)
		})
	}

	if len(rows) > 0 {
		t = t.Rows(rows...)
	}

	if _, tableErr := fmt.Fprintln(humanWriter(), t.String()); tableErr != nil {
		return fmt.Errorf("failed to print table: %w", tableErr)
	}
	return nil
}

func fitTableToTerminal(headers []string, rows [][]string) ([]string, [][]string) {
	if len(headers) == 0 {
		return headers, rows
	}

	columnCount := len(headers)
	displayHeaders := make([]string, columnCount)
	for i, header := range headers {
		displayHeaders[i] = strings.TrimSpace(tableWhitespaceReplacer.Replace(header))
	}

	displayRows := rows
	for _, row := range displayRows {
		for col := range columnCount {
			row[col] = strings.TrimSpace(tableWhitespaceReplacer.Replace(row[col]))
		}
	}

	fd, ok := Stdout().(fileDescriptor)
	if !ok || !term.IsTerminal(fd.Fd()) {
		return displayHeaders, displayRows
	}

	terminalWidth, _, err := term.GetSize(fd.Fd())
	if err != nil || terminalWidth <= 0 {
		return displayHeaders, displayRows
	}

	columnWidths := make([]int, columnCount)
	for i, header := range displayHeaders {
		columnWidths[i] = max(1, runewidth.StringWidth(ansiRegexp.ReplaceAllString(header, "")))
	}

	for _, row := range displayRows {
		for col := range columnCount {
			columnWidths[col] = max(columnWidths[col], runewidth.StringWidth(ansiRegexp.ReplaceAllString(row[col], "")))
		}
	}

	availableContentWidth := terminalWidth - (3*columnCount + 1)
	if availableContentWidth <= 0 {
		return displayHeaders, displayRows
	}

	fitWidths := fitColumnWidths(columnWidths, availableContentWidth)
	for i, header := range displayHeaders {
		displayHeaders[i] = truncateVisible(header, fitWidths[i])
	}

	for _, row := range displayRows {
		for col := range columnCount {
			row[col] = truncateVisible(row[col], fitWidths[col])
		}
	}

	return displayHeaders, displayRows
}

func truncateVisible(value string, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}
	if runewidth.StringWidth(ansiRegexp.ReplaceAllString(value, "")) <= maxWidth {
		return value
	}

	plain := ansiRegexp.ReplaceAllString(value, "")
	truncated := "…"
	if maxWidth > 1 {
		truncated = runewidth.Truncate(plain, maxWidth, "…")
	}

	// Preserve the leading color sequence and reset after truncation.
	codes := ansiRegexp.FindAllString(value, -1)
	if len(codes) == 0 || !strings.HasPrefix(value, codes[0]) {
		return truncated
	}
	return codes[0] + truncated + ansiReset
}

func fitColumnWidths(widths []int, available int) []int {
	fitted := slices.Clone(widths)

	if len(fitted) == 0 {
		return fitted
	}

	available = max(available, len(fitted))

	current := 0
	for _, width := range fitted {
		current += width
	}
	for current > available {
		idx, maxWidth := -1, 1
		for i, width := range fitted {
			if width > maxWidth {
				idx, maxWidth = i, width
			}
		}
		if idx < 0 {
			break
		}
		fitted[idx]--
		current--
	}

	return fitted
}
