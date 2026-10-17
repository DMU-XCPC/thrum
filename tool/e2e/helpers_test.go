// Package e2e drives the built binary through the design's operations: every command,
// the refusals that matter, and two sites over a real channel.
package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

var (
	thrumBin string
	hubBin   string
)

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "thrum-e2e-*")
	if err != nil {
		panic(err)
	}
	thrumBin = filepath.Join(dir, "thrum")
	build := exec.Command("go", "build", "-o", thrumBin, "./cmd/thrum")
	build.Dir = ".."
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "building thrum: %v\n%s\n", err, out)
		os.Exit(1)
	}
	hubBin = filepath.Join(dir, "thrum-hub")
	buildHub := exec.Command("go", "build", "-o", hubBin, "./e2e/hub")
	buildHub.Dir = ".."
	buildHub.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := buildHub.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "building the test hub: %v\n%s\n", err, out)
		os.Exit(1)
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

type result struct {
	stdout string
	stderr string
	code   int
}

func (r result) output() string { return r.stdout + r.stderr }

func (r result) ok(t *testing.T, what string) result {
	t.Helper()
	if r.code != 0 {
		t.Fatalf("%s: exit %d\n--- stdout ---\n%s\n--- stderr ---\n%s", what, r.code, r.stdout, r.stderr)
	}
	return r
}

func (r result) fails(t *testing.T, what, want string) result {
	t.Helper()
	if r.code == 0 {
		t.Fatalf("%s: expected a refusal, but it succeeded\n%s", what, r.stdout)
	}
	if want != "" && !strings.Contains(r.output(), want) {
		t.Fatalf("%s: expected %q\n--- stdout ---\n%s\n--- stderr ---\n%s", what, want, r.stdout, r.stderr)
	}
	return r
}

type runner struct {
	t    *testing.T
	root string
	env  []string
}

func (r *runner) with(env ...string) *runner {
	cp := *r
	cp.env = append(append([]string{}, r.env...), env...)
	return &cp
}

func (r *runner) as(actor string) *runner {
	cp := *r
	cp.env = append(append([]string{}, r.env...), "THRUM_ACTOR="+actor)
	return &cp
}

func (r *runner) run(args ...string) result {
	r.t.Helper()
	full := append([]string{"--root", r.root}, args...)
	cmd := exec.Command(thrumBin, full...)
	cmd.Env = envWith(append([]string{"THRUM_ROOT=" + r.root}, r.env...)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		r.t.Fatalf("running %v: %v", args, err)
	}
	return result{stdout.String(), stderr.String(), code}
}

// withStdin runs a command with data on stdin.
func (r *runner) withStdin(data string, args ...string) result {
	r.t.Helper()
	full := append([]string{"--root", r.root}, args...)
	cmd := exec.Command(thrumBin, full...)
	cmd.Env = envWith(append([]string{"THRUM_ROOT=" + r.root}, r.env...)...)
	cmd.Stdin = strings.NewReader(data)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		r.t.Fatalf("running %v: %v", args, err)
	}
	return result{stdout.String(), stderr.String(), code}
}

func envWith(overrides ...string) []string {
	base := map[string]string{}
	for _, e := range os.Environ() {
		k, v, _ := strings.Cut(e, "=")
		base[k] = v
	}
	for _, e := range overrides {
		if k, v, ok := strings.Cut(e, "="); ok {
			base[k] = v
		}
	}
	out := make([]string, 0, len(base))
	for k, v := range base {
		out = append(out, k+"="+v)
	}
	return out
}

type workspace struct {
	t    *testing.T
	root string
	src  string
}

// connectSites gives every site the addresses of every other site and starts a
// daemon for each. From here on sync is a daemon operation; the repositories have
// no remote at all.
func connectSites(t *testing.T, sites []*workspace) []string {
	t.Helper()
	names := make([]string, len(sites))
	for i, w := range sites {
		names[i] = strings.TrimSpace(w.file("var/site"))
	}
	ports := make([]string, len(sites))
	used := map[string]bool{}
	for i := range ports {
		for {
			p := fmt.Sprintf("127.0.0.1:%d", freePort(t))
			if !used[p] {
				used[p] = true
				ports[i] = p
				break
			}
		}
	}
	for _, w := range sites {
		for j := range sites {
			w.r().run("site", "reach", names[j], "--at", ports[j]).ok(t, "reach "+names[j])
		}
	}
	for i, w := range sites {
		startDaemon(t, w, ports[i])
	}
	return ports
}

func newWorkspace(t *testing.T) *workspace {
	t.Helper()
	dir := t.TempDir()
	root := filepath.Join(dir, "ws")
	src := filepath.Join(dir, "src")
	initRepo(t, src)
	r := &runner{t: t, root: root}
	r.run("create", root, "--site", "s1", "--code", "code="+src).ok(t, "create")
	return &workspace{t: t, root: root, src: src}
}

func (w *workspace) r() *runner {
	return &runner{t: w.t, root: w.root, env: []string{"THRUM_ACTOR=a1"}}
}

func (w *workspace) path(rel ...string) string {
	return filepath.Join(append([]string{w.root}, rel...)...)
}

func (w *workspace) git(args ...string) string { return runGit(w.t, w.root, args...) }

func (w *workspace) file(rel string) string {
	w.t.Helper()
	b, err := os.ReadFile(w.path(rel))
	if err != nil {
		w.t.Fatalf("reading %s: %v", rel, err)
	}
	return string(b)
}

func (w *workspace) write(rel, content string) {
	w.t.Helper()
	p := w.path(rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		w.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		w.t.Fatal(err)
	}
}

func (w *workspace) exists(rel string) bool {
	_, err := os.Stat(w.path(rel))
	return err == nil
}

func (w *workspace) mustExist(rel string) {
	w.t.Helper()
	if !w.exists(rel) {
		w.t.Fatalf("%s is missing", rel)
	}
}

func (w *workspace) mustNotExist(rel string) {
	w.t.Helper()
	if w.exists(rel) {
		w.t.Fatalf("%s should not exist", rel)
	}
}

func initRepo(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "README"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "init")
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := gitOutput(dir, args...)
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
	return out
}

func gitOutput(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// jsonLines decodes one JSON value per line, which is what --json prints for a stream.
func jsonLines[T any](t *testing.T, s string) []T {
	t.Helper()
	var out []T
	for _, ln := range strings.Split(s, "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" {
			continue
		}
		var v T
		if err := json.Unmarshal([]byte(ln), &v); err != nil {
			t.Fatalf("decoding %q: %v", ln, err)
		}
		out = append(out, v)
	}
	return out
}
