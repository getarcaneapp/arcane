package output

import (
	"fmt"
	"io"
	"sync"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"go.withmatt.com/size"
)

type spinnerDoneMsg struct{}

type spinnerModel struct {
	spinner spinner.Model
	label   string
}

func newSpinnerModel(label string) spinnerModel {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(arcanePurple)
	return spinnerModel{spinner: s, label: label}
}

func (m spinnerModel) Init() tea.Cmd {
	return m.spinner.Tick
}

func (m spinnerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case spinnerDoneMsg:
		return m, tea.Quit
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			return m, tea.Quit
		}
	}

	var cmd tea.Cmd
	m.spinner, cmd = m.spinner.Update(msg)
	return m, cmd
}

func (m spinnerModel) View() string {
	return fmt.Sprintf("%s %s", m.spinner.View(), m.label)
}

// Spinner renders a Bubble Tea spinner inline.
type Spinner struct {
	program *tea.Program
	done    chan struct{}
}

// StartSpinner starts a spinner with the given label.
func StartSpinner(label string) *Spinner {
	model := newSpinnerModel(label)
	program := tea.NewProgram(model)
	done := make(chan struct{})

	spinner := &Spinner{program: program, done: done}
	go func() {
		_, _ = program.Run()
		close(done)
	}()

	return spinner
}

// Stop stops the spinner and moves to the next line.
func (s *Spinner) Stop() {
	if s == nil || s.program == nil {
		return
	}
	s.program.Send(spinnerDoneMsg{})
	<-s.done
	fmt.Println()
}

type progressUpdateMsg struct {
	current int64
	total   int64
}

type progressLabelMsg string

type progressDoneMsg struct{}

type progressModel struct {
	progress progress.Model
	label    string
	current  int64
	total    int64
}

func newProgressModel(label string, total int64) progressModel {
	p := progress.New(progress.WithDefaultGradient())
	p.Width = 40
	return progressModel{progress: p, label: label, total: total}
}

func (m progressModel) Init() tea.Cmd {
	return m.progress.Init()
}

func (m progressModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case progressUpdateMsg:
		m.current = msg.current
		if msg.total > 0 {
			m.total = msg.total
		}
	case progressLabelMsg:
		m.label = string(msg)
	case progressDoneMsg:
		return m, tea.Quit
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			return m, tea.Quit
		}
	case tea.WindowSizeMsg:
		if msg.Width > 10 {
			m.progress.Width = msg.Width - 10
		}
	}

	var cmd tea.Cmd
	updated, cmd := m.progress.Update(msg)
	if model, ok := updated.(progress.Model); ok {
		m.progress = model
	}
	return m, cmd
}

func (m progressModel) View() string {
	percent := 0.0
	if m.total > 0 {
		percent = float64(m.current) / float64(m.total)
		if percent < 0 {
			percent = 0
		} else if percent > 1 {
			percent = 1
		}
	}

	bar := m.progress.ViewAs(percent)
	if m.total > 0 {
		return fmt.Sprintf("%s\n%s %s/%s", m.label, bar, safeCapacity(m.current), safeCapacity(m.total))
	}
	return fmt.Sprintf("%s\n%s", m.label, bar)
}

func safeCapacity(value int64) size.Capacity {
	if value < 0 {
		return size.Capacity(0)
	}
	return size.Capacity(uint64(value))
}

// Progress renders a Bubble Tea progress bar inline.
type Progress struct {
	program *tea.Program
	done    chan struct{}

	mu      sync.Mutex
	current int64
	total   int64
}

// StartProgress starts a progress bar with the given label and total.
func StartProgress(label string, total int64) *Progress {
	model := newProgressModel(label, total)
	program := tea.NewProgram(model)
	done := make(chan struct{})

	progressUI := &Progress{program: program, done: done, total: total}
	go func() {
		_, _ = program.Run()
		close(done)
	}()

	return progressUI
}

// SetLabel updates the progress label.
func (p *Progress) SetLabel(label string) {
	if p == nil || p.program == nil {
		return
	}
	p.program.Send(progressLabelMsg(label))
}

// SetTotal updates the total value.
func (p *Progress) SetTotal(total int64) {
	if p == nil || p.program == nil {
		return
	}
	p.mu.Lock()
	p.total = total
	current := p.current
	p.mu.Unlock()
	p.program.Send(progressUpdateMsg{current: current, total: total})
}

// SetCurrent sets the current progress value.
func (p *Progress) SetCurrent(current int64) {
	if p == nil || p.program == nil {
		return
	}
	p.mu.Lock()
	p.current = current
	total := p.total
	p.mu.Unlock()
	p.program.Send(progressUpdateMsg{current: current, total: total})
}

// Add increments progress by the given value.
func (p *Progress) Add(delta int64) {
	if p == nil || p.program == nil {
		return
	}
	p.mu.Lock()
	p.current += delta
	current := p.current
	total := p.total
	p.mu.Unlock()
	p.program.Send(progressUpdateMsg{current: current, total: total})
}

// Stop stops the progress bar and moves to the next line.
func (p *Progress) Stop() {
	if p == nil || p.program == nil {
		return
	}
	p.program.Send(progressDoneMsg{})
	<-p.done
	fmt.Println()
}

// NewProgressReader wraps a reader to report progress updates.
func NewProgressReader(r io.Reader, progress *Progress) io.Reader {
	if progress == nil {
		return r
	}
	return &progressReader{reader: r, progress: progress}
}

type progressReader struct {
	reader   io.Reader
	progress *Progress
}

func (p *progressReader) Read(buf []byte) (int, error) {
	n, err := p.reader.Read(buf)
	if n > 0 {
		p.progress.Add(int64(n))
	}
	return n, err
}
