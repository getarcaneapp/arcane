package prompt

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/term"
)

// IsInteractive reports whether stdin is a terminal.
func IsInteractive() bool {
	return term.IsTerminal(os.Stdin.Fd())
}

// Select prompts the user to select from a list of options and returns the zero-based index.
func Select(label string, options []string) (int, error) {
	if len(options) == 0 {
		return -1, fmt.Errorf("no %s options available", label)
	}

	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Printf("\nMultiple %s match. Select one:\n", label)
		for i, option := range options {
			fmt.Printf("  %d) %s\n", i+1, option)
		}
		fmt.Printf("Enter number (1-%d): ", len(options))

		input, err := reader.ReadString('\n')
		if err != nil {
			return -1, fmt.Errorf("failed to read selection: %w", err)
		}

		input = strings.TrimSpace(input)
		if input == "" {
			return -1, fmt.Errorf("selection required")
		}

		if strings.EqualFold(input, "q") || strings.EqualFold(input, "quit") {
			return -1, fmt.Errorf("selection canceled")
		}

		index, err := strconv.Atoi(input)
		if err != nil || index < 1 || index > len(options) {
			fmt.Println("Invalid selection. Try again or press q to cancel.")
			continue
		}

		return index - 1, nil
	}
}
