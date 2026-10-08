package source_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/efuturetoday/gonf"
	"github.com/efuturetoday/gonf/source"
)

// content is a ReadFunc whose result tests change with set.
type content struct {
	mu   sync.Mutex
	data string
	err  error
}

func (c *content) read(context.Context) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return []byte(c.data), c.err
}

func (c *content) set(data string, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data, c.err = data, err
}

// manual returns a Trigger that checks when the test calls check, which
// returns once the check is done.
func manual() (source.Trigger, func()) {
	due := make(chan chan struct{})
	trigger := func(ctx context.Context, check func()) error {
		for {
			select {
			case <-ctx.Done():
				return nil
			case done := <-due:
				check()
				close(done)
			}
		}
	}
	return trigger, func() {
		done := make(chan struct{})
		due <- done
		<-done
	}
}

// watch runs the Watch of src until the test ends and returns how often it
// reported a change.
func watch(t *testing.T, src gonf.Source) *atomic.Int32 {
	t.Helper()
	w, ok := src.(gonf.Watcher)
	if !ok {
		t.Fatalf("%T is no gonf.Watcher", src)
	}
	var changes atomic.Int32
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		if err := w.Watch(ctx, func() { changes.Add(1) }); err != nil {
			t.Errorf("Watch: %v", err)
		}
	}()
	t.Cleanup(func() {
		cancel()
		<-stopped
	})
	return &changes
}

func TestEvery(t *testing.T) {
	t.Parallel()
	c := &content{data: "a=1"}
	src := source.New("test:doc", c.read, keyValue, source.WithTrigger(source.Every(5*time.Millisecond)))
	if _, err := src.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	changes := watch(t, src)

	c.set("a=2", nil)
	deadline := time.Now().Add(5 * time.Second)
	for changes.Load() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("no change reported")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestEvery_NotPositive(t *testing.T) {
	t.Parallel()
	err := source.Every(0)(context.Background(), func() {})
	if err == nil || err.Error() != "poll interval 0s is not positive" {
		t.Errorf("error = %v", err)
	}
}
