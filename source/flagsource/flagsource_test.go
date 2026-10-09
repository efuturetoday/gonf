package flagsource_test

import (
	"context"
	"flag"
	"io"
	"strings"
	"testing"

	"github.com/efuturetoday/gonf"
	"github.com/efuturetoday/gonf/source"
	"github.com/efuturetoday/gonf/source/flagsource"
	"github.com/efuturetoday/gonf/source/memory"
)

func TestNew(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		args     []string
		expected map[string]string
	}{
		{name: "set flags only", args: []string{"-db.url=x", "--db.pool.max-size", "5"}, expected: map[string]string{"db.url": "x", "db.pool.max-size": "5"}},
		{name: "unset flags give no key", args: nil, expected: map[string]string{}},
		{name: "flag set to empty counts", args: []string{"-db.user="}, expected: map[string]string{"db.user": ""}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fs := flag.NewFlagSet("app", flag.ContinueOnError)
			for _, n := range []string{"db.url", "db.pool.max-size", "db.user"} {
				fs.String(n, "default", "")
			}
			if err := fs.Parse(tt.args); err != nil {
				t.Fatal(err)
			}
			src := flagsource.New(fs)
			if src.Name() != "flag:" || src.Ordinal() != flagsource.Ordinal {
				t.Errorf("name/ordinal = %q/%d", src.Name(), src.Ordinal())
			}
			got, err := src.Load(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != len(tt.expected) {
				t.Errorf("keys = %v, want %v", got, tt.expected)
			}
			for k, want := range tt.expected {
				if got[k].Raw != want || got[k].Origin.Location != "flag:-"+k {
					t.Errorf("%s = %+v, want %q", k, got[k], want)
				}
			}
		})
	}
}

func TestNew_Options(t *testing.T) {
	t.Parallel()
	src := flagsource.New(flag.NewFlagSet("app", flag.ContinueOnError), source.WithOrdinal(7), source.WithName("flag:x"))
	if src.Name() != "flag:x" || src.Ordinal() != 7 {
		t.Errorf("name/ordinal = %q/%d", src.Name(), src.Ordinal())
	}
}

type testDB struct {
	URL      string `default:"localhost" desc:"Database address."`
	Password string `default:"hunter2" desc:"Login."`
	Pools    map[string]struct{ Size int }
	Port     int
}

func TestDefine(t *testing.T) {
	t.Parallel()
	props, err := gonf.Describe(gonf.WithMappings(gonf.NewMapping[testDB]("db")))
	if err != nil {
		t.Fatal(err)
	}
	fs := flag.NewFlagSet("app", flag.ContinueOnError)
	var out strings.Builder
	fs.SetOutput(&out)
	if err := flagsource.Define(fs, props); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name          string
		flag          string
		expectedUsage string
		expectedDef   string
		isDefined     bool
	}{
		{name: "usage and default", flag: "db.url", expectedUsage: "Database address.", expectedDef: "localhost", isDefined: true},
		{name: "secret has no default", flag: "db.password", expectedUsage: "Login. (secret)", isDefined: true},
		{name: "no default", flag: "db.port", isDefined: true},
		{name: "pattern skipped", flag: "db.pools.<name>.size"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := fs.Lookup(tt.flag)
			if (f != nil) != tt.isDefined {
				t.Fatalf("defined = %v, want %v", f != nil, tt.isDefined)
			}
			if f != nil && (f.Usage != tt.expectedUsage || f.DefValue != tt.expectedDef) {
				t.Errorf("usage/default = %q/%q, want %q/%q", f.Usage, f.DefValue, tt.expectedUsage, tt.expectedDef)
			}
		})
	}
}

func TestDefine_PrintDefaults(t *testing.T) {
	t.Parallel()
	props, err := gonf.Describe(gonf.WithMappings(gonf.NewMapping[testDB]("db")))
	if err != nil {
		t.Fatal(err)
	}
	fs := flag.NewFlagSet("app", flag.ContinueOnError)
	var out strings.Builder
	fs.SetOutput(&out)
	if err := flagsource.Define(fs, props); err != nil {
		t.Fatal(err)
	}
	fs.PrintDefaults()
	if strings.Contains(out.String(), "hunter2") || !strings.Contains(out.String(), "localhost") {
		t.Errorf("PrintDefaults = %q", out.String())
	}
}

func TestDefine_Duplicate(t *testing.T) {
	t.Parallel()
	fs := flag.NewFlagSet("app", flag.ContinueOnError)
	fs.String("a.b", "", "")
	err := flagsource.Define(fs, []gonf.Property{{Key: "a.c"}, {Key: "a.b"}})
	if err == nil || !strings.Contains(err.Error(), "-a.b") {
		t.Errorf("err = %v", err)
	}
	if fs.Lookup("a.c") != nil {
		t.Error("a.c defined despite error")
	}
}

func TestLoad_FlagsAndUnknownKeys(t *testing.T) {
	t.Parallel()
	m := gonf.NewMapping[struct{ URL string }]("db")
	fs := flag.NewFlagSet("app", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.String("db.url", "", "")
	fs.String("db.urll", "", "")
	if err := fs.Parse([]string{"-db.url=flag", "-db.urll=typo"}); err != nil {
		t.Fatal(err)
	}
	_, err := gonf.Load(context.Background(), gonf.WithMappings(m), gonf.WithSources(flagsource.New(fs)))
	if err == nil || !strings.Contains(err.Error(), "db.urll") || !strings.Contains(err.Error(), "unknown key") {
		t.Fatalf("err = %v, want unknown key db.urll", err)
	}

	fs = flag.NewFlagSet("app", flag.ContinueOnError)
	fs.String("db.url", "", "")
	if err := fs.Parse([]string{"-db.url=flag"}); err != nil {
		t.Fatal(err)
	}
	cfg, err := gonf.Load(context.Background(), gonf.WithMappings(m),
		gonf.WithSources(memory.New(map[string]string{"db.url": "mem"}, source.WithOrdinal(350)), flagsource.New(fs)))
	if err != nil {
		t.Fatal(err)
	}
	if got := m.Get(cfg).URL; got != "flag" {
		t.Errorf("url = %q, want flag", got)
	}
}
