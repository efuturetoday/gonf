package env

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/efuturetoday/gonf"
	"github.com/efuturetoday/gonf/source"
)

// FileOrdinal is the default ordinal of a .env file Source, just below the
// process environment, as in Quarkus.
const FileOrdinal = 295

type fileSource struct {
	gonf.Source
}

var _ gonf.KeyMapper = fileSource{}

// File returns a Source reading the .env file at path, named "file:" + path.
// Like New it implements gonf.KeyMapper, so "DB_URL" in the file is found
// as "db.url" and "_DEV_DB_URL" as "%dev.db.url". The variables are not
// added to the process environment.
//
// Supported syntax, one assignment per line:
//
//	# comment
//	DB_URL=jdbc:postgresql://localhost/app   # trailing comment
//	export DB_USER=app
//	DB_PASSWORD="multi word \"quoted\" value\nwith escapes"
//	GREETING='single quoted, taken literally'
//
// Double-quoted values support \n, \r, \t, \" and \\ and may span lines;
// single-quoted values are taken literally. Unquoted values end at the line
// end or at " #". Later assignments override earlier ones.
func File(path string, opts ...source.Option) gonf.Source {
	opts = append([]source.Option{source.WithOrdinal(FileOrdinal)}, opts...)
	read := func(context.Context) ([]byte, error) { return os.ReadFile(path) }
	return fileSource{Source: source.New("file:"+path, read, source.FormatFunc(parseDotenv), opts...)}
}

// MapKey maps key like the env Source does.
func (fileSource) MapKey(key string) string { return mapKey(key) }

// parseDotenv parses the content of a .env file.
func parseDotenv(name string, data []byte) (map[string]gonf.Value, error) {
	p := dotenvParser{name: name, s: string(data), line: 1}
	out := map[string]gonf.Value{}
	for {
		p.skipBlankAndComments()
		if p.i >= len(p.s) {
			return out, nil
		}
		line := p.line
		key, err := p.key()
		if err != nil {
			return nil, err
		}
		value, err := p.value()
		if err != nil {
			return nil, err
		}
		out[key] = gonf.Value{Raw: value, Origin: gonf.Origin{Location: fmt.Sprintf("%s:%d", name, line)}}
	}
}

type dotenvParser struct {
	name string
	s    string
	i    int
	line int
}

func (p *dotenvParser) errorf(format string, args ...any) error {
	return fmt.Errorf("%s:%d: %s", p.name, p.line, fmt.Sprintf(format, args...))
}

// skipBlankAndComments moves to the start of the next assignment.
func (p *dotenvParser) skipBlankAndComments() {
	for p.i < len(p.s) {
		switch c := p.s[p.i]; {
		case c == ' ' || c == '\t' || c == '\r':
			p.i++
		case c == '\n':
			p.i++
			p.line++
		case c == '#':
			p.skipToLineEnd()
		default:
			return
		}
	}
}

func (p *dotenvParser) skipToLineEnd() {
	if end := strings.IndexByte(p.s[p.i:], '\n'); end >= 0 {
		p.i += end
		return
	}
	p.i = len(p.s)
}

func (p *dotenvParser) skipSpaces() {
	for p.i < len(p.s) && (p.s[p.i] == ' ' || p.s[p.i] == '\t') {
		p.i++
	}
}

// key reads "[export ]KEY=" and returns KEY.
func (p *dotenvParser) key() (string, error) {
	if rest := p.s[p.i:]; strings.HasPrefix(rest, "export ") || strings.HasPrefix(rest, "export\t") {
		p.i += len("export")
		p.skipSpaces()
	}
	start := p.i
	for p.i < len(p.s) && p.s[p.i] != '=' && p.s[p.i] != '\n' {
		p.i++
	}
	if p.i >= len(p.s) || p.s[p.i] != '=' {
		return "", p.errorf("missing '='")
	}
	key := strings.TrimSpace(p.s[start:p.i])
	p.i++ // '='
	if !validDotenvKey(key) {
		return "", p.errorf("invalid key %q", key)
	}
	return key, nil
}

// validDotenvKey allows letters, digits, '_', '.' and '-'.
func validDotenvKey(key string) bool {
	if key == "" {
		return false
	}
	for _, c := range []byte(key) {
		isLetter := 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z'
		if !isLetter && !('0' <= c && c <= '9') && c != '_' && c != '.' && c != '-' {
			return false
		}
	}
	return true
}

// value reads the value after '=' up to the end of its line.
func (p *dotenvParser) value() (string, error) {
	p.skipSpaces()
	if p.i >= len(p.s) {
		return "", nil
	}
	quote := p.s[p.i]
	if quote != '"' && quote != '\'' {
		end := strings.IndexByte(p.s[p.i:], '\n')
		if end < 0 {
			end = len(p.s) - p.i
		}
		raw := p.s[p.i : p.i+end]
		p.i += end
		if c := strings.Index(raw, " #"); c >= 0 {
			raw = raw[:c]
		}
		if c := strings.Index(raw, "\t#"); c >= 0 {
			raw = raw[:c]
		}
		return strings.TrimRight(raw, " \t\r"), nil
	}

	start := p.line
	p.i++
	var b strings.Builder
	for {
		if p.i >= len(p.s) {
			p.line = start
			return "", p.errorf("unclosed %c", quote)
		}
		c := p.s[p.i]
		switch {
		case c == quote:
			p.i++
			return b.String(), p.afterQuoted()
		case c == '\\' && quote == '"' && p.i+1 < len(p.s):
			p.i += 2
			switch esc := p.s[p.i-1]; esc {
			case 'n':
				b.WriteByte('\n')
			case 'r':
				b.WriteByte('\r')
			case 't':
				b.WriteByte('\t')
			case '"', '\\':
				b.WriteByte(esc)
			default:
				if esc == '\n' {
					p.line++
				}
				b.WriteByte('\\')
				b.WriteByte(esc)
			}
		default:
			if c == '\n' {
				p.line++
			}
			b.WriteByte(c)
			p.i++
		}
	}
}

// afterQuoted allows only whitespace and a comment after a quoted value.
func (p *dotenvParser) afterQuoted() error {
	p.skipSpaces()
	if p.i < len(p.s) && p.s[p.i] == '\r' {
		p.i++
	}
	if p.i >= len(p.s) || p.s[p.i] == '\n' {
		return nil
	}
	if p.s[p.i] == '#' {
		p.skipToLineEnd()
		return nil
	}
	return p.errorf("unexpected text after quoted value")
}
