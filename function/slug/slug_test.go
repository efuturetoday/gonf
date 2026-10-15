package slug_test

import (
	"context"
	"strings"
	"testing"

	"github.com/efuturetoday/gonf"
	"github.com/efuturetoday/gonf/function/slug"
	"github.com/efuturetoday/gonf/source/memory"
)

func TestMake(t *testing.T) {
	t.Parallel()
	for in, expected := range map[string]string{
		"feature/Login #12":       "feature-login-12",
		"main":                    "main",
		"--Fix__the.bug--":        "fix-the-bug",
		"Ünïcode":                 "n-code",
		"":                        "",
		strings.Repeat("ab-", 30): strings.TrimRight(strings.Repeat("ab-", 21), "-"),
	} {
		if got := slug.Make(in); got != expected {
			t.Errorf("Make(%q) = %q, want %q", in, got, expected)
		}
	}
}

// ${slug::...} makes a value fit for a name; an empty slug is an error.
func TestFunction(t *testing.T) {
	t.Parallel()
	cfg, err := gonf.Load(context.Background(),
		gonf.WithSources(memory.New(map[string]string{"branch": "feature/Login"})),
		gonf.WithFunctions(slug.Function()),
	)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := cfg.Eval("${slug::${branch}}-1"); err != nil || got != "feature-login-1" {
		t.Errorf("= %q, %v, want feature-login-1", got, err)
	}
	if _, err := cfg.Eval("${slug::--}"); err == nil || !strings.Contains(err.Error(), "no letter or digit") {
		t.Errorf("empty slug: error %v", err)
	}
}
