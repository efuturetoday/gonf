// Package properties provides the Java .properties format for gonf.
package properties

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/efuturetoday/gonf"
	"github.com/efuturetoday/gonf/source"
)

// Format is the Java .properties format (UTF-8).
var Format source.Format = source.FormatFunc(parseProperties)

// parseProperties parses the Java .properties format (UTF-8):
// "key=value", "key: value" or "key value"; comments start with # or !;
// a trailing backslash continues the line. Duplicate keys: last wins.
func parseProperties(name string, data []byte) (map[string]gonf.Value, error) {
	out := make(map[string]gonf.Value)
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")

	for i := 0; i < len(lines); i++ {
		startLine := i + 1
		line := strings.TrimLeft(lines[i], " \t\f")
		if line == "" || line[0] == '#' || line[0] == '!' {
			continue
		}
		// Join continuation lines.
		for endsWithContinuation(line) && i+1 < len(lines) {
			i++
			line = line[:len(line)-1] + strings.TrimLeft(lines[i], " \t\f")
		}
		if endsWithContinuation(line) {
			line = line[:len(line)-1]
		}

		rawKey, rawValue := splitKeyValue(line)
		key, err := unescapeProperties(rawKey)
		if err != nil {
			return nil, fmt.Errorf("%s:%d: %w", name, startLine, err)
		}
		value, err := unescapeProperties(rawValue)
		if err != nil {
			return nil, fmt.Errorf("%s:%d: %w", name, startLine, err)
		}
		out[key] = gonf.Value{
			Raw:    value,
			Origin: gonf.Origin{Location: fmt.Sprintf("%s:%d", name, startLine)},
		}
	}
	return out, nil
}

// endsWithContinuation reports whether line ends with an odd number of backslashes.
func endsWithContinuation(line string) bool {
	n := 0
	for i := len(line) - 1; i >= 0 && line[i] == '\\'; i-- {
		n++
	}
	return n%2 == 1
}

// splitKeyValue splits at the first unescaped '=', ':' or whitespace.
func splitKeyValue(line string) (key, value string) {
	end := len(line)
	for i := 0; i < len(line); i++ {
		c := line[i]
		if c == '\\' {
			i++
			continue
		}
		if c == '=' || c == ':' || c == ' ' || c == '\t' || c == '\f' {
			end = i
			break
		}
	}
	key = line[:end]
	rest := strings.TrimLeft(line[end:], " \t\f")
	if rest != "" && (rest[0] == '=' || rest[0] == ':') {
		rest = strings.TrimLeft(rest[1:], " \t\f")
	}
	return key, rest
}

func unescapeProperties(s string) (string, error) {
	if !strings.Contains(s, `\`) {
		return s, nil
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '\\' || i+1 == len(s) {
			b.WriteByte(c)
			continue
		}
		i++
		switch s[i] {
		case 't':
			b.WriteByte('\t')
		case 'n':
			b.WriteByte('\n')
		case 'r':
			b.WriteByte('\r')
		case 'f':
			b.WriteByte('\f')
		case 'u':
			r, ok := parseUnicodeEscape(s, i+1)
			if !ok {
				return "", fmt.Errorf("malformed \\u escape in %q", s)
			}
			i += 4
			if utf16.IsSurrogate(r) {
				// Characters outside the BMP are written as a surrogate pair: \uD83D\uDE00.
				var lo rune
				if i+2 < len(s) && s[i+1] == '\\' && s[i+2] == 'u' {
					lo, ok = parseUnicodeEscape(s, i+3)
				} else {
					ok = false
				}
				r = utf16.DecodeRune(r, lo)
				if !ok || r == utf8.RuneError {
					return "", fmt.Errorf("unpaired surrogate in \\u escape in %q", s)
				}
				i += 6
			}
			b.WriteRune(r)
		default:
			// Unknown escapes yield the character itself, as in Java.
			r, size := utf8.DecodeRuneInString(s[i:])
			b.WriteRune(r)
			i += size - 1
		}
	}
	return b.String(), nil
}

// parseUnicodeEscape parses the 4 hex digits of a \u escape starting at s[i].
func parseUnicodeEscape(s string, i int) (rune, bool) {
	if i+4 > len(s) {
		return 0, false
	}
	r, err := strconv.ParseUint(s[i:i+4], 16, 32)
	if err != nil {
		return 0, false
	}
	return rune(r), true
}
