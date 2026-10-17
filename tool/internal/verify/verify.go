// Package verify checks the mechanical rules the design states, and reports what
// fails. It never repairs: a wrong repair costs more than a stale line.
package verify

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/multiagentworkspace/thrum/tool/internal/format"
	"github.com/multiagentworkspace/thrum/tool/internal/gitx"
	"github.com/multiagentworkspace/thrum/tool/internal/layout"
	"github.com/multiagentworkspace/thrum/tool/internal/records"
	"github.com/multiagentworkspace/thrum/tool/internal/workspace"
)

// Problem is one thing that is not as the design says it should be.
type Problem struct {
	Path    string `json:"path,omitempty"`
	Message string `json:"message"`
}

func (p Problem) String() string {
	if p.Path == "" {
		return p.Message
	}
	return p.Path + ": " + p.Message
}

var hex64 = regexp.MustCompile(`^[0-9a-f]{64}$`)

type checker struct {
	l     layout.Layout
	ps    []Problem
	repos map[string]records.Repo
}

func (c *checker) add(path, msg string, args ...any) {
	c.ps = append(c.ps, Problem{Path: relative(c.l, path), Message: fmt.Sprintf(msg, args...)})
}

// Verify runs every check and returns what failed.
func Verify(l layout.Layout) []Problem {
	c := &checker{l: l, repos: map[string]records.Repo{}}
	c.tree()
	c.sites()
	c.actors()
	c.reposDecl()
	c.worktrees()
	c.jobs()
	c.handoffs()
	c.conventions()
	c.topics()
	c.skeleton()
	return c.ps
}

func (c *checker) tree() {
	l := c.l
	if st, err := os.Stat(l.Root); err != nil || !st.IsDir() {
		c.add("", "%s is not a directory", l.Root)
		return
	}
	for _, d := range []struct{ path, what string }{
		{l.LineGit(), "the line repository"},
		{l.LedgerGit(), "the ledger repository"},
		{l.Repos(), "the bare repositories directory"},
		{l.Store(), "the store"},
	} {
		if _, err := os.Stat(d.path); err != nil {
			c.add(d.path, "%s is missing", d.what)
		}
	}
}

func (c *checker) sites() {
	l := c.l
	names := map[string]bool{}
	entries, err := os.ReadDir(l.Sites())
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".yaml" {
			continue
		}
		path := filepath.Join(l.Sites(), e.Name())
		var s records.Site
		ok, err := records.Read(path, &s)
		if err != nil {
			c.add(path, "%v", err)
			continue
		}
		if !ok {
			continue
		}
		if s.Site != strings.TrimSuffix(e.Name(), ".yaml") {
			c.add(path, "site does not equal its file name")
		}
		if _, err := workspace.ParseSiteKey(s.Key); err != nil {
			c.add(path, "%v", err)
		}
		names[s.Site] = true
	}
	site := readFile(l.SiteFile())
	if site == "" {
		c.add(l.SiteFile(), "this machine does not name its site")
		return
	}
	if !names[site] {
		c.add(l.SiteFile(), "site %q is not declared", site)
	}
}

func (c *checker) actors() {
	entries, err := os.ReadDir(c.l.Actors())
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".yaml" {
			continue
		}
		path := filepath.Join(c.l.Actors(), e.Name())
		var a records.Actor
		ok, err := records.Read(path, &a)
		if err != nil {
			c.add(path, "%v", err)
			continue
		}
		if !ok {
			continue
		}
		if a.Actor != strings.TrimSuffix(e.Name(), ".yaml") {
			c.add(path, "actor does not equal its file name")
		}
		if a.Kind != "human" && a.Kind != "agent" {
			c.add(path, "%q is not a kind of actor", a.Kind)
		}
	}
}

func (c *checker) reposDecl() {
	entries, err := os.ReadDir(c.l.RepoDecls())
	if err == nil {
		for _, e := range entries {
			if e.IsDir() || filepath.Ext(e.Name()) != ".yaml" {
				continue
			}
			path := filepath.Join(c.l.RepoDecls(), e.Name())
			var r records.Repo
			ok, err := records.Read(path, &r)
			if err != nil {
				c.add(path, "%v", err)
				continue
			}
			if !ok {
				continue
			}
			if r.Repo != strings.TrimSuffix(e.Name(), ".yaml") {
				c.add(path, "repo does not equal its file name")
			}
			c.repos[r.Repo] = r
		}
	}

	entries, err = os.ReadDir(c.l.Repos())
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() || e.Name() == "store.git" {
			continue
		}
		if !strings.HasSuffix(e.Name(), ".git") {
			c.add(filepath.Join(c.l.Repos(), e.Name()), "a bare repository is named <name>.git")
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".git")
		if _, ok := c.repos[name]; !ok {
			c.add(filepath.Join(c.l.Repos(), e.Name()), "no declaration for this repository")
		}
	}
}

func (c *checker) worktrees() {
	labels := map[string]bool{}
	entries, err := os.ReadDir(c.l.WorktreeDecls())
	if err == nil {
		for _, e := range entries {
			if e.IsDir() || filepath.Ext(e.Name()) != ".yaml" {
				continue
			}
			path := filepath.Join(c.l.WorktreeDecls(), e.Name())
			var w records.Worktree
			ok, err := records.Read(path, &w)
			if err != nil {
				c.add(path, "%v", err)
				continue
			}
			if !ok {
				continue
			}
			if w.Label != strings.TrimSuffix(e.Name(), ".yaml") {
				c.add(path, "label does not equal its file name")
			}
			if _, ok := c.repos[w.Repo]; !ok {
				c.add(path, "repository %q is not declared", w.Repo)
			}
			labels[w.Label] = true
		}
	}

	entries, err = os.ReadDir(c.l.Worktrees())
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() && !labels[e.Name()] {
			c.add(filepath.Join(c.l.Worktrees(), e.Name()), "no declaration for this directory")
		}
	}
}

func (c *checker) jobs() {
	entries, err := os.ReadDir(c.l.Tasks())
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		path := c.l.Brief(e.Name())
		var b records.Brief
		body, ok, err := readDoc(path, &b)
		if err != nil {
			c.add(path, "%v", err)
			continue
		}
		if !ok {
			continue
		}
		if b.Job != e.Name() {
			c.add(path, "job does not equal its directory")
		}
		if b.Status != "active" {
			c.add(path, "a brief in doc/task/ must be active")
		}
		if _, ok := c.repos[b.Repo]; !ok {
			c.add(path, "repository %q is not declared", b.Repo)
		}
		if b.Branch != "job/"+b.Job {
			c.add(path, "branch is not job/%s", b.Job)
		}
		if b.Owner != "" && !c.actorExists(b.Owner) {
			c.add(path, "owner %q is not a declared actor", b.Owner)
		}
		for _, d := range b.DependsOn {
			if !c.jobExists(d) {
				c.add(path, "depends_on names no job: %s", d)
			}
		}
		for _, s := range []string{"Goal", "Scope", "Acceptance criteria"} {
			if !hasSection(body, s) {
				c.add(path, "no ## %s section", s)
			}
		}
	}
}

func (c *checker) handoffs() {
	entries, err := os.ReadDir(c.l.Ledger())
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		job := e.Name()
		brief, ok := c.briefOf(job)
		if !ok {
			c.add(c.l.HandoffDir(job), "no brief names this job")
			continue
		}
		files, err := os.ReadDir(c.l.HandoffDir(job))
		if err != nil {
			continue
		}
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".md") {
				continue
			}
			c.handoff(job, brief, f.Name())
		}
	}
}

func (c *checker) handoff(job string, brief records.Brief, name string) {
	path := c.l.Handoff(job, name)
	var h records.Handoff
	body, ok, err := readDoc(path, &h)
	if err != nil {
		c.add(path, "%v", err)
		return
	}
	if !ok {
		return
	}
	if h.Job != job {
		c.add(path, "job does not equal its directory")
	}
	if h.Repo != brief.Repo {
		c.add(path, "repo %q does not equal the brief's %q", h.Repo, brief.Repo)
	}
	if h.Branch != "job/"+job {
		c.add(path, "branch is not job/%s", job)
	}
	if h.Agent == "" {
		c.add(path, "no agent")
	} else if !c.actorExists(h.Agent) {
		c.add(path, "agent %q is not a declared actor", h.Agent)
	}
	if stamp, err := format.UTCToStamp(h.Started); err != nil {
		c.add(path, "started: %v", err)
	} else if stamp != strings.TrimSuffix(name, ".md") {
		c.add(path, "started does not match the file name")
	}
	switch h.Status {
	case "done":
		if h.Next != "none" {
			c.add(path, "a done stretch has next: none")
		}
	case "partial", "aborted":
		if h.Next == "" || h.Next == "none" {
			c.add(path, "a stretch that is not done leaves a baton")
		}
		if len(h.BlockedBy) > 0 {
			c.add(path, "blocked_by belongs to a blocked stretch only")
		}
	case "blocked":
		if len(h.BlockedBy) == 0 {
			c.add(path, "a blocked stretch names the jobs it waits on")
		}
		if h.Next == "" || h.Next == "none" {
			c.add(path, "a blocked stretch leaves a baton")
		}
	default:
		c.add(path, "%q is not a handoff status", h.Status)
	}
	if h.Supersedes != "" {
		if _, err := os.Stat(c.l.Handoff(job, h.Supersedes)); err != nil {
			c.add(path, "supersedes names no sibling: %s", h.Supersedes)
		}
	}
	for _, a := range h.Artifacts {
		if !strings.HasPrefix(filepath.ToSlash(a.Path), "job/"+job+"/") {
			c.add(path, "artifact %s is outside job/%s/", a.Path, job)
		}
		if !hex64.MatchString(a.SHA256) {
			c.add(path, "artifact %s has no raw sha256", a.Path)
		}
	}
	if gitdir := c.l.CodeRepo(brief.Repo); dirExists(gitdir) {
		for _, rev := range []string{h.Base, h.Head} {
			if rev == "" {
				continue
			}
			if _, err := gitx.Run("", "--git-dir", gitdir, "rev-parse", "--verify", "--quiet", rev+"^{commit}"); err != nil {
				c.add(path, "commit %s does not resolve in %s", rev, brief.Repo)
			}
		}
	}
	for _, s := range []string{"What changed", "What was verified", "What remains", "Open questions"} {
		if !hasSection(body, s) {
			c.add(path, "no ## %s section", s)
		}
	}
}

func (c *checker) briefOf(job string) (records.Brief, bool) {
	var b records.Brief
	_, ok, err := readDoc(c.l.Brief(job), &b)
	if err != nil {
		return b, false
	}
	if ok {
		return b, true
	}
	_, ok, err = readDoc(c.l.ArchivedBrief(job), &b)
	if err != nil {
		return b, false
	}
	return b, ok
}

func (c *checker) conventions() {
	seen := map[int]string{}
	entries, err := os.ReadDir(c.l.Conventions())
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		path := filepath.Join(c.l.Conventions(), e.Name())
		n, ok := format.ConventionFile(e.Name())
		if !ok {
			c.add(path, "a convention is <nn>-<slug>.md")
			continue
		}
		if prev, dup := seen[n]; dup {
			c.add(path, "number %02d is also %s", n, prev)
		}
		seen[n] = e.Name()
		c.oneTitle(path)
	}
}

func (c *checker) oneTitle(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	body := data
	if _, rest, err := format.Split(data); err == nil {
		body = rest
	}
	n := 0
	for _, ln := range strings.Split(string(body), "\n") {
		if strings.HasPrefix(ln, "# ") {
			n++
		}
	}
	if n != 1 {
		c.add(path, "wants exactly one H1 title, has %d", n)
	}
}

func (c *checker) topics() {
	entries, err := os.ReadDir(c.l.Topics())
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(c.l.Topics(), e.Name(), "overview.md")); err != nil {
			c.add(filepath.Join(c.l.Topics(), e.Name()), "a topic needs overview.md")
		}
	}
}

func (c *checker) skeleton() {
	for _, f := range layout.SkeletonFiles() {
		if _, err := os.Stat(filepath.Join(c.l.Root, f.Path)); err != nil {
			c.add(f.Path, "skeleton file is missing")
		}
	}
	want := []string{".gitignore", "/repos/", "/handoff/", "/var/", "/job/", "/worktree/"}
	for _, f := range layout.SkeletonFiles() {
		want = append(want, f.Ignore())
	}
	for _, path := range []string{c.l.GitIgnore(), c.l.InfoExclude()} {
		data, err := os.ReadFile(path)
		if err != nil {
			c.add(path, "the ignore rules are missing")
			continue
		}
		have := map[string]bool{}
		for _, ln := range strings.Split(string(data), "\n") {
			have[strings.TrimSpace(ln)] = true
		}
		for _, w := range want {
			if !have[w] {
				c.add(path, "the ignore rule %q is missing", w)
			}
		}
	}
}

func (c *checker) actorExists(name string) bool {
	if _, err := os.Stat(c.l.Actor(name)); err == nil {
		return true
	}
	_, err := os.Stat(c.l.ArchivedActor(name))
	return err == nil
}

func (c *checker) jobExists(job string) bool {
	if _, err := os.Stat(c.l.Brief(job)); err == nil {
		return true
	}
	_, err := os.Stat(c.l.ArchivedBrief(job))
	return err == nil
}

func readDoc(path string, v any) (body []byte, found bool, err error) {
	front, body, err := format.LoadDocument(path)
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if err := format.Decode(front, v); err != nil {
		return nil, true, err
	}
	return body, true, nil
}

func hasSection(body []byte, name string) bool {
	for _, ln := range strings.Split(string(body), "\n") {
		if strings.TrimSpace(ln) == "## "+name {
			return true
		}
	}
	return false
}

func readFile(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func dirExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.IsDir()
}

func relative(l layout.Layout, p string) string {
	if p == "" {
		return ""
	}
	r, err := filepath.Rel(l.Root, p)
	if err != nil {
		return p
	}
	return r
}
