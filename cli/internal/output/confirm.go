package output

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/muesli/cancelreader"
)

type (
	secretModel struct {
		input           textinput.Model
		done, submitted bool
	}

	confirmModel struct {
		out                  io.Writer
		label                string
		yes, confirmed, done bool
	}
)

// Confirm requires terminal input, using a selector or line input depending on the output terminal.
func Confirm(ctx context.Context, input io.Reader, out io.Writer, label string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if !IsTerminal(input) {
		return false, fmt.Errorf("confirmation required: re-run with --yes (%s)", label)
	}
	defer SuspendProgress()()
	if !IsTerminal(out) {
		if _, err := fmt.Fprintf(out, "%s (y/N): ", strings.TrimSpace(label)); err != nil {
			return false, err
		}
		answer, err := ReadWithContext(ctx, input, readLine)
		if ctxErr := ctx.Err(); ctxErr != nil {
			return false, ctxErr
		}
		if err != nil && !errors.Is(err, io.EOF) {
			return false, fmt.Errorf("failed to read confirmation input: %w", err)
		}
		return strings.EqualFold(strings.TrimSpace(answer), "y") || strings.EqualFold(strings.TrimSpace(answer), "yes"), nil
	}
	model, err := tea.NewProgram(confirmModel{label: label, out: out}, tea.WithContext(ctx), tea.WithInput(input), tea.WithOutput(out)).Run()
	if ctxErr := ctx.Err(); ctxErr != nil {
		return false, ctxErr
	}
	if errors.Is(err, tea.ErrInterrupted) {
		return false, context.Canceled
	}
	if err != nil {
		return false, fmt.Errorf("confirmation prompt: %w", err)
	}
	result, ok := model.(confirmModel)
	if !ok {
		return false, errors.New("confirmation returned an unexpected result")
	}
	return result.confirmed, nil
}

// Secret reads a masked value from a terminal, or one line from piped input.
// An empty value asks the caller to pass flagName instead.
func Secret(ctx context.Context, input io.Reader, out io.Writer, label, flagName string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if !IsTerminal(input) {
		value, err := ReadWithContext(ctx, input, readLine)
		if err != nil {
			return "", err
		}
		if value == "" {
			return "", fmt.Errorf("%s is required; pass --%s", strings.ToLower(label), flagName)
		}
		return value, nil
	}
	defer SuspendProgress()()
	field := textinput.New()
	field.Prompt = label + ": "
	field.EchoMode = textinput.EchoPassword
	field.Focus()
	model, err := tea.NewProgram(secretModel{input: field}, tea.WithContext(ctx), tea.WithInput(input), tea.WithOutput(out)).Run()
	if ctxErr := ctx.Err(); ctxErr != nil {
		return "", ctxErr
	}
	if errors.Is(err, tea.ErrInterrupted) {
		return "", context.Canceled
	}
	if err != nil {
		return "", fmt.Errorf("secret prompt: %w", err)
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

// ReadWithContext reads input until read returns or ctx is canceled.
// Pollable files cancel in place; other blocking readers use an asynchronous read.
func ReadWithContext[T any](ctx context.Context, input io.Reader, read func(io.Reader) (T, error)) (T, error) {
	var value T
	var err, closeErr error
	var mode os.FileMode
	isFile := false
	if file, ok := input.(interface{ Stat() (os.FileInfo, error) }); ok {
		if info, statErr := file.Stat(); statErr == nil {
			mode, isFile = info.Mode(), true
		}
	}
	var reader cancelreader.CancelReader
	if isFile && !mode.IsRegular() {
		reader, _ = cancelreader.NewReader(input)
	}
	switch {
	case isFile && mode.IsRegular():
		// kqueue never reports a regular file readable at EOF, so poll nothing.
		value, err = read(input)
	case reader != nil:
		stop := context.AfterFunc(ctx, func() { reader.Cancel() })
		value, err = read(reader)
		stop()
		if releaseErr := reader.Close(); releaseErr != nil {
			closeErr = fmt.Errorf("release input reader: %w", releaseErr)
		}
	default:
		value, err = readAsync(ctx, input, read)
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		var zero T
		return zero, errors.Join(ctxErr, closeErr)
	}
	return value, errors.Join(err, closeErr)
}

// readAsync stops waiting on cancellation; its goroutine exits when input returns.
func readAsync[T any](ctx context.Context, input io.Reader, read func(io.Reader) (T, error)) (T, error) {
	type result struct {
		value T
		err   error
	}
	done := make(chan result, 1)
	go func() {
		value, err := read(input)
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

// readLine reads byte by byte so input after the newline stays
// available to the next prompt.
func readLine(r io.Reader) (string, error) {
	var line []byte
	buf := make([]byte, 1)
	for {
		n, err := r.Read(buf)
		if n > 0 && buf[0] == '\n' {
			break
		}
		if n > 0 {
			line = append(line, buf[0])
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", err
		}
	}
	return strings.TrimSuffix(string(line), "\r"), nil
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
	hint := renderFor(m.out, statusMutedStyle, "←/→ or tab: choose · enter: submit · esc: cancel")
	return tea.NewView("\n  " + renderFor(m.out, headerStyle, m.label) + "\n\n  " + no + "  " + yes + "\n\n  " + hint + "\n")
}
