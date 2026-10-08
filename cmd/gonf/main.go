// Command gonf helps to write gonf configuration.
//
//	gonf encrypt [-key-file path]
//
// encrypt reads a value and prints it encrypted as ${aes-gcm::...}, to paste
// into a configuration file. The key comes from GONF_SECRET_AES_GCM_KEY, the
// variable the application reads it from, or from the file given with
// -key-file. The value is typed in without echo, or piped in:
//
//	echo -n s3cret | gonf encrypt
//
// Neither the key nor the value is ever an argument, so neither ends up in
// the shell history.
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/efuturetoday/gonf/secret/aesgcm"
	"github.com/efuturetoday/gonf/source/env"
	"golang.org/x/term"
)

// keyVariable is the environment variable that holds the key.
var keyVariable = env.MapKey(aesgcm.Key)

func main() {
	in := readPiped(os.Stdin)
	if fd := int(os.Stdin.Fd()); term.IsTerminal(fd) {
		in = func() (string, error) {
			fmt.Fprint(os.Stderr, "Value: ")
			b, err := term.ReadPassword(fd)
			fmt.Fprintln(os.Stderr)
			return string(b), err
		}
	}
	os.Exit(run(os.Args[1:], in, os.Getenv, os.Stdout, os.Stderr))
}

// run runs the command in args, reading the value with readValue, and
// returns the exit code.
func run(args []string, readValue func() (string, error), getenv func(string) string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "encrypt" {
		return report(stderr, 2, "usage: gonf encrypt [-key-file path]")
	}
	flags := flag.NewFlagSet("encrypt", flag.ContinueOnError)
	flags.SetOutput(stderr)
	keyFile := flags.String("key-file", "", "read the key from this file instead of "+keyVariable)
	if err := flags.Parse(args[1:]); err != nil {
		return 2
	}
	if flags.NArg() > 0 {
		return report(stderr, 2, "gonf encrypt: the value is read from the terminal or a pipe, not from arguments")
	}
	if err := encrypt(*keyFile, readValue, getenv, stdout); err != nil {
		return report(stderr, 1, "gonf encrypt:", err)
	}
	return 0
}

// report prints a to stderr and returns code. A failure to print is not
// reported; there is nowhere left to report it.
func report(stderr io.Writer, code int, a ...any) int {
	_, _ = fmt.Fprintln(stderr, a...)
	return code
}

// encrypt prints the value from readValue encrypted with the key.
func encrypt(keyFile string, readValue func() (string, error), getenv func(string) string, stdout io.Writer) error {
	key := getenv(keyVariable)
	if keyFile != "" {
		b, err := os.ReadFile(keyFile)
		if err != nil {
			return err
		}
		key = strings.TrimRight(string(b), "\r\n")
	}
	if key == "" {
		return fmt.Errorf("no key: set %s or use -key-file", keyVariable)
	}
	value, err := readValue()
	if err != nil {
		return fmt.Errorf("read value: %w", err)
	}
	if value == "" {
		return errors.New("empty value")
	}
	encrypted, err := aesgcm.Encrypt(key, value)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(stdout, "${%s::%s}\n", aesgcm.Name, encrypted)
	return err
}

// readPiped returns a function that reads the value from r, without the
// line break at the end that echo or a file adds.
func readPiped(r io.Reader) func() (string, error) {
	return func() (string, error) {
		b, err := io.ReadAll(r)
		b = bytes.TrimSuffix(b, []byte("\n"))
		b = bytes.TrimSuffix(b, []byte("\r"))
		return string(b), err
	}
}
