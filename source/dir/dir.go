// Package dir provides a gonf Source reading a directory with one file per
// key, as Kubernetes mounts a Secret or a ConfigMap: the file name is the
// key, the file content the value.
package dir

import (
	"bytes"
	"context"
	"encoding/gob"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/efuturetoday/gonf"
	"github.com/efuturetoday/gonf/source"
)

// Ordinal is the default ordinal of directory Sources.
const Ordinal = source.DefaultOrdinal

// MaxFileSize is the largest file a directory Source reads. A larger file
// is an error.
const MaxFileSize = 1 << 20

// New returns a Source reading the directory at path. Its name is
// "dir:" + path.
//
// Every regular file directly in the directory is a key: the file name is
// the key, e.g. "db.password", and the content, exactly as written, the
// value. A newline at the end of the content stays; see TrimNewline.
//
// Names starting with ".." are ignored, as are subdirectories. Kubernetes
// fills a mounted directory with symbolic links such as
// "db.password" -> "..data/db.password", where "..data" points to a
// directory like "..2026_01_02_03_04_05.123". Symbolic links are followed,
// but only inside the directory: a link pointing outside, a link that
// leads nowhere and a file larger than MaxFileSize are errors naming the
// file.
//
// With gonf.Watch the Source notices when a file changes, appears or
// disappears, and when Kubernetes swaps "..data" for a new directory.
// With source.Optional a missing directory yields no values.
func New(path string, opts ...source.Option) gonf.Source {
	opts = append([]source.Option{source.WithOrdinal(Ordinal)}, opts...)
	_, trim := source.NewMeta("", 0, opts...).Extension(trimKey{})
	return source.New("dir:"+path, reader(path, trim), format{}, opts...)
}

type trimKey struct{}

// TrimNewline is an Option for New. It removes one newline ("\n" or
// "\r\n") from the end of every value, so that a file written with echo
// holds the value without the newline. Further newlines and other white
// space stay.
func TrimNewline() source.Option {
	return source.Extension(trimKey{}, true)
}

type entry struct {
	Name string
	Data []byte
}

// reader returns a ReadFunc that reads all files of the directory and
// encodes them as one document, which format decodes. The change detection
// of source.New compares the document, so any changed file is a change.
func reader(path string, trim bool) source.ReadFunc {
	return func(context.Context) ([]byte, error) {
		root, err := os.OpenRoot(path)
		if err != nil {
			return nil, err
		}
		defer root.Close()

		d, err := root.Open(".")
		if err != nil {
			return nil, err
		}
		entries, err := d.ReadDir(-1)
		d.Close()
		if err != nil {
			return nil, err
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

		files := []entry{}
		for _, e := range entries {
			name := e.Name()
			if strings.HasPrefix(name, "..") {
				continue
			}
			data, ok, err := readFile(root, name)
			if err != nil {
				return nil, fmt.Errorf("dir %s: file %s: %w", path, name, err)
			}
			if !ok {
				continue
			}
			if trim {
				data = trimNewline(data)
			}
			files = append(files, entry{name, data})
		}
		var buf bytes.Buffer
		if err := gob.NewEncoder(&buf).Encode(files); err != nil {
			return nil, err
		}
		return buf.Bytes(), nil
	}
}

// readFile reads name inside root, following symbolic links within root. ok
// is false for anything but a regular file, such as a subdirectory.
func readFile(root *os.Root, name string) (data []byte, ok bool, err error) {
	f, err := root.Open(name)
	if err != nil {
		return nil, false, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, false, err
	}
	if !info.Mode().IsRegular() {
		return nil, false, nil
	}
	data, err = io.ReadAll(io.LimitReader(f, MaxFileSize+1))
	if err != nil {
		return nil, false, err
	}
	if len(data) > MaxFileSize {
		return nil, false, fmt.Errorf("larger than %d bytes", MaxFileSize)
	}
	return data, true, nil
}

func trimNewline(data []byte) []byte {
	if d, ok := bytes.CutSuffix(data, []byte("\r\n")); ok {
		return d
	}
	return bytes.TrimSuffix(data, []byte("\n"))
}

// format decodes the document made by reader.
type format struct{}

func (format) Parse(name string, data []byte) (map[string]gonf.Value, error) {
	var files []entry
	if err := gob.NewDecoder(bytes.NewReader(data)).Decode(&files); err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	values := make(map[string]gonf.Value, len(files))
	for _, f := range files {
		values[f.Name] = gonf.Value{
			Raw:    string(f.Data),
			Origin: gonf.Origin{Location: f.Name},
		}
	}
	return values, nil
}
