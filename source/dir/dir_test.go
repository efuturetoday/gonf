package dir_test

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/efuturetoday/gonf"
	"github.com/efuturetoday/gonf/source"
	"github.com/efuturetoday/gonf/source/dir"
	"github.com/efuturetoday/gonf/source/sourcetest"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func symlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
}

func raws(t *testing.T, src gonf.Source) map[string]string {
	t.Helper()
	got, err := src.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for k, v := range got {
		out[k] = v.Raw
	}
	return out
}

func equal(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if w, ok := b[k]; !ok || v != w {
			return false
		}
	}
	return true
}

func TestNew_Files(t *testing.T) {
	t.Parallel()
	d := t.TempDir()
	write(t, filepath.Join(d, "db.password"), "s3cret\n")
	write(t, filepath.Join(d, "db.user"), "app")
	write(t, filepath.Join(d, "sub", "ignored"), "x")
	write(t, filepath.Join(d, "..hidden"), "x")

	src := dir.New(d)
	if got, want := src.Name(), "dir:"+d; got != want {
		t.Errorf("Name() = %q, want %q", got, want)
	}
	if src.Ordinal() != dir.Ordinal {
		t.Errorf("Ordinal() = %d, want %d", src.Ordinal(), dir.Ordinal)
	}
	want := map[string]string{"db.password": "s3cret\n", "db.user": "app"}
	if got := raws(t, src); !equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
	v, _ := src.Load(context.Background())
	if loc := v["db.user"].Origin.Location; loc != "db.user" {
		t.Errorf("Location = %q, want db.user", loc)
	}
}

func TestNew_Empty(t *testing.T) {
	t.Parallel()
	if got := raws(t, dir.New(t.TempDir())); len(got) != 0 {
		t.Errorf("got %q, want no keys", got)
	}
}

func TestNew_TrimNewline(t *testing.T) {
	t.Parallel()
	tests := []struct{ name, content, want string }{
		{"none", "a", "a"},
		{"lf", "a\n", "a"},
		{"crlf", "a\r\n", "a"},
		{"two", "a\n\n", "a\n"},
		{"space", "a \n", "a "},
		{"only", "\n", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			d := t.TempDir()
			write(t, filepath.Join(d, "k"), tt.content)
			if got := raws(t, dir.New(d, dir.TrimNewline()))["k"]; got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// k8sLayout builds what Kubernetes mounts: visible symlinks into ..data,
// which is a symlink to a timestamped directory.
func k8sLayout(t *testing.T, d, version string, files map[string]string) {
	t.Helper()
	write(t, filepath.Join(d, version, ".keep"), "")
	for name, content := range files {
		write(t, filepath.Join(d, version, name), content)
	}
}

func TestNew_Kubernetes(t *testing.T) {
	t.Parallel()
	d := t.TempDir()
	k8sLayout(t, d, "..2026_01", map[string]string{"db.password": "one"})
	symlink(t, "..2026_01", filepath.Join(d, "..data"))
	symlink(t, filepath.Join("..data", "db.password"), filepath.Join(d, "db.password"))

	want := map[string]string{"db.password": "one"}
	if got := raws(t, dir.New(d)); !equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestNew_LinkErrors(t *testing.T) {
	t.Parallel()
	outside := t.TempDir()
	write(t, filepath.Join(outside, "secret"), "x")
	tests := []struct {
		name   string
		target string
	}{
		{"escapes", filepath.Join(outside, "secret")},
		{"relative escape", filepath.Join("..", filepath.Base(outside), "secret")},
		{"broken", "nowhere"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			d := t.TempDir()
			symlink(t, tt.target, filepath.Join(d, "link"))
			_, err := dir.New(d).Load(context.Background())
			if err == nil || !strings.Contains(err.Error(), "link") {
				t.Errorf("err = %v, want an error naming link", err)
			}
		})
	}
}

func TestNew_SizeLimit(t *testing.T) {
	t.Parallel()
	d := t.TempDir()
	write(t, filepath.Join(d, "ok"), strings.Repeat("a", dir.MaxFileSize))
	if got := raws(t, dir.New(d)); len(got["ok"]) != dir.MaxFileSize {
		t.Fatalf("file of exactly MaxFileSize: got %d bytes", len(got["ok"]))
	}
	write(t, filepath.Join(d, "big.key"), strings.Repeat("a", dir.MaxFileSize+1))
	_, err := dir.New(d).Load(context.Background())
	if err == nil || !strings.Contains(err.Error(), "big.key") {
		t.Errorf("err = %v, want an error naming big.key", err)
	}
}

func TestNew_Missing(t *testing.T) {
	t.Parallel()
	missing := filepath.Join(t.TempDir(), "none")
	if _, err := dir.New(missing).Load(context.Background()); err == nil {
		t.Error("missing directory: want an error")
	}
	got, err := dir.New(missing, source.Optional()).Load(context.Background())
	if err != nil || len(got) != 0 {
		t.Errorf("optional missing: got %v, %v; want empty, nil", got, err)
	}
}

func TestNew_NotADirectory(t *testing.T) {
	t.Parallel()
	f := filepath.Join(t.TempDir(), "file")
	write(t, f, "x")
	if _, err := dir.New(f, source.Optional()).Load(context.Background()); err == nil {
		t.Error("a file instead of a directory: want an error")
	}
}

func TestNew_Options(t *testing.T) {
	t.Parallel()
	d := t.TempDir()
	write(t, filepath.Join(d, "k"), "v")
	src := dir.New(d, source.WithOrdinal(7), source.WithProfile("dev"))
	if src.Ordinal() != 7 {
		t.Errorf("Ordinal() = %d, want 7", src.Ordinal())
	}
	if got := raws(t, src); !equal(got, map[string]string{"%dev.k": "v"}) {
		t.Errorf("got %q, want %%dev.k", got)
	}
}

func TestNew_WatchSwap(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		d := t.TempDir()
		k8sLayout(t, d, "..v1", map[string]string{"api-key": "one"})
		symlink(t, "..v1", filepath.Join(d, "..data"))
		symlink(t, filepath.Join("..data", "api-key"), filepath.Join(d, "api-key"))

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		reloads := make(chan *gonf.Config, 10)
		_, err := gonf.Watch(ctx,
			gonf.WithSources(dir.New(d)),
			gonf.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))),
			gonf.OnReload(func(cfg *gonf.Config, err error) {
				if err == nil {
					reloads <- cfg
				}
			}),
		)
		if err != nil {
			t.Fatal(err)
		}

		// Swap ..data atomically, as the kubelet does: new link, then rename.
		k8sLayout(t, d, "..v2", map[string]string{"api-key": "two"})
		symlink(t, "..v2", filepath.Join(d, "..data_tmp"))
		if err := os.Rename(filepath.Join(d, "..data_tmp"), filepath.Join(d, "..data")); err != nil {
			t.Fatal(err)
		}

		select {
		case cfg := <-reloads:
			if v, _ := cfg.Get("api-key"); v != "two" {
				t.Errorf("api-key = %q, want two", v)
			}
		case <-time.After(time.Minute):
			t.Fatal("swap of ..data not noticed")
		}
	})
}

func TestContract(t *testing.T) {
	t.Parallel()
	path := t.TempDir()
	write(t, filepath.Join(path, "db.url"), "postgres://db")
	sourcetest.Run(t, dir.New(path), map[string]string{"db.url": "postgres://db"})
}
