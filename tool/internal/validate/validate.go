// Package validate runs the mechanical checks the design collects in one list.
//
// It reports and never repairs: a wrong repair costs more than a stale line, because
// the thing being reported may be someone else's work in progress.
package validate

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/multiagentworkspace/thrum/tool/internal/convention"
	"github.com/multiagentworkspace/thrum/tool/internal/format"
	"github.com/multiagentworkspace/thrum/tool/internal/gitx"
	"github.com/multiagentworkspace/thrum/tool/internal/workspace"
)

// Finding is one failed check.
type Finding struct {
	Where string // workspace-relative path, or "" when the check is about the tree
	Rule  string // the check, in the design's own words where it has any
	What  string // what is actually wrong
}

func (f Finding) String() string {
	if f.Where == "" {
		return fmt.Sprintf("%s: %s", f.Rule, f.What)
	}
	return fmt.Sprintf("%s: %s: %s", f.Where, f.Rule, f.What)
}

var (
	hex64RE = regexp.MustCompile(`^[0-9a-f]{64}$`)
	h1RE    = regexp.MustCompile(`^#\s+\S`)
	h2RE    = regexp.MustCompile(`^##\s+(.*\S)\s*$`)
)

// Validate checks a workspace and returns everything wrong with it, in a stable
// order. An empty result means the workspace satisfies every mechanical rule.
func Validate(l workspace.Layout) []Finding {
	v := &validator{l: l}
	v.run()
	sort.SliceStable(v.findings, func(i, j int) bool {
		if v.findings[i].Where != v.findings[j].Where {
			return v.findings[i].Where < v.findings[j].Where
		}
		return v.findings[i].Rule < v.findings[j].Rule
	})
	return v.findings
}

type validator struct {
	l        workspace.Layout
	findings []Finding
	// resources is every declared resource, by name, with the kind it was declared in.
	resources map[string]format.Resource
	kinds     map[string]string
	// worktreeEntries is doc/resource/worktree.yaml alone.
	worktreeEntries map[string]format.Resource
}

func (v *validator) add(where, rule, what string, args ...any) {
	v.findings = append(v.findings, Finding{Where: where, Rule: rule, What: fmt.Sprintf(what, args...)})
}

func (v *validator) rel(path string) string {
	r, err := filepath.Rel(v.l.Root, path)
	if err != nil {
		return path
	}
	return r
}

func (v *validator) run() {
	v.readResources()
	v.checkSite()
	v.checkBriefs(v.l.Task(), false)
	v.checkBriefs(v.l.ArchiveTask(""), true)
	v.checkHandoffs()
	v.checkManifests(v.l.Asset(), false)
	v.checkManifests(filepath.Join(v.l.Archive(), "asset"), true)
	v.checkLeases()
	v.checkOccupy()
	v.checkConventions()
	v.checkTopics()
	v.checkWorktreeDirs()
	v.checkSkeleton()
}

// checkSkeleton reports the unversioned skeleton the tooling carries: the directories a
// workspace is made of, the files it materialises for an actor, and the ignore rules that
// keep build state out of the source history.
//
// None of them is in a repository, so none can be checked against a record — which is
// exactly why they are what drifts when the tooling moves on, and why this check exists
// rather than being left to the tooling that wrote them.
func (v *validator) checkSkeleton() {
	drift, err := workspace.Compare(v.l)
	if err != nil {
		return
	}
	for _, dir := range drift.MissingDirs {
		v.add(dir, "the workspace has every directory the template defines", "the directory is not there")
	}
	for _, file := range drift.Skeleton {
		what := "it is not the copy the tooling carries — updating the skeleton says what the tooling has now, and writes nothing until it is asked to"
		if len(file.Workspace) == 0 {
			what = "it is not there"
		}
		v.add(file.Path, "the workspace holds the skeleton the tooling materialises", "%s", what)
	}
	if len(drift.IgnoreMissing) > 0 {
		v.add("", "the ignore rules are the tooling's list, in both copies",
			"these rules are not ignored in .gitignore or .git/info/exclude, so build state could be committed: %s",
			strings.Join(drift.IgnoreMissing, " "))
	}
}

// readResources loads every registry, checking as it goes that a kind file's key
// matches its name and that no name is used twice.
func (v *validator) readResources() {
	v.resources = map[string]format.Resource{}
	v.kinds = map[string]string{}
	v.worktreeEntries = map[string]format.Resource{}

	paths, _ := filepath.Glob(filepath.Join(v.l.Resource(), "*.yaml"))
	for _, path := range paths {
		kind := strings.TrimSuffix(filepath.Base(path), ".yaml")
		where := v.rel(path)

		raw, err := os.ReadFile(path)
		if err != nil {
			v.add(where, "a registry is readable", "%v", err)
			continue
		}
		var file map[string][]format.Resource
		if err := format.DecodeLoose(raw, &file); err != nil {
			v.add(where, "a registry is a mapping of kind to entries", "%v", err)
			continue
		}
		if len(file) != 1 {
			v.add(where, "one file per kind", "holds %d top-level keys, expected exactly the kind", len(file))
		}
		entries, ok := file[kind]
		if !ok {
			v.add(where, "one file per kind", "has no top-level key %q", kind)
		}
		for _, e := range entries {
			if e.Name == "" {
				v.add(where, "`name` is always present", "an entry has no name")
				continue
			}
			if !format.ValidSlug(e.Name) {
				v.add(where, "a name is a slug", "%q is not one", e.Name)
			}
			if kind, dup := v.kinds[e.Name]; dup {
				v.add(where, "resource names are unique across every kind",
					"%q is already declared as a %s", e.Name, kind)
			}
			v.resources[e.Name] = e
			v.kinds[e.Name] = kind
			if kind == format.WorktreeKind {
				v.worktreeEntries[e.Name] = e
			}
		}
	}
}

func (v *validator) checkSite() {
	site, err := os.ReadFile(v.l.SiteFile())
	if err != nil {
		v.add("var/site", "a site can name itself", "%v", err)
		return
	}
	name := strings.TrimSpace(string(site))
	if !format.ValidSlug(name) {
		v.add("var/site", "a site name is a slug", "%q is not one", name)
	}
}

// checkBriefs reads every brief in doc/task/ and, when archived, in doc/archive/task/.
func (v *validator) checkBriefs(dir string, archived bool) {
	pattern := filepath.Join(dir, "*", "brief.md")
	if archived {
		pattern = filepath.Join(dir, "*", "brief.md")
	}
	paths, _ := filepath.Glob(pattern)
	for _, path := range paths {
		where := v.rel(path)
		job := filepath.Base(filepath.Dir(path))

		front, body, err := format.LoadDocument(path)
		if err != nil {
			v.add(where, "a brief is front-matter and a body", "%v", err)
			continue
		}
		var b format.Brief
		if err := format.Decode(front, &b); err != nil {
			v.add(where, "a brief decodes", "%v", err)
			continue
		}
		if b.Job != job {
			v.add(where, "a brief's `job` equals its directory name", "%q != %q", b.Job, job)
		}
		switch b.Status {
		case format.StatusActive, format.StatusDone, format.StatusAbandoned:
		default:
			v.add(where, "a brief's `status` is in its enumeration",
				"%q is not active, done or abandoned", b.Status)
		}
		if archived && b.Status == format.StatusActive {
			v.add(where, "a closed job stays closed", "an archived brief is still %q", b.Status)
		}
		if b.Owner != "" && !format.ValidSlug(b.Owner) {
			v.add(where, "an owner is an actor name", "%q is not one", b.Owner)
		}
		v.checkSections(where, body, format.BriefSections)
	}
}

func (v *validator) checkHandoffs() {
	paths, _ := filepath.Glob(filepath.Join(v.l.Handoff(), "*", "*.md"))
	for _, path := range paths {
		where := v.rel(path)
		job := filepath.Base(filepath.Dir(path))
		name := filepath.Base(path)

		front, body, err := format.LoadDocument(path)
		if err != nil {
			v.add(where, "a handoff is front-matter and a body", "%v", err)
			continue
		}
		var h format.Handoff
		if err := format.Decode(front, &h); err != nil {
			v.add(where, "a handoff decodes", "%v", err)
			continue
		}

		if h.Job != job {
			v.add(where, "a handoff's `job` equals its directory name", "%q != %q", h.Job, job)
		}
		if h.Branch != "job/"+job {
			v.add(where, "a handoff's `branch` equals `job/` plus its directory name", "%q", h.Branch)
		}
		if !format.ValidSlug(h.Agent) {
			v.add(where, "an agent is an actor name", "%q is not one", h.Agent)
		}
		// `started` equals the file name converted.
		stamp := strings.TrimSuffix(name, ".md")
		if want, err := format.StampToUTC(stamp); err != nil || want != h.Started {
			v.add(where, "a handoff's `started` equals its file name converted",
				"file says %q, front-matter says %q", stamp, h.Started)
		}
		switch h.Status {
		case format.HandoffDone, format.HandoffPartial, format.HandoffBlocked, format.HandoffAborted:
		default:
			v.add(where, "a handoff's `status` is in its enumeration", "%q", h.Status)
		}
		nextNone := strings.EqualFold(strings.TrimSpace(h.Next), "none")
		if h.Status == format.HandoffDone && !nextNone {
			v.add(where, "`next` is `none` exactly when the status is `done`", "status done, next %q", h.Next)
		}
		if h.Status != format.HandoffDone && nextNone {
			v.add(where, "`next` is `none` exactly when the status is `done`", "status %q with next none", h.Status)
		}
		if (h.Status == format.HandoffBlocked) != (len(h.BlockedBy) > 0) {
			v.add(where, "`blocked_by` is present exactly when the status is `blocked`",
				"status %q, %d names", h.Status, len(h.BlockedBy))
		}
		if !gitx.Resolves(v.l.CodeRepo(), h.Base) {
			v.add(where, "`base` resolves in the code repository", "%q does not", h.Base)
		}
		if !gitx.Resolves(v.l.CodeRepo(), h.Head) {
			v.add(where, "`head` resolves in the code repository", "%q does not", h.Head)
		}
		v.checkSupersedes(where, h)
		for i, a := range h.Artifacts {
			want := "job/" + job + "/"
			if !strings.HasPrefix(a.Path, want) {
				v.add(where, "an artifact path lies under `job/<job-id>/`",
					"artifact %d is %q", i, a.Path)
			}
			if !hex64RE.MatchString(a.SHA256) {
				v.add(where, "an artifact's `sha256` is 64 hexadecimal digits",
					"artifact %d is %q", i, a.SHA256)
			}
		}
		v.checkSections(where, body, format.HandoffSections)
	}
}

// checkSupersedes follows the chain a correction starts, refusing a cycle.
func (v *validator) checkSupersedes(where string, h format.Handoff) {
	seen := map[string]bool{}
	cur := h
	for cur.Supersedes != "" {
		target := cur.Supersedes
		if seen[target] {
			v.add(where, "`supersedes` does not form a cycle", "%q is reached twice", target)
			return
		}
		seen[target] = true

		path := v.l.HandoffFile(cur.Job, target)
		front, _, err := format.LoadDocument(path)
		if err != nil {
			v.add(where, "`supersedes` names an existing sibling", "%q does not exist", target)
			return
		}
		var next format.Handoff
		if err := format.Decode(front, &next); err != nil {
			v.add(where, "`supersedes` names a sibling that decodes", "%q: %v", target, err)
			return
		}
		cur = next
	}
}

func (v *validator) checkManifests(dir string, archived bool) {
	paths, _ := filepath.Glob(filepath.Join(dir, "*", "*.md"))
	for _, path := range paths {
		where := v.rel(path)
		asset := filepath.Base(filepath.Dir(path))
		version := strings.TrimSuffix(filepath.Base(path), ".md")

		front, body, err := format.LoadDocument(path)
		if err != nil {
			v.add(where, "a manifest is front-matter and a body", "%v", err)
			continue
		}
		var m format.Manifest
		if err := format.Decode(front, &m); err != nil {
			v.add(where, "a manifest decodes", "%v", err)
			continue
		}
		if m.Asset != asset {
			v.add(where, "a manifest's `asset` equals its directory name", "%q != %q", m.Asset, asset)
		}
		if m.Version != version {
			v.add(where, "a manifest's `version` equals its file name", "%q != %q", m.Version, version)
		}
		if m.Ref != "" && m.Ref != format.AssetTag(m.Asset, m.Version) {
			v.add(where, "an asset tag equals `asset@version`", "%q", m.Ref)
		}
		if m.SHA256 != "" && !hex64RE.MatchString(m.SHA256) {
			v.add(where, "a raw `sha256` is 64 hexadecimal digits", "%q", m.SHA256)
		}
		// A retirement notice is the only body a retired manifest needs; a live one
		// needs its Description.
		if !archived {
			v.checkSections(where, body, []string{"Description"})
		} else {
			v.checkH1(where, body)
		}
	}
}

func (v *validator) checkLeases() {
	paths, _ := filepath.Glob(filepath.Join(v.l.Lease(), "*.yaml"))
	for _, path := range paths {
		where := v.rel(path)
		base := filepath.Base(path)
		resource, unit, nameErr := format.SplitLeaseFileName(base)

		raw, err := os.ReadFile(path)
		if err != nil {
			v.add(where, "a lease is readable", "%v", err)
			continue
		}
		var lease format.Lease
		if err := format.DecodeLoose(raw, &lease); err != nil {
			v.add(where, "a lease decodes", "%v", err)
			continue
		}
		if nameErr != nil {
			v.add(where, "a lease file is named `<resource>.<unit>.yaml`", "%v", nameErr)
		} else if lease.Resource != resource || lease.Unit != unit {
			v.add(where, "a lease's fields match its file name",
				"file says %s.%s, front-matter says %s.%s", resource, unit, lease.Resource, lease.Unit)
		}
		declared, ok := v.resources[lease.Resource]
		if !ok {
			v.add(where, "a lease's `resource` names a declaration", "%q is not declared", lease.Resource)
		} else {
			cap := declared.Capacity
			if cap == 0 {
				cap = 1
			}
			n, err := strconv.Atoi(lease.Unit)
			if err != nil {
				v.add(where, "a unit is a number", "%q", lease.Unit)
			} else if n < 1 || n > cap {
				v.add(where, "a unit is no greater than the resource's `capacity`",
					"unit %d, capacity %d", n, cap)
			}
		}
	}
}

func (v *validator) checkOccupy() {
	paths, _ := filepath.Glob(filepath.Join(v.l.Occupy(), "*.yaml"))
	for _, path := range paths {
		where := v.rel(path)
		job := strings.TrimSuffix(filepath.Base(path), ".yaml")

		raw, err := os.ReadFile(path)
		if err != nil {
			v.add(where, "an occupancy record is readable", "%v", err)
			continue
		}
		var o format.Occupy
		if err := format.DecodeLoose(raw, &o); err != nil {
			v.add(where, "an occupancy record decodes", "%v", err)
			continue
		}
		if !format.ValidSlug(o.Occupant) {
			v.add(where, "an occupant is an actor name", "%q is not one", o.Occupant)
		}
		if _, err := os.Stat(v.l.Brief(job)); err != nil {
			if _, err := os.Stat(v.l.ArchivedBrief(job)); err != nil {
				v.add(where, "an occupancy record is anchored by its job",
					"%q is in neither doc/task/ nor doc/archive/", job)
			}
		}
	}
}

func (v *validator) checkConventions() {
	paths, _ := filepath.Glob(filepath.Join(v.l.Convention(), "*.md"))
	seen := map[int]string{}
	for _, path := range paths {
		where := v.rel(path)
		name := filepath.Base(path)
		n, ok := format.ConventionFile(name)
		if !ok {
			v.add(where, "a convention file is two digits, a hyphen, a slug and .md", "%q", name)
			continue
		}
		if prev, dup := seen[n]; dup {
			v.add(where, "no two convention files share a number", "%02d is also %s", n, prev)
			continue
		}
		seen[n] = name
		v.checkH1File(where, path)
		// A convention is a rule rather than a record: what it has to say about itself is
		// its number, and that is in its name.
		if raw, err := os.ReadFile(path); err == nil && strings.HasPrefix(strings.TrimSpace(string(raw)), "---") {
			v.add(where, "a convention carries no front-matter", "it starts with one")
		}
	}

	// The values a convention declares are how a project's policy reaches the tooling, so
	// a block nobody can read — or two conventions saying different things about one key —
	// is a rule that silently does not apply.
	if _, problems, err := convention.Read(v.l); err == nil {
		for _, p := range problems {
			v.add(p.Where, "a convention declares what a tool reads in one values block of scalars", "%s", p.What)
		}
	}

	archived, _ := filepath.Glob(filepath.Join(v.l.Archive(), "convention", "*.md"))
	for _, path := range archived {
		n, ok := format.ConventionFile(filepath.Base(path))
		if ok {
			seen[n] = filepath.Base(path) // a retired convention keeps its number taken
		}
	}
}

func (v *validator) checkTopics() {
	base := v.l.Topic()
	_ = filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			// The first component is the topic, so the topic's own directory is the one
			// directly under doc/topic/ — and it says what it covers in its overview.md.
			if path != base && filepath.Dir(path) == filepath.Clean(base) {
				if _, err := os.Stat(filepath.Join(path, "overview.md")); err != nil {
					v.add(v.rel(path), "a topic says what it covers in `overview.md`", "there is none")
				}
			}
			return nil
		}
		if !strings.HasSuffix(path, ".md") {
			return nil
		}
		v.checkH1File(v.rel(path), path)
		return nil
	})
}

func (v *validator) checkWorktreeDirs() {
	dirs, err := os.ReadDir(v.l.Worktrees())
	if err != nil {
		return
	}
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		if _, ok := v.worktreeEntries[d.Name()]; !ok {
			v.add(filepath.Join("worktree", d.Name()),
				"every directory under `worktree/` has an entry in `doc/resource/worktree.yaml`",
				"no entry declares it")
		}
	}
}

// checkSections refuses a body that is missing one of its required sections, or that
// has them out of order. The required ones come first, in the order given; anything
// after them is additional and allowed.
func (v *validator) checkSections(where string, body []byte, want []string) {
	got := sectionNames(body)
	if len(got) < len(want) {
		v.add(where, "every required section is present", "%d found, %d required", len(got), len(want))
		return
	}
	for i, w := range want {
		if got[i] != w {
			v.add(where, "required sections appear in order, and first",
				"position %d is %q, expected %q", i+1, got[i], w)
			return
		}
	}
}

// checkH1File checks a document that can be retired for its one title.
//
// Front-matter is optional here: conventions and topics are written by actors in an
// editor, and the design requires a title of them rather than a front-matter block. A
// document that has one is read from after it, because that is what gives a retirement
// notice a defined position.
func (v *validator) checkH1File(where, path string) {
	raw, err := os.ReadFile(path)
	if err != nil {
		v.add(where, "a document that can be retired is readable", "%v", err)
		return
	}
	front, body, err := format.Split(raw)
	if err != nil || front == nil {
		// No front-matter: the whole file is the body.
		body = raw
	}
	v.checkH1(where, body)
}

// checkH1 checks that a retirable document has exactly one H1, first in its body.
func (v *validator) checkH1(where string, body []byte) {
	lines := strings.Split(string(body), "\n")
	count := 0
	first := ""
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if first == "" {
			first = line
		}
		if h1RE.MatchString(line) {
			count++
		}
	}
	if count != 1 {
		v.add(where, "exactly one H1 title", "found %d", count)
	} else if first != "" && !h1RE.MatchString(first) {
		v.add(where, "the H1 is the first line after any front-matter", "found %q", first)
	}
}

func sectionNames(body []byte) []string {
	var out []string
	for _, line := range strings.Split(string(body), "\n") {
		if m := h2RE.FindStringSubmatch(line); m != nil {
			out = append(out, m[1])
		}
	}
	return out
}
