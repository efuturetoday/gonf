package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/efuturetoday/gonf"
	"github.com/efuturetoday/gonf/secret/aesgcm"
	"github.com/efuturetoday/gonf/source/memory"
)

// value returns a readValue that returns s.
func value(s string) func() (string, error) {
	return func() (string, error) { return s, nil }
}

// environment returns a getenv with the given variables.
func environment(vars map[string]string) func(string) string {
	return func(name string) string { return vars[name] }
}

func TestRun_Encrypt(t *testing.T) {
	t.Parallel()
	keyFile := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(keyFile, []byte("from-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		args []string
		env  map[string]string
		key  string
	}{
		{name: "key from the environment", args: []string{"encrypt"}, env: map[string]string{"GONF_SECRET_AES_GCM_KEY": "k3y"}, key: "k3y"},
		{
			name: "key from a file",
			args: []string{"encrypt", "-key-file", keyFile},
			env:  map[string]string{"GONF_SECRET_AES_GCM_KEY": "ignored"},
			key:  "from-file",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var stdout, stderr bytes.Buffer
			if code := run(tt.args, value("s3cret"), environment(tt.env), &stdout, &stderr); code != 0 {
				t.Fatalf("exit code %d: %s", code, stderr.String())
			}
			// The output is a line to paste into a configuration file.
			line := strings.TrimSuffix(stdout.String(), "\n")
			cfg, err := gonf.Load(context.Background(),
				gonf.WithSecretHandler(aesgcm.Name, aesgcm.WithKey(tt.key)),
				gonf.WithSources(memory.New(map[string]string{"v": line})),
			)
			if err != nil {
				t.Fatal(err)
			}
			if got, err := cfg.Get("v"); err != nil || got != "s3cret" {
				t.Errorf("%q decrypts to %q, %v; want s3cret", line, got, err)
			}
		})
	}
}

func TestRun_Errors(t *testing.T) {
	t.Parallel()
	key := map[string]string{"GONF_SECRET_AES_GCM_KEY": "k3y"}
	tests := []struct {
		name     string
		args     []string
		env      map[string]string
		value    func() (string, error)
		code     int
		expected string
	}{
		{name: "no command", args: nil, value: value("x"), code: 2, expected: "usage: gonf encrypt [-key-file path]\n"},
		{name: "unknown command", args: []string{"decrypt"}, value: value("x"), code: 2, expected: "usage: gonf encrypt [-key-file path]\n"},
		{
			name:     "value as an argument",
			args:     []string{"encrypt", "s3cret"},
			env:      key,
			value:    value("x"),
			code:     2,
			expected: "gonf encrypt: the value is read from the terminal or a pipe, not from arguments\n",
		},
		{
			name:     "no key",
			args:     []string{"encrypt"},
			value:    value("x"),
			code:     1,
			expected: "gonf encrypt: no key: set GONF_SECRET_AES_GCM_KEY or use -key-file\n",
		},
		{
			name:     "missing key file",
			args:     []string{"encrypt", "-key-file", filepath.Join(t.TempDir(), "missing")},
			value:    value("x"),
			code:     1,
			expected: "no such file or directory",
		},
		{name: "empty value", args: []string{"encrypt"}, env: key, value: value(""), code: 1, expected: "gonf encrypt: empty value\n"},
		{
			name:     "value not readable",
			args:     []string{"encrypt"},
			env:      key,
			value:    func() (string, error) { return "", errors.New("closed") },
			code:     1,
			expected: "gonf encrypt: read value: closed\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var stdout, stderr bytes.Buffer
			code := run(tt.args, tt.value, environment(tt.env), &stdout, &stderr)
			if code != tt.code || !strings.Contains(stderr.String(), tt.expected) || stdout.Len() > 0 {
				t.Errorf("exit code %d, stdout %q, stderr %q; want %d and %q", code, stdout.String(), stderr.String(), tt.code, tt.expected)
			}
		})
	}
}

func TestReadPiped(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input    string
		expected string
	}{
		{input: "s3cret", expected: "s3cret"},
		{input: "s3cret\n", expected: "s3cret"},
		{input: "s3cret\r\n", expected: "s3cret"},
		{input: "two\nlines\n", expected: "two\nlines"},
	}
	for _, tt := range tests {
		got, err := readPiped(strings.NewReader(tt.input))()
		if err != nil || got != tt.expected {
			t.Errorf("readPiped(%q) = %q, %v; want %q", tt.input, got, err, tt.expected)
		}
	}
}
