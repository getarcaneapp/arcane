package signals

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"
)

/*
This code is adapted from:
https://github.com/kubernetes-sigs/controller-runtime/blob/8499b67e316a03b260c73f92d0380de8cd2e97a1/pkg/manager/signals/signal.go
Copyright 2017 The Kubernetes Authors.
License: Apache2 (https://github.com/kubernetes-sigs/controller-runtime/blob/8499b67e316a03b260c73f92d0380de8cd2e97a1/LICENSE)

Also referenced from pocket-id/pocket-id
*/

var onlyOneSignalHandler = make(chan struct{})

const forcedShutdownTimeout = 30 * time.Second

// SignalContext returns a context that is canceled when the application receives an interrupt signal.
// A second signal or an expired graceful-shutdown deadline forces the process to exit.
func SignalContext(parentCtx context.Context) context.Context {
	close(onlyOneSignalHandler) // Panics when called twice

	ctx, cancel := context.WithCancel(parentCtx)

	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		slog.Info("Received interrupt signal. Shutting down…")
		cancel()

		shutdownTimer := time.NewTimer(forcedShutdownTimeout)
		defer shutdownTimer.Stop()

		select {
		case <-sigCh:
			slog.Warn("Received a second interrupt signal. Forcing an immediate shutdown.")
		case <-shutdownTimer.C:
			slog.Error("Graceful shutdown timed out. Forcing process exit.", "timeout", forcedShutdownTimeout)
		}
		// Go cannot safely terminate a hung goroutine. Exit the process instead of
		// allowing Bootstrap to close dependencies that goroutine may still use.
		os.Exit(1)
	}()

	return ctx
}
