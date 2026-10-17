package gitx

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// Git is the real git, always as a subprocess. The design leans on git for worktrees,
// bundles, tags and the archive, so git stays the authority rather than being
// reimplemented — and the usage here is unusual enough that a library would have to
// grow to match it.

// Error carries what git said when it failed.
type Error struct {
	Args   []string
	Stderr string
	Err    error
}

func (e *Error) Error() string {
	msg := strings.TrimSpace(e.Stderr)
	if msg == "" {
		msg = e.Err.Error()
	}
	return fmt.Sprintf("git %s: %s", strings.Join(e.Args, " "), msg)
}

func (e *Error) Unwrap() error { return e.Err }

// Run executes git in dir and returns its standard output. dir may be a working tree
// or a bare repository.
func Run(dir string, args ...string) (string, error) {
	return RunEnv(dir, nil, args...)
}

// RunEnv is Run with extra environment variables, which is how a commit message or an
// author is passed without going through a shell.
func RunEnv(dir string, env []string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	// The C locale, always: several operations read git's output, and a translated
	// message is a different string.
	cmd.Env = append(cmd.Environ(), "LC_ALL=C", "LANG=C")
	if env != nil {
		cmd.Env = append(cmd.Env, env...)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", &Error{Args: args, Stderr: stderr.String(), Err: err}
	}
	return stdout.String(), nil
}

// FetchURL fetches a URL the caller built, not a configured remote. The ext
// allowance is passed on the command line, so no repository stores it.
func FetchURL(dir, url string, refspecs ...string) error {
	args := append([]string{"-c", "protocol.ext.allow=always", "fetch", "--no-tags", url}, refspecs...)
	return OK(dir, args...)
}

// PushURL pushes to a URL the caller built, not a configured remote.
func PushURL(dir, url string, refs ...string) error {
	args := append([]string{"-c", "protocol.ext.allow=always", "push", url}, refs...)
	return OK(dir, args...)
}

// OK runs git for its effect and discards its output.
func OK(dir string, args ...string) error {
	_, err := Run(dir, args...)
	return err
}

// Version reports the git version, which the validator checks a floor against.
func Version() (string, error) {
	out, err := Run("", "--version")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// IsRepo reports whether dir holds a git repository.
func IsRepo(dir string) bool {
	_, err := Run(dir, "rev-parse", "--git-dir")
	return err == nil
}

// Resolves reports whether rev names a commit in the repository at dir.
func Resolves(dir, rev string) bool {
	_, err := Run(dir, "rev-parse", "--verify", "--quiet", rev+"^{commit}")
	return err == nil
}

// IsErr reports whether err came from git and its stderr contains any of the given
// needles. Callers use it to tell "already exists" from a real failure.
func IsErr(err error, needles ...string) bool {
	var ge *Error
	if !errors.As(err, &ge) {
		return false
	}
	for _, n := range needles {
		if strings.Contains(ge.Stderr, n) {
			return true
		}
	}
	return false
}

// IdentityArgs supplies a commit identity when the environment has none, so that the
// tool works on a machine where git has never been configured. When git does have
// one, nothing is added and the commit is attributed to whoever is running it.
func IdentityArgs(dir, fallbackName string) []string {
	if _, err := Run(dir, "config", "user.email"); err == nil {
		return nil
	}
	name := fallbackName
	if name == "" {
		name = "thrum"
	}
	return []string{"-c", "user.name=" + name, "-c", "user.email=" + name + "@localhost"}
}
