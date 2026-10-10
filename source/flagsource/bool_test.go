package flagsource_test

import (
	"context"
	"flag"
	"testing"

	"github.com/efuturetoday/gonf"
	"github.com/efuturetoday/gonf/source/flagsource"
)

func TestDefine_BoolFlag(t *testing.T) {
	t.Parallel()
	type cfg struct {
		Debug bool `default:"false"`
		Trace *bool
		Name  string
	}
	props, err := gonf.Describe(gonf.WithMappings(gonf.NewMapping[cfg]("app")))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		args []string
		want map[string]string
	}{
		{"bare", []string{"-app.debug", "-app.trace"}, map[string]string{"app.debug": "true", "app.trace": "true"}},
		{"explicit", []string{"-app.debug=no", "-app.name", "x"}, map[string]string{"app.debug": "no", "app.name": "x"}},
		{"unset", nil, map[string]string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fs := flag.NewFlagSet("app", flag.ContinueOnError)
			if err := flagsource.Define(fs, props); err != nil {
				t.Fatal(err)
			}
			if err := fs.Parse(tt.args); err != nil {
				t.Fatal(err)
			}
			got, err := flagsource.New(fs).Load(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != len(tt.want) {
				t.Errorf("keys = %v, want %v", got, tt.want)
			}
			for k, want := range tt.want {
				if got[k].Raw != want {
					t.Errorf("%s = %q, want %q", k, got[k].Raw, want)
				}
			}
		})
	}
}
