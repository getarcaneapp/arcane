package services

import "testing"

func TestDiagnosticsServiceCollect(t *testing.T) {
	s := NewDiagnosticsService()

	rt, mem, _ := s.Collect()

	if rt.Goroutines <= 0 {
		t.Errorf("expected a positive goroutine count, got %d", rt.Goroutines)
	}
	if rt.GoVersion == "" {
		t.Error("expected a non-empty Go version")
	}
	if rt.NumCPU <= 0 {
		t.Errorf("expected a positive CPU count, got %d", rt.NumCPU)
	}
	if mem.Sys == 0 {
		t.Error("expected non-zero Sys memory")
	}
}
