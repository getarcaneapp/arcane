package output

import (
	"context"
	"io"
)

type TransferOptions struct {
	Total        int64
	Label        string
	ShowProgress bool
}

// Copy copies a transfer while reporting bytes, or activity for unknown totals.
func Copy(ctx context.Context, dst io.Writer, src io.Reader, opts TransferOptions) (int64, error) {
	loading := StartTracker(ctx, Stderr(), opts.ShowProgress, opts.Label)
	loading.NextStep()
	defer loading.Stop()
	loading.SetProgress("", opts.Label, 0, opts.Total, true)
	written, err := io.Copy(dst, &transferReader{ctx: ctx, source: src, loading: loading, total: opts.Total, label: opts.Label})
	loading.Finish("", err)
	return written, err
}

type transferReader struct {
	ctx            context.Context
	source         io.Reader
	loading        *tracker
	label          string
	current, total int64
}

func (r *transferReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := r.source.Read(p)
	r.current += int64(n)
	r.loading.SetProgress("", r.label, r.current, r.total, true)
	return n, err
}
