package boot

import (
	"flag"
	"testing"

	"github.com/efuturetoday/gonf"
)

// Not parallel: t.Setenv modifies the process environment.
func TestWithFlags(t *testing.T) {
	t.Setenv("BOOTFLAGTEST_URL", "env")
	dir := workDir(t, map[string]string{
		"config/app.properties": "bootflagtest.url=file\nbootflagtest.other=file\n",
		".env":                  "BOOTFLAGTEST_URL=dotenv\n",
	})
	tests := []struct {
		name     string
		args     []string
		expected string
	}{
		{name: "flag wins over env, .env and files", args: []string{"-bootflagtest.url=flag"}, expected: "flag"},
		{name: "unset flag changes nothing", args: nil, expected: "env"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := flag.NewFlagSet("app", flag.ContinueOnError)
			fs.String("bootflagtest.url", "default", "")
			if err := fs.Parse(tt.args); err != nil {
				t.Fatal(err)
			}
			m := gonf.NewMapping[struct{ URL, Other string }]("bootflagtest")
			cfg, err := load(dir, WithFlags(fs), WithMappings(m))
			if err != nil {
				t.Fatal(err)
			}
			if got := m.Get(cfg); got.URL != tt.expected || got.Other != "file" {
				t.Errorf("config = %+v, want url %q", got, tt.expected)
			}
		})
	}
}
