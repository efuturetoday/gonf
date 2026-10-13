package gonf_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/efuturetoday/gonf"
)

// liveSource is a Watcher whose values tests change with set.
type liveSource struct {
	mu       sync.Mutex
	values   map[string]string
	loads    int
	changed  func()
	contexts []context.Context
	watching int
	watchErr error
	closes   int
	// started receives a value whenever Watch starts.
	started chan struct{}
}

func newLiveSource(values map[string]string) *liveSource {
	return &liveSource{values: values, started: make(chan struct{}, 10)}
}

func (s *liveSource) Name() string { return "test:live" }
func (s *liveSource) Ordinal() int { return 100 }

func (s *liveSource) Load(context.Context) (map[string]gonf.Value, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loads++
	out := make(map[string]gonf.Value, len(s.values))
	for k, v := range s.values {
		out[k] = gonf.Value{Raw: v}
	}
	return out, nil
}

func (s *liveSource) Watch(ctx context.Context, changed func()) error {
	s.mu.Lock()
	s.changed = changed
	s.contexts = append(s.contexts, ctx)
	s.watching++
	err := s.watchErr
	s.mu.Unlock()
	s.started <- struct{}{}
	defer func() {
		s.mu.Lock()
		s.watching--
		s.mu.Unlock()
	}()
	if err != nil {
		return err
	}
	<-ctx.Done()
	// In the synctest bubble this sleep takes no real time; it makes
	// Watch's wait for stopped Watchers observable.
	time.Sleep(time.Millisecond)
	return nil
}

// Close counts how often the Source was closed.
func (s *liveSource) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closes++
	return errors.New("close is ignored")
}

func (s *liveSource) closed() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closes
}

// set changes a value and reports the change.
func (s *liveSource) set(key, value string) {
	s.mu.Lock()
	s.values[key] = value
	changed := s.changed
	s.mu.Unlock()
	changed()
}

func (s *liveSource) state() (loads, watching int, contexts []context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loads, s.watching, s.contexts
}

// discard drops all log records.
var discard = slog.New(slog.NewTextHandler(io.Discard, nil))

// records collects log records as "LEVEL message: error".
type records struct {
	mu    sync.Mutex
	lines []string
}

func (r *records) Enabled(context.Context, slog.Level) bool { return true }
func (r *records) WithAttrs([]slog.Attr) slog.Handler       { return r }
func (r *records) WithGroup(string) slog.Handler            { return r }
func (r *records) Handle(_ context.Context, rec slog.Record) error {
	line := rec.Level.String() + " " + rec.Message
	rec.Attrs(func(a slog.Attr) bool {
		line += ": " + a.Value.String()
		return true
	})
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lines = append(r.lines, line)
	return nil
}

func (r *records) all() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.lines)
}

type reload struct {
	cfg *gonf.Config
	err error
}

type portConfig struct {
	Port int
}

// startWatch runs gonf.Watch until the test ends and returns the results of
// all reloads.
func startWatch(t *testing.T, opts ...gonf.WatchOption) (*gonf.Live, <-chan reload) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	reloads := make(chan reload, 10)
	opts = append([]gonf.WatchOption{gonf.WithLogger(discard)}, opts...)
	opts = append(opts, gonf.OnReload(func(cfg *gonf.Config, err error) {
		reloads <- reload{cfg: cfg, err: err}
	}))
	live, err := gonf.Watch(ctx, opts...)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cancel()
		receive(t, gonf.LiveDone(live))
	})
	return live, reloads
}

// receive returns the next value of ch or fails after a while.
func receive[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
		var zero T
		return zero
	}
}

func TestWatch(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		src := newLiveSource(map[string]string{"app.port": "80"})
		app := gonf.NewMapping[portConfig]("app")
		live, reloads := startWatch(t, gonf.WithSources(src), gonf.WithMappings(app))
		first := live.Config()
		receive(t, src.started)

		src.set("app.port", "81")
		r := receive(t, reloads)
		if r.err != nil {
			t.Fatal(r.err)
		}
		if live.Config() != r.cfg {
			t.Error("Live.Config is not the reloaded Config")
		}
		if got := app.Get(live.Config()).Port; got != 81 {
			t.Errorf("port = %d, want 81", got)
		}
		if got := app.Get(first).Port; got != 80 {
			t.Errorf("previous Config changed: port = %d, want 80", got)
		}
		receive(t, src.started)
		_, _, contexts := src.state()
		if len(contexts) != 2 || contexts[0].Err() == nil {
			t.Errorf("the Watcher of the previous Config should be stopped and a new one started, got %d", len(contexts))
		}
	})
}

func TestWatch_KeepsConfigOnError(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		src := newLiveSource(map[string]string{"app.port": "80"})
		app := gonf.NewMapping[portConfig]("app")
		live, reloads := startWatch(t, gonf.WithSources(src), gonf.WithMappings(app))
		receive(t, src.started)

		src.set("app.port", "eighty")
		r := receive(t, reloads)
		var bindErr *gonf.BindError
		if r.cfg != nil || !errors.As(r.err, &bindErr) {
			t.Fatalf("reload = %v, %v; want nil and a BindError", r.cfg, r.err)
		}
		if got := app.Get(live.Config()).Port; got != 80 {
			t.Errorf("port = %d, want the previous 80", got)
		}

		src.set("app.port", "81")
		if r := receive(t, reloads); r.err != nil {
			t.Fatalf("reload after the fix: %v", r.err)
		}
		if got := app.Get(live.Config()).Port; got != 81 {
			t.Errorf("port = %d, want 81", got)
		}
	})
}

func TestWatch_MergesChanges(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		src := newLiveSource(map[string]string{"a": "1"})
		_, reloads := startWatch(t, gonf.WithSources(src))
		receive(t, src.started)

		// Changes a little apart, as an editor writes a file in several steps.
		src.set("a", "2")
		time.Sleep(30 * time.Millisecond)
		src.set("a", "3")
		time.Sleep(30 * time.Millisecond)
		src.set("a", "4")
		r := receive(t, reloads)
		if v, _ := r.cfg.Get("a"); v != "4" {
			t.Errorf("a = %q, want 4", v)
		}
		select {
		case r := <-reloads:
			t.Errorf("second reload %v, %v; want one reload for all changes", r.cfg, r.err)
		case <-time.After(3 * time.Second / 10):
		}
		if loads, _, _ := src.state(); loads != 2 {
			t.Errorf("loads = %d, want 2 (start and one reload)", loads)
		}
	})
}

func TestWatch_WatcherFails(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		src := newLiveSource(map[string]string{"a": "1"})
		src.watchErr = errors.New("connection lost")
		_, reloads := startWatch(t, gonf.WithSources(src))

		r := receive(t, reloads)
		var srcErr *gonf.SourceError
		if !errors.As(r.err, &srcErr) || srcErr.Source != "test:live" {
			t.Fatalf("error = %v, want a SourceError of test:live", r.err)
		}
		if want := `gonf: source "test:live": connection lost`; r.err.Error() != want {
			t.Errorf("error = %q, want %q", r.err, want)
		}
	})
}

func TestWatch_WatcherRetries(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		src := newLiveSource(map[string]string{"a": "1"})
		src.watchErr = errors.New("connection lost")
		_, reloads := startWatch(t, gonf.WithSources(src))
		receive(t, src.started)
		src.mu.Lock()
		src.watchErr = nil
		src.mu.Unlock()

		if r := receive(t, reloads); r.err == nil {
			t.Fatal("first result is no error")
		}
		receive(t, src.started) // watched again after the delay
		if r := receive(t, reloads); r.err != nil {
			t.Errorf("reload after the retry: %v", r.err)
		}
	})
}

func TestWatch_FactorySources(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		late := newLiveSource(map[string]string{"a": "1"})
		factory := factoryOf(func(context.Context) ([]gonf.Source, error) {
			return []gonf.Source{late}, nil
		})
		_, reloads := startWatch(t, gonf.WithFactories(factory))
		receive(t, late.started)

		late.set("a", "2")
		r := receive(t, reloads)
		if v, _ := r.cfg.Get("a"); v != "2" {
			t.Errorf("a = %q, want 2", v)
		}
	})
}

func TestWatch_FirstLoadFails(t *testing.T) {
	t.Parallel()
	app := gonf.NewMapping[portConfig]("app")
	live, err := gonf.Watch(context.Background(), gonf.WithMappings(app))
	if live != nil || err == nil {
		t.Errorf("Watch = %v, %v; want nil and the error of Load", live, err)
	}
}

func TestWatch_Stops(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		src := newLiveSource(map[string]string{"a": "1"})
		ctx, cancel := context.WithCancel(context.Background())
		live, err := gonf.Watch(ctx, gonf.WithSources(src), gonf.WithLogger(discard))
		if err != nil {
			t.Fatal(err)
		}
		receive(t, src.started)
		cancel()
		receive(t, gonf.LiveDone(live))
		if _, watching, _ := src.state(); watching != 0 {
			t.Errorf("%d Watchers still running", watching)
		}
		if v, _ := live.Config().Get("a"); v != "1" {
			t.Errorf("a = %q after stop, want 1", v)
		}
	})
}

// sourceFactory returns a factory that creates a new liveSource on every
// call and records them.
func sourceFactory() (gonf.SourceFactory, func() []*liveSource) {
	var mu sync.Mutex
	var created []*liveSource
	factory := factoryOf(func(context.Context) ([]gonf.Source, error) {
		mu.Lock()
		defer mu.Unlock()
		src := newLiveSource(map[string]string{"late": "1"})
		created = append(created, src)
		return []gonf.Source{src}, nil
	})
	return factory, func() []*liveSource {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(created)
	}
}

func TestWatch_ClosesFactorySources(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		src := newLiveSource(map[string]string{"app.port": "80"})
		factory, created := sourceFactory()
		app := gonf.NewMapping[portConfig]("app")
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		reloads := make(chan reload, 10)
		live, err := gonf.Watch(ctx,
			gonf.WithLogger(discard),
			gonf.WithSources(src),
			gonf.WithFactories(factory),
			gonf.WithMappings(app),
			gonf.OnReload(func(cfg *gonf.Config, err error) { reloads <- reload{cfg: cfg, err: err} }),
		)
		if err != nil {
			t.Fatal(err)
		}
		receive(t, src.started)
		closes := func() []int {
			var out []int
			for _, s := range created() {
				out = append(out, s.closed())
			}
			return out
		}

		src.set("app.port", "81")
		if r := receive(t, reloads); r.err != nil {
			t.Fatal(r.err)
		}
		if got := closes(); !slices.Equal(got, []int{1, 0}) {
			t.Errorf("after a reload: closes = %v, want the replaced Source closed", got)
		}

		src.set("app.port", "eighty")
		if r := receive(t, reloads); r.err == nil {
			t.Fatal("reload succeeded, want a BindError")
		}
		if got := closes(); !slices.Equal(got, []int{1, 0, 1}) {
			t.Errorf("after a failed reload: closes = %v, want the new Source closed", got)
		}

		cancel()
		receive(t, gonf.LiveDone(live))
		if got := closes(); !slices.Equal(got, []int{1, 1, 1}) {
			t.Errorf("after stop: closes = %v, want all closed", got)
		}
		if n := src.closed(); n != 0 {
			t.Errorf("a Source of WithSources was closed %d times", n)
		}
	})
}

func TestWatch_KeepsReturnedSourceOpen(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		src := newLiveSource(map[string]string{"app.port": "80"})
		kept := newLiveSource(map[string]string{"late": "1"})
		factory := factoryOf(func(context.Context) ([]gonf.Source, error) {
			return []gonf.Source{kept}, nil
		})
		app := gonf.NewMapping[portConfig]("app")
		_, reloads := startWatch(t, gonf.WithSources(src), gonf.WithFactories(factory), gonf.WithMappings(app))
		receive(t, src.started)

		src.set("app.port", "81")
		if r := receive(t, reloads); r.err != nil {
			t.Fatal(r.err)
		}
		if n := kept.closed(); n != 0 {
			t.Errorf("after a reload: a Source the factory returned again was closed %d times", n)
		}

		src.set("app.port", "eighty")
		if r := receive(t, reloads); r.err == nil {
			t.Fatal("reload succeeded, want a BindError")
		}
		if n := kept.closed(); n != 0 {
			t.Errorf("after a failed reload: the Source in use was closed %d times", n)
		}
	})
}

// mapSource is a Source of a type that == cannot compare.
type mapSource map[string]string

func (s mapSource) Name() string { return "test:map" }
func (s mapSource) Ordinal() int { return 100 }
func (s mapSource) Close() error { return nil }
func (s mapSource) Load(context.Context) (map[string]gonf.Value, error) {
	return map[string]gonf.Value{}, nil
}

func TestWatch_IncomparableSource(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		src := newLiveSource(map[string]string{"a": "1"})
		factory := factoryOf(func(context.Context) ([]gonf.Source, error) {
			return []gonf.Source{mapSource{}}, nil
		})
		_, reloads := startWatch(t, gonf.WithSources(src), gonf.WithFactories(factory))
		receive(t, src.started)

		src.set("a", "2")
		if r := receive(t, reloads); r.err != nil {
			t.Fatal(r.err)
		}
	})
}

func TestWithLogger(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		src := newLiveSource(map[string]string{"app.port": "80"})
		factory, _ := sourceFactory()
		app := gonf.NewMapping[portConfig]("app")
		var logged records
		_, reloads := startWatch(t,
			gonf.WithSources(src),
			gonf.WithFactories(factory),
			gonf.WithMappings(app),
			gonf.WithLogger(slog.New(&logged)),
		)
		receive(t, src.started)

		src.set("app.port", "81")
		receive(t, reloads)
		src.set("app.port", "eighty")
		receive(t, reloads)

		expected := []string{
			`WARN gonf: closing a source failed: gonf: source "test:live": close: close is ignored`,
			"INFO gonf: configuration reloaded",
			`WARN gonf: closing a source failed: gonf: source "test:live": close: close is ignored`,
			`WARN gonf: reload failed, keeping the previous configuration: gonf: bind "app": app.port: invalid int "eighty" (from test:live)`,
		}
		if got := logged.all(); !slices.Equal(got, expected) {
			t.Errorf("logged\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(expected, "\n"))
		}
	})
}

func TestWithLogger_WatcherFails(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		src := newLiveSource(map[string]string{"a": "1"})
		src.watchErr = errors.New("connection lost")
		var logged records
		_, reloads := startWatch(t, gonf.WithSources(src), gonf.WithLogger(slog.New(&logged)))

		receive(t, reloads)
		expected := []string{`WARN gonf: watching a source failed, retrying: gonf: source "test:live": connection lost`}
		if got := logged.all(); !slices.Equal(got, expected) {
			t.Errorf("logged %q, want %q", got, expected)
		}
	})
}
