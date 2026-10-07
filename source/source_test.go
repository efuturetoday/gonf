package source_test

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"testing"

	"github.com/efuturetoday/gonf"
	"github.com/efuturetoday/gonf/source"
)

// keyValue is a minimal "k=v" per line Format, so these tests do not
// depend on the format packages.
var keyValue = source.FormatFunc(func(name string, data []byte) (map[string]gonf.Value, error) {
	out := map[string]gonf.Value{}
	for i, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("%s:%d: missing '='", name, i+1)
		}
		out[k] = gonf.Value{Raw: v, Origin: gonf.Origin{Location: fmt.Sprintf("%s:%d", name, i+1)}}
	}
	return out, nil
})

func TestFormatFunc(t *testing.T) {
	t.Parallel()
	got, err := keyValue.Parse("x", []byte("a=1"))
	if err != nil {
		t.Fatal(err)
	}
	if got["a"].Raw != "1" {
		t.Errorf("a = %q", got["a"].Raw)
	}
}

func TestNew(t *testing.T) {
	t.Parallel()
	src := source.New("s3:cfg", static("a=remote"), keyValue)
	if src.Name() != "s3:cfg" || src.Ordinal() != source.DefaultOrdinal {
		t.Errorf("name/ordinal = %q/%d", src.Name(), src.Ordinal())
	}
	got, err := src.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got["a"].Raw != "remote" || got["a"].Origin.Location != "s3:cfg:1" {
		t.Errorf("a = %+v", got["a"])
	}
}

func TestNew_Optional(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		readErr     error
		expectedErr error
	}{
		{name: "wrapped not exist is ignored", readErr: fmt.Errorf("remote: %w", fs.ErrNotExist)},
		{name: "permission error is reported", readErr: fs.ErrPermission, expectedErr: fs.ErrPermission},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			read := func(context.Context) ([]byte, error) { return nil, tt.readErr }
			got, err := source.New("x", read, keyValue, source.Optional()).Load(context.Background())
			if !errors.Is(err, tt.expectedErr) {
				t.Fatalf("err = %v, want %v", err, tt.expectedErr)
			}
			if err == nil && len(got) != 0 {
				t.Errorf("got %v, want empty", got)
			}
		})
	}
}

func TestNew_ParseError(t *testing.T) {
	t.Parallel()
	if _, err := source.New("x:bad", static("no-equals"), keyValue).Load(context.Background()); err == nil {
		t.Error("expected parse error")
	}
}

func TestNew_StripsBOM(t *testing.T) {
	t.Parallel()
	got, err := source.New("x:bom", static("\xEF\xBB\xBFdb.url=jdbc:x"), keyValue).Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got["db.url"].Raw != "jdbc:x" {
		t.Errorf("db.url = %q", got["db.url"].Raw)
	}
}

// static returns a ReadFunc delivering content.
func static(content string) source.ReadFunc {
	return func(context.Context) ([]byte, error) { return []byte(content), nil }
}
