package util

import (
	"context"
	"sync"
	"time"

	"github.com/pkg/errors"
	"go.viam.com/rdk/logging"
)

// Routine manages the lifecycle of a long-running background goroutine.
// It serializes Run/Stop, owns a detached context for cancellation, and
// guarantees Stop only returns once the routine has exited.
//
// Create a named Routine using NewRoutineInstance. After Stop returns
// (or the routine returns on its own and Stop is then called), Run may
// be called again to relaunch the routine.
type Routine struct {
	name    string
	mu      sync.Mutex
	cancel  context.CancelFunc // non-nil iff a routine is running
	stopped chan struct{}      // closed when the running routine returns
}

func NewRoutineInstance(name string) *Routine {
	return &Routine{
		name: name,
	}
}

// Start launches tickFunc in a new goroutine and returns immediately.
// Returns an error if a routine is already running.
//
// The goroutine runs tickFunc in a loop with a frequency of tickRate.
// The context passed to tickFunc is detached from any caller's context;
// it is canceled only when Stop is called.
func (r *Routine) Start(logger logging.Logger, tickFunc func(ctx context.Context), tickRate time.Duration) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.cancel != nil {
		return errors.Errorf("%s is already running", r.name)
	}

	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	r.cancel = cancel
	r.stopped = stopped

	go func() {
		defer close(stopped)

		ticker := time.NewTicker(tickRate)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				logger.Infof("%s stopping due to context cancellation", r.name)
				return
			case <-ticker.C:
				tickFunc(ctx)
			}
		}
	}()

	return nil
}

// Stop cancels the routine's context and blocks until the goroutine
// has exited. Returns an error if no routine is running.
func (r *Routine) Stop() error {
	r.mu.Lock()
	cancel := r.cancel
	stopped := r.stopped
	r.cancel = nil
	r.stopped = nil
	r.mu.Unlock()

	if cancel == nil {
		return errors.Errorf("%s is not running", r.name)
	}

	cancel()
	<-stopped
	return nil
}
