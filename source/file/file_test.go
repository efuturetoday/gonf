package file_test

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/efuturetoday/gonf/format/json"
	"github.com/efuturetoday/gonf/format/properties"
	"github.com/efuturetoday/gonf/source"
	"github.com/efuturetoday/gonf/source/file"
)

func TestNew(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	write := func(name, content string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}

	tests := []struct {
		name        string
		path        string
		format      source.Format
		key         string
		expectedRaw string
	}{
		{name: "properties", path: write("app.properties", "db.url=postgres://x\n"), format: properties.Format, key: "db.url", expectedRaw: "postgres://x"},
		{name: "json", path: write("app.json", `{"a":{"b":1}}`), format: json.Format, key: "a.b", expectedRaw: "1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			src := file.New(tt.path, tt.format)
			if src.Name() != "file:"+tt.path || src.Ordinal() != file.Ordinal {
				t.Errorf("name/ordinal = %q/%d", src.Name(), src.Ordinal())
			}
			got, err := src.Load(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if got[tt.key].Raw != tt.expectedRaw {
				t.Errorf("%s = %q, want %q", tt.key, got[tt.key].Raw, tt.expectedRaw)
			}
		})
	}
}

func TestNew_Missing(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "missing.properties")

	if _, err := file.New(path, properties.Format).Load(context.Background()); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("required: err = %v, want ErrNotExist", err)
	}
	got, err := file.New(path, properties.Format, source.Optional()).Load(context.Background())
	if err != nil || len(got) != 0 {
		t.Errorf("optional: got %v, %v; want empty, nil", got, err)
	}
}

func TestNew_OptionsOverrideOrdinal(t *testing.T) {
	t.Parallel()
	if src := file.New("x.json", json.Format, source.WithOrdinal(7)); src.Ordinal() != 7 {
		t.Errorf("ordinal = %d, want 7", src.Ordinal())
	}
}

func TestFS(t *testing.T) {
	t.Parallel()
	fsys := fstest.MapFS{"config/app.json": {Data: []byte(`{"a":"from-fs"}`)}}

	src := file.FS(fsys, "config/app.json", json.Format)
	if src.Name() != "fs:config/app.json" || src.Ordinal() != file.Ordinal {
		t.Errorf("name/ordinal = %q/%d", src.Name(), src.Ordinal())
	}
	got, err := src.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got["a"].Raw != "from-fs" || got["a"].Origin.Location != "fs:config/app.json:1" {
		t.Errorf("a = %+v", got["a"])
	}

	missing := file.FS(fsys, "config/missing.json", json.Format, source.Optional())
	if got, err := missing.Load(context.Background()); err != nil || len(got) != 0 {
		t.Errorf("optional missing: got %v, %v; want empty, nil", got, err)
	}
}

func TestNew_ReadsOnLoad(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "late.properties")
	src := file.New(path, properties.Format) // must not read yet
	if err := os.WriteFile(path, []byte("a=late"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := src.Load(context.Background())
	if err != nil || got["a"].Raw != "late" {
		t.Errorf("got %v, %v; want a=late", got, err)
	}
}
