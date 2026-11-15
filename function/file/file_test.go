package file_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/efuturetoday/gonf"
	"github.com/efuturetoday/gonf/function/file"
	"github.com/efuturetoday/gonf/source/memory"
)

func eval(t *testing.T, expr string) (string, error) {
	t.Helper()
	cfg, err := gonf.Load(context.Background(), gonf.WithSources(memory.New(nil)), gonf.WithFunctions(file.Function()))
	if err != nil {
		t.Fatal(err)
	}
	return cfg.Eval(expr)
}

// The content comes as is, with its trailing newline.
func TestRead(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "password")
	if err := os.WriteFile(path, []byte("s3cret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := eval(t, "${file::"+path+"}"); err != nil || got != "s3cret\n" {
		t.Errorf("= %q, %v, want s3cret and a newline", got, err)
	}
}

// Errors name the path, never the content.
func TestErrors(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	large := filepath.Join(dir, "large")
	if err := os.WriteFile(large, []byte(strings.Repeat("x", file.MaxSize+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, tt := range map[string]struct{ path, message string }{
		"empty path": {"", "empty path"},
		"missing":    {filepath.Join(dir, "missing"), "missing"},
		"directory":  {dir, dir},
		"too large":  {large, "larger than"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := eval(t, "${file::"+tt.path+"}")
			if err == nil || !strings.Contains(err.Error(), tt.message) {
				t.Errorf("error %v, want one with %q", err, tt.message)
			}
			if err != nil && strings.Contains(err.Error(), "xxxx") {
				t.Errorf("error shows the content")
			}
		})
	}
}
