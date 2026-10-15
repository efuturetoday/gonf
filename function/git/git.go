// Package git provides the gonf.Function git, with what git knows of a
// directory, for values such as the tag of an image:
//
//	app.image.tag=${git::version:latest}
//	app.image.branch-tag=${slug::${git::branch}}-${git::commit}
//
// The argument names what, with a default after ':' for when git does not
// know it, as ${key:default} has for a key:
//
//	commit   the short commit of HEAD
//	sha      the full commit of HEAD
//	branch   the branch of HEAD, as named, such as feature/login; while
//	         HEAD is detached, as in CI, from GITHUB_HEAD_REF,
//	         GITHUB_REF_NAME, CI_COMMIT_REF_NAME or BRANCH_NAME
//	tag      a tag on HEAD
//	dirty    true if there are changes that are not committed, untracked
//	         files included, else false
//	changes  a short hash of these changes, the same for the same changes;
//	         empty without changes
//	version  the short commit, or <commit>-dirty.<changes>
//
// The Function runs the git command, only when a value calls it, and gonf
// keeps its result for the Load, so a reload of Watch asks git again.
// Without git, outside a repository, before the first commit, or without
// a branch or tag, it fails with the reason, unless there is a default.
package git

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/efuturetoday/gonf"
)

// Name is the name of Function in ${git::commit}.
const Name = "git"

// timeout bounds one run of git.
const timeout = 10 * time.Second

// ciBranches are the variables of CI systems with the branch, for a
// detached HEAD: GitHub Actions (of a pull request first), GitLab CI,
// Jenkins.
var ciBranches = []string{"GITHUB_HEAD_REF", "GITHUB_REF_NAME", "CI_COMMIT_REF_NAME", "BRANCH_NAME"}

// Option configures Function.
type Option func(*repo)

// WithName gives the Function another name than git, for a second
// directory: ${git-lib::commit}.
func WithName(name string) Option {
	return func(r *repo) { r.name = name }
}

// repo is the directory a Function asks git about.
type repo struct {
	name, dir string
}

// Function returns the gonf.Function git for the repository of dir.
func Function(dir string, opts ...Option) gonf.Function {
	r := &repo{name: Name, dir: dir}
	for _, opt := range opts {
		opt(r)
	}
	return gonf.Function{Name: r.name, Call: func(_ *gonf.Config, arg string, _ gonf.Origin) (string, error) {
		what, def, hasDefault := strings.Cut(arg, ":")
		v, err := r.value(what)
		if err != nil && hasDefault && !errors.Is(err, errUnknown) {
			return def, nil
		}
		return v, err
	}}
}

var errUnknown = errors.New("unknown argument (known: commit, sha, branch, tag, dirty, changes, version)")

// value returns what git knows as what.
func (r *repo) value(what string) (string, error) {
	switch what {
	case "commit", "sha", "branch", "tag", "dirty", "changes", "version":
	default:
		return "", fmt.Errorf("%q: %w", what, errUnknown)
	}
	if err := r.check(); err != nil {
		return "", err
	}
	switch what {
	case "branch":
		if b, _ := r.git("symbolic-ref", "--short", "-q", "HEAD"); b != "" {
			return b, nil
		}
		for _, k := range ciBranches {
			if b := os.Getenv(k); b != "" {
				return b, nil
			}
		}
		return "", errors.New("HEAD is detached, and no variable of CI names the branch")
	case "dirty":
		changes, err := r.changes()
		return strconv.FormatBool(changes != ""), err
	case "changes":
		return r.changes()
	}
	commit, err := r.git("rev-parse", "--short", "HEAD")
	if err != nil {
		return "", errors.New("no commit yet")
	}
	switch what {
	case "sha":
		return r.git("rev-parse", "HEAD")
	case "tag":
		tags, err := r.git("tag", "--points-at", "HEAD")
		if err != nil || tags == "" {
			return "", errors.New("no tag on HEAD")
		}
		return strings.SplitN(tags, "\n", 2)[0], nil
	case "version":
		changes, err := r.changes()
		if err != nil || changes == "" {
			return commit, err
		}
		return commit + "-dirty." + changes, nil
	}
	return commit, nil
}

// check returns why git cannot answer for dir, or nil.
func (r *repo) check() error {
	if _, err := exec.LookPath("git"); err != nil {
		if top := findRepo(r.dir); top != "" {
			return fmt.Errorf("%s is a repository of git, but there is no git on PATH", top)
		}
		return errors.New("no git on PATH")
	}
	if inside, _ := r.git("rev-parse", "--is-inside-work-tree"); inside != "true" {
		return fmt.Errorf("%s is not in a repository of git", r.dir)
	}
	return nil
}

// git runs git in dir and returns its output, trimmed.
func (r *repo) git(args ...string) (string, error) {
	out, err := r.gitBytes(args...)
	return strings.TrimSpace(string(out)), err
}

func (r *repo) gitBytes(args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", append([]string{"-C", r.dir}, args...)...).Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && len(exit.Stderr) > 0 {
			return nil, fmt.Errorf("git %s: %s", args[0], bytes.TrimSpace(exit.Stderr))
		}
		return nil, fmt.Errorf("git %s: %w", args[0], err)
	}
	return out, nil
}

// changes returns a short hash of the changes of dir that are not
// committed, of tracked and untracked files, or "" if there are none.
func (r *repo) changes() (string, error) {
	st, err := r.gitBytes("status", "--porcelain", ".")
	if err != nil {
		return "", err
	}
	if len(bytes.TrimSpace(st)) == 0 {
		return "", nil
	}
	top, err := r.git("rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}
	h := sha256.New()
	diff, _ := r.gitBytes("diff", "HEAD", "--binary", "--", ".") // fails before the first commit
	h.Write(diff)
	untracked, err := r.gitBytes("ls-files", "-z", "--others", "--exclude-standard", "--full-name", ".")
	if err != nil {
		return "", err
	}
	for f := range strings.SplitSeq(string(untracked), "\x00") {
		if f == "" {
			continue
		}
		fmt.Fprintf(h, "\x00%s\x00", f)
		if data, err := os.ReadFile(filepath.Join(top, f)); err == nil {
			h.Write(data)
		}
	}
	return hex.EncodeToString(h.Sum(nil))[:7], nil
}

// findRepo returns the directory of dir or above with a .git, or "".
func findRepo(dir string) string {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return ""
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}
