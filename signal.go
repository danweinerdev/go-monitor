package monitor

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
)

// SignalHandler handles OS signals for graceful shutdown and config reload.
type SignalHandler struct {
	logger     *slog.Logger
	shutdownCh chan struct{}
	reloadCh   chan struct{}
}

// NewSignalHandler creates a new signal handler.
func NewSignalHandler(logger *slog.Logger) *SignalHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &SignalHandler{
		logger:     logger,
		shutdownCh: make(chan struct{}),
		reloadCh:   make(chan struct{}, 1),
	}
}

// Start begins listening for signals.
// Returns a context that is cancelled on shutdown signals.
func (h *SignalHandler) Start(parent context.Context) context.Context {
	ctx, cancel := context.WithCancel(parent)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

	go func() {
		for {
			select {
			case sig := <-sigCh:
				switch sig {
				case syscall.SIGINT, syscall.SIGTERM:
					h.logger.Info("received shutdown signal", "signal", sig)
					close(h.shutdownCh)
					cancel()
					signal.Stop(sigCh)
					return
				case syscall.SIGHUP:
					h.logger.Info("received reload signal")
					select {
					case h.reloadCh <- struct{}{}:
					default:
					}
				}
			case <-parent.Done():
				signal.Stop(sigCh)
				return
			}
		}
	}()

	return ctx
}

// Shutdown returns a channel that is closed on shutdown signal.
func (h *SignalHandler) Shutdown() <-chan struct{} {
	return h.shutdownCh
}

// Reload returns a channel that receives on SIGHUP.
func (h *SignalHandler) Reload() <-chan struct{} {
	return h.reloadCh
}
