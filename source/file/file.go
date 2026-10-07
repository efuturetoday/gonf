// Package file provides gonf Sources reading config documents from the
// file system or from an fs.FS such as embed.FS.
package file

import (
	"context"
	"io/fs"
	"os"

	"github.com/efuturetoday/gonf"
	"github.com/efuturetoday/gonf/source"
)

// Ordinal is the default ordinal of file Sources.
const Ordinal = source.DefaultOrdinal

// New returns a Source reading the file at path in the given format.
// Its name is "file:" + path.
func New(path string, format source.Format, opts ...source.Option) gonf.Source {
	opts = append([]source.Option{source.WithOrdinal(Ordinal)}, opts...)
	return source.New("file:"+path, reader(path), format, opts...)
}

// FS returns a Source reading path from fsys, e.g. an embed.FS.
// Its name is "fs:" + path.
func FS(fsys fs.FS, path string, format source.Format, opts ...source.Option) gonf.Source {
	opts = append([]source.Option{source.WithOrdinal(Ordinal)}, opts...)
	return source.New("fs:"+path, fsReader(fsys, path), format, opts...)
}

// reader returns a ReadFunc that reads the file at path when called.
func reader(path string) source.ReadFunc {
	return func(context.Context) ([]byte, error) { return os.ReadFile(path) }
}

// fsReader returns a ReadFunc that reads path from fsys when called.
func fsReader(fsys fs.FS, path string) source.ReadFunc {
	return func(context.Context) ([]byte, error) { return fs.ReadFile(fsys, path) }
}
