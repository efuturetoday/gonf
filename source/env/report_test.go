package env_test

import (
	"context"
	"slices"
	"testing"

	"github.com/efuturetoday/gonf"
	"github.com/efuturetoday/gonf/source/env"
)

// Not parallel: t.Setenv modifies the process environment.
func TestEnvSource_LoadReport(t *testing.T) {
	t.Setenv("GONFR_DB_URL", "u")
	t.Setenv("GONFR_DB_PORT", "1")
	t.Setenv("GONFR_DB_POOLS_MAIN_URL", "p")
	t.Setenv("_DEV_GONFR_DB_HOST", "h")
	t.Setenv("GONFR_DBX", "x")
	t.Setenv("GONFR_CACHE_URL", "c")

	reporter, ok := env.New().(gonf.NameReporter)
	if !ok {
		t.Fatal("env Source does not implement gonf.NameReporter")
	}
	tests := []struct {
		name            string
		keys, prefixes  []string
		expectedUnknown []string
		expectedKeys    []string
	}{
		{
			name:            "names under the prefix without a key",
			keys:            []string{"gonfr.db.url", "gonfr.db.pools.<name>.url"},
			prefixes:        []string{"gonfr.db"},
			expectedUnknown: []string{"GONFR_DB_PORT", "_DEV_GONFR_DB_HOST"},
			expectedKeys:    []string{"gonfr.db.url", "gonfr.db.pools.main.url"},
		},
		{
			name:            "several prefixes",
			keys:            []string{"gonfr.db.url"},
			prefixes:        []string{"gonfr.db", "gonfr.cache"},
			expectedUnknown: []string{"GONFR_CACHE_URL", "GONFR_DB_POOLS_MAIN_URL", "GONFR_DB_PORT", "_DEV_GONFR_DB_HOST"},
			expectedKeys:    []string{"gonfr.db.url"},
		},
		{
			name:         "no prefix, no unknown",
			keys:         []string{"gonfr.db.url"},
			expectedKeys: []string{"gonfr.db.url"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			values, unknown, err := reporter.LoadReport(context.Background(), tt.keys, tt.prefixes)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(unknown, tt.expectedUnknown) {
				t.Errorf("unknown = %v, want %v", unknown, tt.expectedUnknown)
			}
			for _, key := range tt.expectedKeys {
				if _, ok := values[key]; !ok {
					t.Errorf("values lack %q: %v", key, values)
				}
			}
		})
	}
}
