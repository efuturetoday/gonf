package base64_test

import (
	"context"
	"strings"
	"testing"

	"github.com/efuturetoday/gonf"
	"github.com/efuturetoday/gonf/function/base64"
	"github.com/efuturetoday/gonf/source/memory"
)

func load(t *testing.T) *gonf.Config {
	t.Helper()
	cfg, err := gonf.Load(context.Background(), gonf.WithSources(memory.New(nil)),
		gonf.WithFunctions(base64.Encode(), base64.Decode()))
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestRoundTrip(t *testing.T) {
	t.Parallel()
	cfg := load(t)
	if got, err := cfg.Eval("${base64::s3cret}"); err != nil || got != "czNjcmV0" {
		t.Errorf("encode = %q, %v, want czNjcmV0", got, err)
	}
	if got, err := cfg.Eval("${base64-decode::czNjcmV0}"); err != nil || got != "s3cret" {
		t.Errorf("decode = %q, %v, want s3cret", got, err)
	}
}

// Invalid input is an error that does not show the input, as it may be a
// secret.
func TestDecodeInvalid(t *testing.T) {
	t.Parallel()
	_, err := load(t).Eval("${base64-decode::not*base64}")
	if err == nil || !strings.Contains(err.Error(), "not standard base64") {
		t.Fatalf("error %v, want not standard base64", err)
	}
	if strings.Contains(err.Error(), "not*base64") {
		t.Errorf("error %q shows the input", err)
	}
}
