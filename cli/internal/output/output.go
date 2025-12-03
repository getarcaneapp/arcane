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

	"github.com/fatih/color"
	"github.com/olekukonko/tablewriter"
)

var (
	successColor = color.New(color.FgGreen).SprintFunc()
	errorColor   = color.New(color.FgRed).SprintFunc()
	warnColor    = color.New(color.FgYellow).SprintFunc()
	infoColor    = color.New(color.FgCyan).SprintFunc()
	headerColor  = color.New(color.FgHiWhite, color.Bold).SprintFunc()
)

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

// Table prints a formatted table with headers and rows.
// Headers are displayed in bold cyan. The table is rendered without borders
// for a clean terminal appearance. Columns are automatically aligned.
func Table(headers []string, rows [][]string) {
	fmt.Println()
	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader(headers)
	table.SetAutoWrapText(false)
	table.SetAutoFormatHeaders(true)
	table.SetHeaderAlignment(tablewriter.ALIGN_LEFT)
	table.SetAlignment(tablewriter.ALIGN_LEFT)
	table.SetCenterSeparator("")
	table.SetColumnSeparator("")
	table.SetRowSeparator("")
	table.SetHeaderLine(false)
	table.SetBorder(false)
	table.SetTablePadding("  ") // pad with spaces
	table.SetNoWhiteSpace(false)

	// Set header colors
	headerColors := make([]tablewriter.Colors, len(headers))
	for i := range headerColors {
		headerColors[i] = tablewriter.Colors{tablewriter.Bold, tablewriter.FgHiCyanColor}
	}
	table.SetHeaderColor(headerColors...)

	table.AppendBulk(rows)
	table.Render()
}
