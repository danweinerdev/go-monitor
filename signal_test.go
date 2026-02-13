package monitor

import (
	"context"
	"testing"
	"time"
)

func TestSignalHandlerStart(t *testing.T) {
	handler := NewSignalHandler(nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCtx := handler.Start(ctx)

	// Context should not be cancelled yet.
	select {
	case <-sigCtx.Done():
		t.Error("Context should not be cancelled yet")
	default:
	}

	// Cancel parent context to clean up.
	cancel()

	select {
	case <-sigCtx.Done():
	case <-time.After(1 * time.Second):
		t.Error("Context should be cancelled after parent cancel")
	}
}

func TestSignalHandlerChannels(t *testing.T) {
	handler := NewSignalHandler(nil)

	// Shutdown channel should not be closed initially.
	select {
	case <-handler.Shutdown():
		t.Error("Shutdown channel should not be closed initially")
	default:
	}

	// Reload channel should be empty initially.
	select {
	case <-handler.Reload():
		t.Error("Reload channel should be empty initially")
	default:
	}
}
