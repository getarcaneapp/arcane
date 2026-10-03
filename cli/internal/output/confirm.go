package output

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

// Confirm uses a terminal selector, falling back to line input for scripts.
func Confirm(ctx context.Context, input io.Reader, out io.Writer, label string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	defer SuspendProgress()()
	if !IsTerminal(input) || !IsTerminal(out) {
		if _, err := fmt.Fprintf(out, "%s (y/N): ", strings.TrimSpace(label)); err != nil {
			return false, err
		}
		answer, err := ReadWithContext(ctx, func() (string, error) {
			var answer string
			_, err := fmt.Fscanln(input, &answer)
			return answer, err
		})
		if err != nil && !errors.Is(err, io.EOF) {
			if ctx.Err() != nil {
				return false, ctx.Err()
			}
			return false, fmt.Errorf("failed to read confirmation input: %w", err)
		}
		return strings.EqualFold(answer, "y") || strings.EqualFold(answer, "yes"), nil
	}
	model, err := tea.NewProgram(confirmModel{label: label, out: out}, tea.WithContext(ctx), tea.WithInput(input), tea.WithOutput(out)).Run()
	if promptErr := programErrInternal(ctx, err, "confirmation prompt"); promptErr != nil {
		return false, promptErr
	}
	result, ok := model.(confirmModel)
	if !ok {
		return false, errors.New("confirmation returned an unexpected result")
	}
	return result.confirmed, nil
}

// Secret reads a masked value on a terminal; scripts must pass flagName instead.
func Secret(ctx context.Context, input io.Reader, out io.Writer, label, flagName string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if !IsTerminal(input) || !IsTerminal(out) {
		return "", fmt.Errorf("%s is required; pass --%s", strings.ToLower(label), flagName)
	}
	defer SuspendProgress()()
	field := textinput.New()
	field.Prompt = label + ": "
	field.EchoMode = textinput.EchoPassword
	field.Focus()
	model, err := tea.NewProgram(secretModel{input: field}, tea.WithContext(ctx), tea.WithInput(input), tea.WithOutput(out)).Run()
	if promptErr := programErrInternal(ctx, err, "secret prompt"); promptErr != nil {
		return "", promptErr
	}
	result, ok := model.(secretModel)
	if !ok {
		return "", errors.New("secret prompt returned an unexpected result")
	}
	if !result.submitted {
		return "", context.Canceled
	}
	return result.input.Value(), nil
}

// ReadWithContext returns once read finishes or ctx is canceled; an abandoned
// blocking read ends with the process.
func ReadWithContext[T any](ctx context.Context, read func() (T, error)) (T, error) {
	type result struct {
		value T
		err   error
	}
	done := make(chan result, 1)
	go func() {
		value, err := read()
		done <- result{value, err}
	}()
	select {
	case r := <-done:
		return r.value, r.err
	case <-ctx.Done():
		var zero T
		return zero, ctx.Err()
	}
}

// programErrInternal maps prompt cancellation, including Ctrl+C in raw mode,
// to a context error.
func programErrInternal(ctx context.Context, err error, name string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(err, tea.ErrInterrupted) {
		return context.Canceled
	}
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

type secretModel struct {
	input           textinput.Model
	done, submitted bool
}

func (m secretModel) Init() tea.Cmd { return nil }

func (m secretModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.done {
		return m, nil
	}
	if key, ok := msg.(tea.KeyPressMsg); ok {
		switch key.String() {
		case "enter":
			m.done, m.submitted = true, true
			return m, tea.Quit
		case "esc":
			m.done = true
			return m, tea.Quit
		case "ctrl+c":
			m.done = true
			return m, tea.Interrupt
		}
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m secretModel) View() tea.View {
	if m.done {
		return tea.NewView("")
	}
	return tea.NewView(m.input.View() + "\n")
}

type confirmModel struct {
	label                string
	out                  io.Writer
	yes, confirmed, done bool
}

func (m confirmModel) Init() tea.Cmd { return nil }

func (m confirmModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.done {
		return m, nil
	}
	if key, ok := msg.(tea.KeyPressMsg); ok {
		switch strings.ToLower(key.String()) {
		case "left", "right", "up", "down", "tab", "shift+tab":
			m.yes = !m.yes
		case "y":
			m.yes = true
		case "n":
			m.yes = false
		case "enter":
			m.done, m.confirmed = true, m.yes
			return m, tea.Quit
		case "esc", "q":
			m.done = true
			return m, tea.Quit
		case "ctrl+c":
			m.done = true
			return m, tea.Interrupt
		}
	}
	return m, nil
}

func (m confirmModel) View() tea.View {
	if m.done {
		return tea.NewView("")
	}
	no, yes := "  No  ", "  Yes  "
	if m.yes {
		yes = "[ Yes ]"
	} else {
		no = "[ No ]"
	}
	hint := renderForInternal(m.out, statusMutedStyle, "←/→ or tab: choose · enter: submit · esc: cancel")
	return tea.NewView("\n  " + renderForInternal(m.out, headerStyle, m.label) + "\n\n  " + no + "  " + yes + "\n\n  " + hint + "\n")
}
