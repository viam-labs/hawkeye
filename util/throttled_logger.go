package util

import (
	"context"
	"sync"
	"time"

	"go.viam.com/rdk/logging"
)

type throttledLogInterval time.Duration

const (
	LOG_EVERY_5_SECS  = throttledLogInterval(5 * time.Second)
	LOG_EVERY_10_SECS = throttledLogInterval(10 * time.Second)
)

// ThrottledLogger wraps a Viam Logger to bypass the RDK's built-in
// deduplication and throttle log calls to at most one per minInterval.
// Debug/Info/Warn/Error are all throttled (both context and non-context
// variants); Fatal is left unwrapped so process-exit calls always go through.
type ThrottledLogger struct {
	logging.Logger

	interval throttledLogInterval

	mu          sync.Mutex
	lastLogTime time.Time
}

func NewThrottledLogger(logger logging.Logger, interval throttledLogInterval) *ThrottledLogger {
	throttledLogger := logger.Sublogger("throttled")
	throttledLogger.NeverDeduplicate()
	return &ThrottledLogger{
		Logger:   throttledLogger,
		interval: interval,
	}
}

// shouldEmit returns true (and advances the rate-limit window) when at least
// minInterval has passed since the last successful emit.
func (l *ThrottledLogger) shouldEmit() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if now.Sub(l.lastLogTime) < time.Duration(l.interval) {
		return false
	}
	l.lastLogTime = now
	return true
}

// Context-aware variants.

func (l *ThrottledLogger) CDebug(ctx context.Context, args ...any) {
	if l.shouldEmit() {
		l.Logger.CDebug(ctx, args...)
	}
}

func (l *ThrottledLogger) CDebugf(ctx context.Context, template string, args ...any) {
	if l.shouldEmit() {
		l.Logger.CDebugf(ctx, template, args...)
	}
}

func (l *ThrottledLogger) CDebugw(ctx context.Context, msg string, keysAndValues ...any) {
	if l.shouldEmit() {
		l.Logger.CDebugw(ctx, msg, keysAndValues...)
	}
}

func (l *ThrottledLogger) CInfo(ctx context.Context, args ...any) {
	if l.shouldEmit() {
		l.Logger.CInfo(ctx, args...)
	}
}

func (l *ThrottledLogger) CInfof(ctx context.Context, template string, args ...any) {
	if l.shouldEmit() {
		l.Logger.CInfof(ctx, template, args...)
	}
}

func (l *ThrottledLogger) CInfow(ctx context.Context, msg string, keysAndValues ...any) {
	if l.shouldEmit() {
		l.Logger.CInfow(ctx, msg, keysAndValues...)
	}
}

func (l *ThrottledLogger) CWarn(ctx context.Context, args ...any) {
	if l.shouldEmit() {
		l.Logger.CWarn(ctx, args...)
	}
}

func (l *ThrottledLogger) CWarnf(ctx context.Context, template string, args ...any) {
	if l.shouldEmit() {
		l.Logger.CWarnf(ctx, template, args...)
	}
}

func (l *ThrottledLogger) CWarnw(ctx context.Context, msg string, keysAndValues ...any) {
	if l.shouldEmit() {
		l.Logger.CWarnw(ctx, msg, keysAndValues...)
	}
}

func (l *ThrottledLogger) CError(ctx context.Context, args ...any) {
	if l.shouldEmit() {
		l.Logger.CError(ctx, args...)
	}
}

func (l *ThrottledLogger) CErrorf(ctx context.Context, template string, args ...any) {
	if l.shouldEmit() {
		l.Logger.CErrorf(ctx, template, args...)
	}
}

func (l *ThrottledLogger) CErrorw(ctx context.Context, msg string, keysAndValues ...any) {
	if l.shouldEmit() {
		l.Logger.CErrorw(ctx, msg, keysAndValues...)
	}
}

// Non-context (Zap-compatible) variants.

func (l *ThrottledLogger) Debug(args ...any) {
	if l.shouldEmit() {
		l.Logger.Debug(args...)
	}
}

func (l *ThrottledLogger) Debugf(template string, args ...any) {
	if l.shouldEmit() {
		l.Logger.Debugf(template, args...)
	}
}

func (l *ThrottledLogger) Debugw(msg string, keysAndValues ...any) {
	if l.shouldEmit() {
		l.Logger.Debugw(msg, keysAndValues...)
	}
}

func (l *ThrottledLogger) Info(args ...any) {
	if l.shouldEmit() {
		l.Logger.Info(args...)
	}
}

func (l *ThrottledLogger) Infof(template string, args ...any) {
	if l.shouldEmit() {
		l.Logger.Infof(template, args...)
	}
}

func (l *ThrottledLogger) Infow(msg string, keysAndValues ...any) {
	if l.shouldEmit() {
		l.Logger.Infow(msg, keysAndValues...)
	}
}

func (l *ThrottledLogger) Warn(args ...any) {
	if l.shouldEmit() {
		l.Logger.Warn(args...)
	}
}

func (l *ThrottledLogger) Warnf(template string, args ...any) {
	if l.shouldEmit() {
		l.Logger.Warnf(template, args...)
	}
}

func (l *ThrottledLogger) Warnw(msg string, keysAndValues ...any) {
	if l.shouldEmit() {
		l.Logger.Warnw(msg, keysAndValues...)
	}
}

func (l *ThrottledLogger) Error(args ...any) {
	if l.shouldEmit() {
		l.Logger.Error(args...)
	}
}

func (l *ThrottledLogger) Errorf(template string, args ...any) {
	if l.shouldEmit() {
		l.Logger.Errorf(template, args...)
	}
}

func (l *ThrottledLogger) Errorw(msg string, keysAndValues ...any) {
	if l.shouldEmit() {
		l.Logger.Errorw(msg, keysAndValues...)
	}
}
