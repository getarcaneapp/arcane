package prompt

import (
	"fmt"
	"os"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
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
	if !IsInteractive() {
		return -1, fmt.Errorf("interactive terminal required")
	}

	items := make([]list.Item, len(options))
	for i, option := range options {
		items[i] = selectItem{index: i, title: option}
	}

	model := newSelectModel(label, items)
	program := tea.NewProgram(model)
	finalModel, err := program.Run()
	if err != nil {
		return -1, err
	}

	result, ok := finalModel.(selectModel)
	if !ok {
		return -1, fmt.Errorf("failed to read selection")
	}
	if result.canceled {
		return -1, fmt.Errorf("selection canceled")
	}
	if result.choice < 0 {
		return -1, fmt.Errorf("selection required")
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

func newSelectModel(label string, items []list.Item) selectModel {
	delegate := list.NewDefaultDelegate()
	model := selectModel{list: list.New(items, delegate, 0, 0), choice: -1}
	model.list.Title = fmt.Sprintf("Select %s", label)
	model.list.SetShowStatusBar(false)
	model.list.SetFilteringEnabled(true)
	model.list.SetShowHelp(true)
	return model
}

func (m selectModel) Init() tea.Cmd {
	return nil
}

func (m selectModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		width := msg.Width
		height := max(msg.Height-4, 6)
		if width < 20 {
			width = 20
		}
		m.list.SetSize(width, height)
		return m, nil
	case tea.KeyMsg:
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

func (m selectModel) View() string {
	return "\n" + m.list.View()
}
