package main

import (
	"bytes"
	"context"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/vaarde/mise/cmd"
)

// waitFor polls until cond holds or the deadline passes, so the tests do
// not depend on goroutine scheduling.
func waitFor(t *testing.T, cond func() bool) bool {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(time.Millisecond)
	}
	return false
}

func TestFirstInterruptCancelsRatherThanKills(t *testing.T) {
	signals := make(chan os.Signal, 2)
	var out bytes.Buffer

	exited := make(chan int, 1)
	ctx, stop := watchInterrupts(context.Background(), signals, &out, func(code int) {
		exited <- code
	})
	defer stop()

	signals <- os.Interrupt

	require.True(t, waitFor(t, func() bool { return ctx.Err() != nil }),
		"the first signal must cancel the context")

	// Cancelling is what lets the deferred lock release and state save
	// run. Exiting here would skip both.
	assert.Empty(t, exited, "the first signal must not exit the process")
	assert.Contains(t, out.String(), "recording what landed")
	assert.Contains(t, out.String(), "Ctrl-C again")
}

func TestSecondInterruptQuits(t *testing.T) {
	signals := make(chan os.Signal, 2)
	var out bytes.Buffer

	exited := make(chan int, 1)
	ctx, stop := watchInterrupts(context.Background(), signals, &out, func(code int) {
		exited <- code
	})
	defer stop()

	signals <- os.Interrupt
	require.True(t, waitFor(t, func() bool { return ctx.Err() != nil }))

	signals <- os.Interrupt

	select {
	case code := <-exited:
		assert.Equal(t, cmd.ExitCodeInterrupted, code)
	case <-time.After(2 * time.Second):
		t.Fatal("the second signal must quit outright, as the escape hatch for a stuck unwind")
	}

	assert.Contains(t, out.String(), "mise fetch",
		"a hard quit leaves state uncertain, so it must say how to re-sync")
}

func TestSigtermIsHandledLikeAnInterrupt(t *testing.T) {
	// CI runners and container orchestrators send SIGTERM, not SIGINT.
	signals := make(chan os.Signal, 2)
	var out bytes.Buffer

	ctx, stop := watchInterrupts(context.Background(), signals, &out, func(int) {})
	defer stop()

	signals <- syscall.SIGTERM

	assert.True(t, waitFor(t, func() bool { return ctx.Err() != nil }))
}

func TestNoSignalLeavesTheContextAlone(t *testing.T) {
	signals := make(chan os.Signal, 2)
	var out bytes.Buffer

	ctx, stop := watchInterrupts(context.Background(), signals, &out, func(int) {
		t.Error("nothing should exit without a signal")
	})
	defer stop()

	time.Sleep(50 * time.Millisecond)

	assert.NoError(t, ctx.Err())
	assert.Empty(t, out.String())
}

func TestWatcherStopsWithTheRun(t *testing.T) {
	signals := make(chan os.Signal, 2)
	var out bytes.Buffer

	_, stop := watchInterrupts(context.Background(), signals, &out, func(int) {
		t.Error("a completed run must not exit on a late signal")
	})

	// A command that finished normally: the watcher goroutine has to go
	// away rather than linger waiting for a signal.
	stop()
	time.Sleep(20 * time.Millisecond)

	signals <- os.Interrupt
	time.Sleep(50 * time.Millisecond)

	assert.Empty(t, out.String())
}
