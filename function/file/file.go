// Package file provides the gonf.Function file, which reads a file, such as
// a secret mounted by a container platform:
//
//	db.password=${file::/run/secrets/db-password}
//
// The result is the content as is, also a trailing newline, and a secret.
package file

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/efuturetoday/gonf"
)

// Name is the name of Function in ${file::path}.
const Name = "file"

// MaxSize is the largest file Function reads, in bytes.
const MaxSize = 1 << 20

// Function returns the secret gonf.Function file. Its argument is the
// path of the file, relative to the working directory unless absolute. A
// missing file, a directory or a file larger than MaxSize is an error that
// names the path; the error never contains the content.
func Function() gonf.Function {
	return gonf.Function{Name: Name, IsSecret: true, Call: read}
}

func read(_ *gonf.Config, path string, _ gonf.Origin) (string, error) {
	if path == "" {
		return "", errors.New("empty path")
	}
	f, err := os.Open(path) // #nosec G304 -- the configuration names the file on purpose
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }() // read only
	data, err := io.ReadAll(io.LimitReader(f, MaxSize+1))
	if err != nil {
		return "", err // an *fs.PathError, which names the path
	}
	if len(data) > MaxSize {
		return "", fmt.Errorf("%s is larger than %d bytes", path, MaxSize)
	}
	return string(data), nil
}
