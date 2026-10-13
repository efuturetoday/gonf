package gonf

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// Watcher is implemented by Sources that notice when their values change,
// such as a file that is edited or a key store that pushes updates. Watch
// calls changed whenever the values may differ from what Load last
// returned; changed is safe for concurrent use and never blocks. Watch
// blocks until ctx ends and returns an error only if it can no longer
// watch.
type Watcher interface {
	Watch(ctx context.Context, changed func()) error
}

// reloadDelay is how long Watch waits after a change before it reloads, so
// that a burst of changes, such as an editor writing a file in several
// steps, causes one reload.
const reloadDelay = 100 * time.Millisecond

// watchOptions are the options of Watch: those of Load and its own.
type watchOptions struct {
	loadOptions
	onReload func(cfg *Config, err error)
	logger   *slog.Logger
}

// A WatchOption configures Watch. Every Option is one, so Watch takes
// everything Load takes; OnReload and WithLogger are WatchOptions only and
// do not compile as options of Load.
type WatchOption interface {
	applyWatch(*watchOptions)
}

type watchOptionFunc func(*watchOptions)

func (f watchOptionFunc) applyWatch(o *watchOptions) { f(o) }

// applyWatch makes an Option a WatchOption.
func (o Option) applyWatch(w *watchOptions) { o(&w.loadOptions) }

// A Watcher that fails is watched again after minRetryDelay, then after
// twice as long each time, up to maxRetryDelay.
const (
	minRetryDelay = time.Second
	maxRetryDelay = time.Minute
)

// OnReload makes Watch call fn after every reload: with the new Config, or
// with the error that kept the previous one. fn is never called
// concurrently, and no reload starts while it runs.
func OnReload(fn func(cfg *Config, err error)) WatchOption {
	return watchOptionFunc(func(o *watchOptions) { o.onReload = fn })
}

// WithLogger sets the logger Watch reports to: reloads at level Info;
// failed reloads, failed Watchers and Sources that failed to close at level
// Warn. Without it Watch uses slog.Default. Load does not log; it returns
// its errors.
func WithLogger(l *slog.Logger) WatchOption {
	return watchOptionFunc(func(o *watchOptions) { o.logger = l })
}

// Live holds the current Config of Watch. It is safe for concurrent use.
type Live struct {
	cfg atomic.Pointer[Config]
	// done is closed when watching has stopped.
	done chan struct{}
}

// Config returns the current Config. Read it again for every unit of work,
// such as a request; a Config, and a struct bound from it, never changes.
func (l *Live) Config() *Config {
	return l.cfg.Load()
}

// Watch loads the configuration like Load and reloads it whenever a Source
// that implements Watcher reports a change, until ctx ends.
//
// A reload is a complete Load with the same options: Sources, factories,
// profiles and all Mappings. If it succeeds, Live.Config returns the new
// Config; if it fails, the previous Config stays. Either way the function
// given with OnReload learns about it, and Watch logs it (see WithLogger). A Watcher that fails is reported the
// same way, as a SourceError; it is watched again after a delay that grows
// from one second to one minute, and every retry also reloads, since
// changes may have been missed.
//
// If the first Load fails, Watch returns its error and does not watch.
func Watch(ctx context.Context, opts ...WatchOption) (*Live, error) {
	var o watchOptions
	for _, opt := range opts {
		opt.applyWatch(&o)
	}
	cfg, created, err := load(ctx, &o.loadOptions)
	if err != nil {
		_ = release(created, nil) // the error of load matters, not this one
		return nil, err
	}
	l := &Live{done: make(chan struct{})}
	l.cfg.Store(cfg)
	w := &reloader{
		live:     l,
		opts:     &o.loadOptions,
		onReload: o.onReload,
		logger:   o.logger,
		changed:  make(chan struct{}, 1),
		failed:   make(chan error),
	}
	go w.run(ctx, cfg, created)
	return l, nil
}

// reloader is the state of one Watch.
type reloader struct {
	live     *Live
	opts     *loadOptions
	onReload func(cfg *Config, err error)
	// logger is nil for slog.Default, which is read when logging so that a
	// later slog.SetDefault applies.
	logger *slog.Logger
	// changed holds at most one pending change, so changes that arrive
	// while one is pending are merged into it.
	changed chan struct{}
	// failed receives the errors of Watchers.
	failed chan error
}

// run reloads on every change until ctx ends. created holds the Sources
// that factories created for cfg; run closes them once they are no longer
// used.
func (w *reloader) run(ctx context.Context, cfg *Config, created []Source) {
	defer close(w.live.done)
	stop := w.start(ctx, cfg)
	defer func() {
		stop()
		w.closed(ctx, release(created, nil))
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case err := <-w.failed:
			w.log().WarnContext(ctx, "gonf: watching a source failed, retrying", "error", err)
			w.report(nil, err)
		case <-w.changed:
			if !w.settle(ctx) {
				return
			}
			next, nextCreated, err := load(ctx, w.opts)
			if err != nil {
				w.closed(ctx, release(nextCreated, created))
				if ctx.Err() != nil {
					return
				}
				w.log().WarnContext(ctx, "gonf: reload failed, keeping the previous configuration", "error", err)
				w.report(nil, err)
				continue
			}
			w.live.cfg.Store(next)
			stop()
			w.closed(ctx, release(created, nextCreated))
			created = nextCreated
			stop = w.start(ctx, next)
			w.log().InfoContext(ctx, "gonf: configuration reloaded")
			w.report(next, nil)
		}
	}
}

// start runs Watch of every Source of cfg that implements Watcher. The
// returned function stops them and waits until they have returned.
func (w *reloader) start(ctx context.Context, cfg *Config) (stop func()) {
	ctx, cancel := context.WithCancel(ctx)
	var running sync.WaitGroup
	for _, s := range cfg.sources {
		watcher, ok := s.src.(Watcher)
		if !ok {
			continue
		}
		running.Add(1)
		go func() {
			defer running.Done()
			w.keepWatching(ctx, s.src.Name(), watcher)
		}()
	}
	return func() {
		cancel()
		running.Wait()
	}
}

// keepWatching runs watcher until ctx ends. If Watch fails, it reports the
// error, waits with a growing delay and watches again; since changes may
// have been missed meanwhile, it also asks for a reload.
func (w *reloader) keepWatching(ctx context.Context, name string, watcher Watcher) {
	delay := minRetryDelay
	for {
		err := watcher.Watch(ctx, w.signal)
		if err == nil || ctx.Err() != nil {
			return
		}
		select {
		case w.failed <- &SourceError{Source: name, Err: err}:
		case <-ctx.Done():
			return
		}
		t := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
		}
		delay = min(2*delay, maxRetryDelay)
		w.signal()
	}
}

// signal records a change without blocking.
func (w *reloader) signal() {
	select {
	case w.changed <- struct{}{}:
	default:
	}
}

// settle waits reloadDelay and drops the changes that arrived meanwhile,
// since the reload covers them. It returns false if ctx ended.
func (w *reloader) settle(ctx context.Context) bool {
	t := time.NewTimer(reloadDelay)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
	}
	select {
	case <-w.changed:
	default:
	}
	return true
}

// log returns the logger given with WithLogger or slog.Default.
func (w *reloader) log() *slog.Logger {
	if w.logger != nil {
		return w.logger
	}
	return slog.Default()
}

// closed logs the error of release, if any.
func (w *reloader) closed(ctx context.Context, err error) {
	if err != nil {
		w.log().WarnContext(ctx, "gonf: closing a source failed", "error", err)
	}
}

// report passes the result of a reload to the function given with
// OnReload.
func (w *reloader) report(cfg *Config, err error) {
	if w.onReload != nil {
		w.onReload(cfg, err)
	}
}
