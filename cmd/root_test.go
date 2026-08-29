package cmd

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestInterruptedCommandsExit130(t *testing.T) {
	// 128 + SIGINT, the convention a shell and a CI runner both already
	// understand.
	assert.Equal(t, 130, ExitCode(context.Canceled))
	assert.Equal(t, 130, ExitCode(fmt.Errorf("apply interrupted: %w", context.Canceled)))
}

func TestATimeoutIsAFailureNotAnInterrupt(t *testing.T) {
	// DeadlineExceeded means Mise gave up, which is a real failure. Only
	// the operator's own cancellation earns 130.
	assert.Equal(t, 1, ExitCode(context.DeadlineExceeded))
}

func TestAnExplicitExitCodeBeatsCancellation(t *testing.T) {
	// Drift wraps its own status; a cancelled context underneath must not
	// silently rewrite it.
	assert.Equal(t, ExitCodeDrift, ExitCode(&driftDetectedError{count: 1}))
}
