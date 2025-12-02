package progress

import (
	"context"
	"io"
	"os"
	"sync"

	"github.com/docker/buildx/util/logutil"
	"github.com/mitchellh/hashstructure/v2"
	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/util/progress/progressui"
	"github.com/opencontainers/go-digest"
	"github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

type printerState int

const (
	printerStateDone printerState = iota
	printerStateRunning
	printerStatePaused
)

type Printer struct {
	out  io.Writer
	mode progressui.DisplayMode
	opt  *printerOpts

	status    chan *client.SolveStatus
	interrupt chan interruptRequest
	state     printerState

	done      chan struct{}
	closeOnce sync.Once

	err          error
	warnings     []client.VertexWarning
	logMu        sync.Mutex
	logSourceMap map[digest.Digest]any
	metrics      *metricWriter

	// TODO: remove once we can use result context to pass build ref
	//  see https://github.com/docker/buildx/pull/1861
	buildRefsMu sync.Mutex
	buildRefs   map[string]string
}

func (p *Printer) Wait() error {
	p.closeOnce.Do(func() {
		close(p.status)
	})
	<-p.done
	return p.err
}

func (p *Printer) IsDone() bool {
	select {
	case <-p.done:
		return true
	default:
		return false
	}
}

func (p *Printer) Pause() error {
	done := make(chan struct{})
	p.interrupt <- interruptRequest{
		desiredState: printerStatePaused,
		done:         done,
	}

	// Need to wait for a response to confirm we have control
	// of the console output.
	<-done
	return nil
}

func (p *Printer) Resume() {
	p.interrupt <- interruptRequest{
		desiredState: printerStateRunning,
	}
	// Do not care about waiting for a response.
}

func (p *Printer) Write(s *client.SolveStatus) {
	p.status <- s
	if p.metrics != nil {
		p.metrics.Write(s)
	}
}

func (p *Printer) Warnings() []client.VertexWarning {
	return dedupWarnings(p.warnings)
}

func (p *Printer) ValidateLogSource(dgst digest.Digest, v any) bool {
	p.logMu.Lock()
	defer p.logMu.Unlock()
	src, ok := p.logSourceMap[dgst]
	if ok {
		if src == v {
			return true
		}
	} else {
		p.logSourceMap[dgst] = v
		return true
	}
	return false
}

func (p *Printer) ClearLogSource(v any) {
	p.logMu.Lock()
	defer p.logMu.Unlock()
	for d := range p.logSourceMap {
		if p.logSourceMap[d] == v {
			delete(p.logSourceMap, d)
		}
	}
}

func NewPrinter(ctx context.Context, out io.Writer, mode progressui.DisplayMode, opts ...PrinterOpt) (*Printer, error) {
	opt := &printerOpts{}
	for _, o := range opts {
		o(opt)
	}

	if v := os.Getenv("BUILDKIT_PROGRESS"); v != "" && mode == progressui.AutoMode {
		mode = progressui.DisplayMode(v)
	}

	d, err := progressui.NewDisplay(out, mode, opt.displayOpts...)
	if err != nil {
		return nil, err
	}

	pw := &Printer{
		out:       out,
		mode:      mode,
		opt:       opt,
		status:    make(chan *client.SolveStatus),
		interrupt: make(chan interruptRequest),
		state:     printerStateRunning,
		done:      make(chan struct{}),
		metrics:   opt.mw,
	}
	go pw.run(ctx, d)

	return pw, nil
}

func (p *Printer) run(ctx context.Context, d progressui.Display) {
	defer close(p.done)
	defer close(p.interrupt)

	var ss []*client.SolveStatus
	for {
		switch p.state {
		case printerStatePaused:
			ss, p.err = p.bufferDisplay(ctx, ss)
		case printerStateRunning:
			p.warnings, ss, p.err = p.updateDisplay(ctx, d, ss)
			if p.opt.onclose != nil {
				p.opt.onclose()
			}
		}

		if p.state == printerStateDone {
			break
		}
		d, _ = p.newDisplay()
	}
}

func (p *Printer) newDisplay() (progressui.Display, error) {
	return progressui.NewDisplay(p.out, p.mode, p.opt.displayOpts...)
}

func (p *Printer) updateDisplay(ctx context.Context, d progressui.Display, ss []*client.SolveStatus) ([]client.VertexWarning, []*client.SolveStatus, error) {
	p.logMu.Lock()
	p.logSourceMap = map[digest.Digest]any{}
	p.logMu.Unlock()

	resumeLogs := logutil.Pause(logrus.StandardLogger())
	defer resumeLogs()

	interruptCh := make(chan interruptRequest, 1)
	ingress := make(chan *client.SolveStatus)

	go func() {
		defer close(ingress)
		defer close(interruptCh)

		for _, s := range ss {
			ingress <- s
		}

		for {
			select {
			case s, ok := <-p.status:
				if !ok {
					return
				}
				ingress <- s
			case req := <-p.interrupt:
				interruptCh <- req
				return
			case <-ctx.Done():
				return
			}
		}
	}()

	warnings, err := d.UpdateFrom(context.Background(), ingress)
	if err == nil {
		err = context.Cause(ctx)
	}

	interrupt := <-interruptCh
	p.state = interrupt.desiredState
	interrupt.close()
	return warnings, nil, err
}

// bufferDisplay will buffer display updates from the status channel into a
// slice.
//
// This method returns if either status gets closed or if an interrupt is received.
func (p *Printer) bufferDisplay(ctx context.Context, ss []*client.SolveStatus) ([]*client.SolveStatus, error) {
	for {
		select {
		case s, ok := <-p.status:
			if !ok {
				p.state = printerStateDone
				return ss, nil
			}
			ss = append(ss, s)
		case req := <-p.interrupt:
			p.state = req.desiredState
			req.close()
			return ss, nil
		case <-ctx.Done():
			p.state = printerStateDone
			return nil, context.Cause(ctx)
		}
	}
}

func (p *Printer) WriteBuildRef(target string, ref string) {
	p.buildRefsMu.Lock()
	defer p.buildRefsMu.Unlock()
	if p.buildRefs == nil {
		p.buildRefs = map[string]string{}
	}
	p.buildRefs[target] = ref
}

func (p *Printer) BuildRefs() map[string]string {
	return p.buildRefs
}

type printerOpts struct {
	displayOpts []progressui.DisplayOpt
	mw          *metricWriter

	onclose func()
}

type PrinterOpt func(b *printerOpts)

func WithPhase(phase string) PrinterOpt {
	return func(opt *printerOpts) {
		opt.displayOpts = append(opt.displayOpts, progressui.WithPhase(phase))
	}
}

func WithDesc(text string, console string) PrinterOpt {
	return func(opt *printerOpts) {
		opt.displayOpts = append(opt.displayOpts, progressui.WithDesc(text, console))
	}
}

func WithMetrics(mp metric.MeterProvider, attrs attribute.Set) PrinterOpt {
	return func(opt *printerOpts) {
		opt.mw = newMetrics(mp, attrs)
	}
}

func WithOnClose(onclose func()) PrinterOpt {
	return func(opt *printerOpts) {
		opt.onclose = onclose
	}
}

func dedupWarnings(inp []client.VertexWarning) []client.VertexWarning {
	m := make(map[uint64]client.VertexWarning)
	for _, w := range inp {
		wcp := w
		wcp.Vertex = ""
		if wcp.SourceInfo != nil {
			wcp.SourceInfo.Definition = nil
		}
		h, err := hashstructure.Hash(wcp, hashstructure.FormatV2, nil)
		if err != nil {
			continue
		}
		if _, ok := m[h]; !ok {
			m[h] = w
		}
	}
	res := make([]client.VertexWarning, 0, len(m))
	for _, w := range m {
		res = append(res, w)
	}
	return res
}

type interruptRequest struct {
	desiredState printerState
	done         chan<- struct{}
}

func (req *interruptRequest) close() {
	if req.done != nil {
		close(req.done)
	}
}
