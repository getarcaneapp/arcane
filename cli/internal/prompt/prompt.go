package prompt

import (
	"errors"
	"fmt"
	"os"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/term"

	"github.com/getarcaneapp/arcane/cli/v3/internal/output"
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
	if !IsInteractive() {
		return -1, errors.New("interactive terminal required")
	}

	defer output.SuspendProgress()()
	items := make([]list.Item, len(options))
	for i, option := range options {
		items[i] = selectItem{index: i, title: option}
	}

	model := selectModel{list: list.New(items, list.NewDefaultDelegate(), 0, 0), choice: -1}
	model.list.Title = ""
	model.list.SetShowStatusBar(false)
	model.list.SetFilteringEnabled(true)
	model.list.SetShowHelp(true)
	finalModel, err := tea.NewProgram(model).Run()
	if err != nil {
		return -1, err
	}

	result, ok := finalModel.(selectModel)
	if !ok {
		return -1, errors.New("failed to read selection")
	}
	if result.canceled {
		return -1, errors.New("selection canceled")
	}
	if result.choice < 0 {
		return -1, errors.New("selection required")
	}
	if result.choice >= len(options) {
		return -1, errors.New("selection out of range")
	}

	_, _ = fmt.Fprint(output.Stderr(), "\033[H\033[2J")
	if label == "" {
		_, _ = fmt.Fprintf(output.Stderr(), "Selected: %s\n\n", options[result.choice])
	} else {
		_, _ = fmt.Fprintf(output.Stderr(), "Selected %s: %s\n\n", label, options[result.choice])
	}
	return result.choice, nil
}

type selectItem struct {
	index int
	title string
}

func (i selectItem) Title() string       { return i.title }
func (i selectItem) Description() string { return "" }
func (i selectItem) FilterValue() string { return i.title }

type selectModel struct {
	list     list.Model
	choice   int
	canceled bool
}

func (m selectModel) Init() tea.Cmd {
	return nil
}

func (m selectModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.list.SetSize(max(msg.Width, 20), max(msg.Height-4, 6))
		return m, nil
	case tea.KeyPressMsg:
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			m.canceled = true
			return m, tea.Quit
		case "enter":
			selected, ok := m.list.SelectedItem().(selectItem)
			if ok {
				m.choice = selected.index
			}
			return m, tea.Quit
		}
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m selectModel) View() tea.View {
	return tea.NewView("\n" + m.list.View())
}
