package git_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/efuturetoday/gonf"
	"github.com/efuturetoday/gonf/function/git"
	"github.com/efuturetoday/gonf/source/memory"
)

// eval returns expr evaluated with the Function git for dir.
func eval(t *testing.T, dir, expr string, opts ...git.Option) (string, error) {
	t.Helper()
	cfg, err := gonf.Load(context.Background(), gonf.WithSources(memory.New(nil)), gonf.WithFunctions(git.Function(dir, opts...)))
	if err != nil {
		t.Fatal(err)
	}
	return cfg.Eval(expr)
}

// must returns expr evaluated, and fails the test on an error.
func must(t *testing.T, dir, expr string) string {
	t.Helper()
	v, err := eval(t, dir, expr)
	if err != nil {
		t.Fatalf("%s: %v", expr, err)
	}
	return v
}

// repo returns a new repository of git on the branch feature/login and a
// function that runs git in it.
func repo(t *testing.T) (string, func(args ...string) string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git on PATH")
	}
	for _, k := range []string{"GITHUB_HEAD_REF", "GITHUB_REF_NAME", "CI_COMMIT_REF_NAME", "BRANCH_NAME"} {
		t.Setenv(k, "")
	}
	dir := t.TempDir()
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false", "-c", "tag.gpgsign=false"}, args...)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	run("init", "-q", "-b", "feature/login")
	return dir, run
}

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Not parallel: t.Setenv modifies the process environment.
func TestFunction(t *testing.T) {
	dir, run := repo(t)
	if got := must(t, dir, "${git::branch}"); got != "feature/login" {
		t.Errorf("branch before a commit = %q", got)
	}
	if _, err := eval(t, dir, "${git::commit}"); err == nil || !strings.Contains(err.Error(), "no commit yet") {
		t.Errorf("commit before a commit: %v", err)
	}

	write(t, dir, "main.go", "package main\n")
	run("add", ".")
	run("commit", "-q", "-m", "first")
	commit, sha := run("rev-parse", "--short", "HEAD"), run("rev-parse", "HEAD")
	for expr, expected := range map[string]string{
		"${git::commit}": commit, "${git::sha}": sha, "${git::dirty}": "false", "${git::changes}": "",
		"${git::version}": commit, "${git::tag:none}": "none",
	} {
		if got := must(t, dir, expr); got != expected {
			t.Errorf("clean: %s = %q, want %q", expr, got, expected)
		}
	}
	if _, err := eval(t, dir, "${git::tag}"); err == nil || !strings.Contains(err.Error(), "no tag on HEAD") {
		t.Errorf("tag without one: %v", err)
	}
	run("tag", "v1.0.0")
	if got := must(t, dir, "${git::tag}"); got != "v1.0.0" {
		t.Errorf("tag = %q", got)
	}

	write(t, dir, "main.go", "package main // changed\n")
	changes := must(t, dir, "${git::changes}")
	if !regexp.MustCompile(`^[0-9a-f]{7}$`).MatchString(changes) || must(t, dir, "${git::dirty}") != "true" || must(t, dir, "${git::version}") != commit+"-dirty."+changes {
		t.Errorf("changed: changes %q, version %q", changes, must(t, dir, "${git::version}"))
	}
	if got := must(t, dir, "${git::changes}"); got != changes {
		t.Errorf("the same changes: %q, want %q", got, changes)
	}
	write(t, dir, "new.go", "package main\n")
	if got := must(t, dir, "${git::changes}"); got == changes {
		t.Errorf("an untracked file: changes stay %q", got)
	}

	if _, err := eval(t, dir, "${git::author:x}"); err == nil || !strings.Contains(err.Error(), "unknown argument") {
		t.Errorf("unknown argument, even with a default: %v", err)
	}
	if got, err := eval(t, dir, "${lib::commit}", git.WithName("lib")); err != nil || got != commit {
		t.Errorf("WithName: %q, %v", got, err)
	}
}

// With a detached HEAD, as in CI, the branch comes from the variables of
// the CI system. Not parallel: t.Setenv.
func TestFunction_detached(t *testing.T) {
	dir, run := repo(t)
	write(t, dir, "main.go", "package main\n")
	run("add", ".")
	run("commit", "-q", "-m", "first")
	run("checkout", "-q", "--detach")
	if _, err := eval(t, dir, "${git::branch}"); err == nil || !strings.Contains(err.Error(), "detached") {
		t.Errorf("detached without CI: %v", err)
	}
	t.Setenv("GITHUB_REF_NAME", "main")
	t.Setenv("GITHUB_HEAD_REF", "feature/pr")
	if got := must(t, dir, "${git::branch}"); got != "feature/pr" {
		t.Errorf("detached in a pull request: branch %q, want feature/pr", got)
	}
}

// Outside a repository or without git, a value fails with the reason, or
// takes its default. Not parallel: t.Setenv.
func TestFunction_noRepository(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git on PATH")
	}
	dir := t.TempDir()
	if _, err := eval(t, dir, "${git::commit}"); err == nil || !strings.Contains(err.Error(), "not in a repository of git") {
		t.Errorf("outside a repository: %v", err)
	}
	if got := must(t, dir, "${git::version:latest}"); got != "latest" {
		t.Errorf("outside a repository with a default: %q", got)
	}
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", t.TempDir())
	if _, err := eval(t, dir, "${git::commit}"); err == nil || !strings.Contains(err.Error(), "is a repository of git, but there is no git on PATH") {
		t.Errorf("a repository without git: %v", err)
	}
	if got := must(t, dir, "${git::commit:none}"); got != "none" {
		t.Errorf("without git with a default: %q", got)
	}
}

// The remote is the web address of origin, without credentials.
func TestFunction_remote(t *testing.T) {
	dir, run := repo(t)
	for _, k := range []string{"GITHUB_SERVER_URL", "GITHUB_REPOSITORY", "CI_PROJECT_URL"} {
		t.Setenv(k, "")
	}
	if _, err := eval(t, dir, "${git::remote}"); err == nil || !strings.Contains(err.Error(), "no remote origin") {
		t.Errorf("without a remote: %v", err)
	}
	t.Setenv("GITHUB_SERVER_URL", "https://github.com")
	t.Setenv("GITHUB_REPOSITORY", "acme/ci")
	if got := must(t, dir, "${git::remote}"); got != "https://github.com/acme/ci" {
		t.Errorf("in GitHub Actions = %q", got)
	}
	run("remote", "add", "origin", "git@github.com:acme/hello.git")
	if got := must(t, dir, "${git::remote}"); got != "https://github.com/acme/hello" {
		t.Errorf("remote = %q", got)
	}
}

func TestWebURL(t *testing.T) {
	t.Parallel()
	for in, expected := range map[string]string{
		"git@github.com:acme/hello.git":                "https://github.com/acme/hello",
		"ssh://git@gitlab.com:22/group/sub/app.git":    "https://gitlab.com/group/sub/app",
		"https://user:token@github.com/acme/hello.git": "https://github.com/acme/hello",
		"https://git.example:8443/acme/hello":          "https://git.example:8443/acme/hello",
		"/srv/git/hello.git":                           "",
		"file:///srv/git/hello.git":                    "",
	} {
		if got := git.WebURL(in); got != expected {
			t.Errorf("webURL(%q) = %q, want %q", in, got, expected)
		}
	}
}
