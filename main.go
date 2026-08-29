package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/vaarde/mise/cmd"
)

func main() {
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)

	ctx, stop := watchInterrupts(context.Background(), signals, os.Stderr, os.Exit)
	defer stop()

	if err := cmd.Execute(ctx); err != nil {
		os.Exit(cmd.ExitCode(err))
	}
}

// watchInterrupts returns a context cancelled by the first signal, and
// arranges for the second one to quit outright.
//
// A hard kill in the middle of an apply is the worst case Mise has: the
// workspace lock is left behind and the resources that already landed
// are never recorded, so the next plan offers to create them again.
// Cancelling the context instead unwinds through the normal paths —
// in-flight requests abort, the lock is released, and state is written
// for whatever succeeded. The second signal is the escape hatch for
// when that unwind is itself stuck.
//
// The signal channel and exit function are parameters so the behaviour
// can be tested without a console: Windows has no way to deliver a
// Ctrl-C to a child process from a test.
func watchInterrupts(
	parent context.Context,
	signals <-chan os.Signal,
	out io.Writer,
	exit func(int),
) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)

	go func() {
		select {
		case <-ctx.Done():
			return
		case <-signals:
		}

		fmt.Fprintln(out,
			"\nInterrupted. Stopping after the current step and recording what landed."+
				"\nPress Ctrl-C again to quit immediately.")
		cancel()

		select {
		case <-signals:
		case <-parent.Done():
			// The whole run is being torn down anyway.
			return
		}

		fmt.Fprintln(out,
			"\nQuit. State may not reflect what was applied — run 'mise fetch' to re-sync.")
		exit(cmd.ExitCodeInterrupted)
	}()

	return ctx, cancel
}
