package sourcetest_test

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"testing"

	"github.com/efuturetoday/gonf"
	"github.com/efuturetoday/gonf/source/memory"
	"github.com/efuturetoday/gonf/source/sourcetest"
)

func TestRunPasses(t *testing.T) {
	t.Parallel()
	sourcetest.Run(t, memory.New(map[string]string{"db.url": "postgres://db", "%dev.db.url": "postgres://dev"}),
		map[string]string{"db.url": "postgres://db", "%dev.db.url": "postgres://dev"})
}

// A Source that breaks the contract fails Run with a message naming the
// rule.
func TestRunFails(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		src      gonf.Source
		expected map[string]string
		message  string
	}{
		{name: "no scheme", src: &broken{name: "vault", values: map[string]string{"a": "1"}}, expected: map[string]string{"a": "1"}, message: "want a scheme"},
		{name: "other values", src: &broken{name: "x:", values: map[string]string{"a": "1"}}, expected: map[string]string{"a": "2"}, message: "Load ="},
		{name: "empty key", src: &broken{name: "x:", values: map[string]string{"": "1"}}, expected: map[string]string{"": "1"}, message: "empty key"},
		{name: "invalid profile key", src: &broken{name: "x:", values: map[string]string{"%dev": "1"}}, expected: map[string]string{"%dev": "1"}, message: "invalid profile key"},
		{name: "shared map", src: &broken{name: "x:", values: map[string]string{"a": "1"}, shared: true}, expected: map[string]string{"a": "1"}, message: "second Load"},
		{name: "missing after values", src: &broken{name: "x:", values: map[string]string{"a": "1"}, missing: true}, expected: map[string]string{"a": "1"}, message: "IsMissing"},
		{name: "watch ignores context", src: &broken{name: "x:", values: map[string]string{"a": "1"}, watchErr: true}, expected: map[string]string{"a": "1"}, message: "Watch returned"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := &recorder{}
			done := make(chan struct{})
			go func() {
				defer close(done)
				sourcetest.Run(r, tt.src, tt.expected)
			}()
			<-done
			if !strings.Contains(strings.Join(r.messages, "\n"), tt.message) {
				t.Errorf("messages %q, want one with %q", r.messages, tt.message)
			}
		})
	}
}

// recorder records what Run reports; Fatalf ends the goroutine of Run, as
// testing.T does.
type recorder struct{ messages []string }

func (r *recorder) Helper() {}

func (r *recorder) Errorf(format string, args ...any) {
	r.messages = append(r.messages, fmt.Sprintf(format, args...))
}

func (r *recorder) Fatalf(format string, args ...any) {
	r.Errorf(format, args...)
	runtime.Goexit()
}

// broken is a Source that breaks one rule of the contract at a time.
type broken struct {
	name     string
	values   map[string]string
	shared   bool // Load returns the same map every time
	missing  bool // IsMissing is always true
	watchErr bool // Watch returns an error when its context ends
	last     map[string]gonf.Value
}

func (b *broken) Name() string { return b.name }
func (b *broken) Ordinal() int { return 100 }

func (b *broken) Load(context.Context) (map[string]gonf.Value, error) {
	if b.shared && b.last != nil {
		return b.last, nil
	}
	out := map[string]gonf.Value{}
	for k, v := range b.values {
		out[k] = gonf.Value{Raw: v}
	}
	b.last = out
	return out, nil
}

func (b *broken) IsOptional() bool { return true }
func (b *broken) IsMissing() bool  { return b.missing }

func (b *broken) Watch(ctx context.Context, _ func()) error {
	<-ctx.Done()
	if b.watchErr {
		return ctx.Err()
	}
	return nil
}
