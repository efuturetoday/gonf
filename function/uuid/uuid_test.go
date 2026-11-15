package uuid_test

import (
	"context"
	"regexp"
	"testing"

	"github.com/efuturetoday/gonf"
	"github.com/efuturetoday/gonf/function/uuid"
	"github.com/efuturetoday/gonf/source/memory"
)

var version4 = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

// One Config gives the same UUID per argument, another argument a new one.
func TestFunction(t *testing.T) {
	t.Parallel()
	cfg, err := gonf.Load(context.Background(), gonf.WithSources(memory.New(nil)), gonf.WithFunctions(uuid.Function()))
	if err != nil {
		t.Fatal(err)
	}
	a, err := cfg.Eval("${uuid::}")
	if err != nil {
		t.Fatal(err)
	}
	if !version4.MatchString(a) {
		t.Errorf("%q is no UUID of version 4", a)
	}
	if again, _ := cfg.Eval("${uuid::}"); again != a {
		t.Errorf("same argument: %q, then %q", a, again)
	}
	if other, _ := cfg.Eval("${uuid::b}"); other == a {
		t.Errorf("other argument gave the same UUID %q", a)
	}
}
