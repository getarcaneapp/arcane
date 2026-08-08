package diagnostics

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDiagnosticsServiceCollect(t *testing.T) {
	s := NewDiagnosticsService()

	rt, mem, _ := s.Collect()

	if rt.Goroutines <= 0 {
		assert.Positive(t, rt.Goroutines,
			"expected a positive goroutine count, got %d", rt.Goroutines)
	}

	assert.NotEmpty(t, rt.GoVersion,
		"expected a non-empty Go version")

	if rt.NumCPU <= 0 {
		assert.Positive(t, rt.NumCPU,
			"expected a positive CPU count, got %d", rt.NumCPU)
	}

	assert.NotEqual(t, 0, mem.Sys,
		"expected non-zero Sys memory")

}
