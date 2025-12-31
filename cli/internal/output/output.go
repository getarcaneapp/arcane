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
	"regexp"
	"strings"

	"github.com/fatih/color"
	"github.com/mattn/go-runewidth"
)

var (
	successColor = color.New(color.FgGreen).SprintFunc()
	errorColor   = color.New(color.FgRed).SprintFunc()
	warnColor    = color.New(color.FgYellow).SprintFunc()
	infoColor    = color.New(color.FgCyan).SprintFunc()
	headerColor  = color.New(color.FgHiWhite, color.Bold).SprintFunc()
)

var ansiRegexp = regexp.MustCompile("\x1b\\[[0-9;]*[a-zA-Z]")

// Success prints a success message in green.
// The message is prefixed with a newline for visual separation.
// Format specifiers and arguments work like fmt.Printf.
func Success(format string, a ...interface{}) {
	fmt.Printf("\n%s\n", successColor(fmt.Sprintf(format, a...)))
}

// Error prints an error message in red.
// The message is prefixed with a newline for visual separation.
// Format specifiers and arguments work like fmt.Printf.
func Error(format string, a ...interface{}) {
	fmt.Printf("\n%s\n", errorColor(fmt.Sprintf(format, a...)))
}

// Warning prints a warning message in yellow.
// The message is prefixed with a newline for visual separation.
// Format specifiers and arguments work like fmt.Printf.
func Warning(format string, a ...interface{}) {
	fmt.Printf("\n%s\n", warnColor(fmt.Sprintf(format, a...)))
}

// Info prints an info message in cyan.
// The message is prefixed with a newline for visual separation.
// Format specifiers and arguments work like fmt.Printf.
func Info(format string, a ...interface{}) {
	fmt.Printf("\n%s\n", infoColor(fmt.Sprintf(format, a...)))
}

// Header prints a header message in bold white.
// Use this to introduce sections of output. The message is prefixed
// with a newline for visual separation.
func Header(format string, a ...interface{}) {
	fmt.Printf("\n%s\n", headerColor(fmt.Sprintf(format, a...)))
}

// Print prints a standard message without color formatting.
// Use this for regular output that doesn't need status indication.
func Print(format string, a ...interface{}) {
	fmt.Printf(format+"\n", a...)
}

// KeyValue prints a key-value pair with the key in bold and value in blue.
// This is useful for displaying structured information like image details
// or configuration values.
func KeyValue(key string, value interface{}) {
	fmt.Printf("%s: %v\n", color.New(color.Bold).Sprint(key), color.New(color.FgBlue).Sprint(value))
}

func stripAnsi(s string) string {
	if s == "" {
		return s
	}
	return ansiRegexp.ReplaceAllString(s, "")
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

	widths := computeWidths(headers, rows)
	printHeader(headers, widths)
	for _, row := range rows {
		printRow(row, widths, n)
	}
}

func computeWidths(headers []string, rows [][]string) []int {
	n := len(headers)
	widths := make([]int, n)
	for i, h := range headers {
		widths[i] = runewidth.StringWidth(stripAnsi(h))
	}
	for _, row := range rows {
		for i := 0; i < n; i++ {
			var cell string
			if i < len(row) {
				cell = row[i]
			}
			lines := strings.Split(cell, "\n")
			for _, ln := range lines {
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
	headerFmt := color.New(color.Bold, color.FgHiCyan).SprintFunc()
	sep := "  "
	n := len(headers)
	for i, h := range headers {
		visible := stripAnsi(h)
		colored := headerFmt(h)
		padLen := widths[i] - runewidth.StringWidth(visible)
		if padLen < 0 {
			padLen = 0
		}
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
	columnFmt := color.New(color.FgYellow).SprintFunc()

	// Prepare lines per column
	cellLines := make([][]string, n)
	maxLines := 1
	for i := 0; i < n; i++ {
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
		for i := 0; i < n; i++ {
			var val string
			if li < len(cellLines[i]) {
				val = cellLines[i][li]
			}

			var rendered string
			if i == 0 {
				rendered = columnFmt(val)
			} else {
				rendered = fmt.Sprint(val)
			}

			padLen := widths[i] - runewidth.StringWidth(stripAnsi(val))
			if padLen < 0 {
				padLen = 0
			}

			if i < n-1 {
				fmt.Print(rendered + strings.Repeat(" ", padLen) + sep)
			} else {
				fmt.Print(rendered + strings.Repeat(" ", padLen))
			}
		}
		fmt.Println()
	}
}
