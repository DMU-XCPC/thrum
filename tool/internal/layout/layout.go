// Package layout is where every path in a workspace is built, so that no other
// package has to know the tree by heart.
//
// The tree has three shapes, and the rule behind them is one sentence: a repository
// with a permanent checkout keeps its git directory beside it; one without goes bare
// under repos/. The line and the ledger have permanent checkouts; the code
// repositories and the store do not.
package layout

import (
	"path/filepath"
	"strings"
)

// A Layout is a workspace root.
type Layout struct {
	Root string
}

// New returns the layout rooted at root, made absolute because git is invoked with
// -C and resolves relative paths against that directory rather than the caller's.
func New(root string) Layout {
	abs, err := filepath.Abs(root)
	if err != nil {
		abs = root
	}
	return Layout{Root: abs}
}

// Repositories with no permanent checkout.
func (l Layout) Repos() string               { return filepath.Join(l.Root, "repos") }
func (l Layout) CodeRepo(name string) string { return filepath.Join(l.Repos(), name+".git") }
func (l Layout) Store() string               { return filepath.Join(l.Repos(), "store.git") }

// The line: a checkout at the root, whose git directory is the root's .git.
func (l Layout) GitIgnore() string   { return filepath.Join(l.Root, ".gitignore") }
func (l Layout) LineGit() string     { return filepath.Join(l.Root, ".git") }
func (l Layout) InfoExclude() string { return filepath.Join(l.LineGit(), "info", "exclude") }

func (l Layout) Doc() string           { return filepath.Join(l.Root, "doc") }
func (l Layout) Actors() string        { return filepath.Join(l.Doc(), "actor") }
func (l Layout) Sites() string         { return filepath.Join(l.Doc(), "site") }
func (l Layout) RepoDecls() string     { return filepath.Join(l.Doc(), "repo") }
func (l Layout) Conventions() string   { return filepath.Join(l.Doc(), "convention") }
func (l Layout) Topics() string        { return filepath.Join(l.Doc(), "topic") }
func (l Layout) Tasks() string         { return filepath.Join(l.Doc(), "task") }
func (l Layout) WorktreeDecls() string { return filepath.Join(l.Doc(), "worktree") }
func (l Layout) Archive() string       { return filepath.Join(l.Doc(), "archive") }
func (l Layout) DocTemplates() string  { return filepath.Join(l.Doc(), "template") }
func (l Layout) Assets() string        { return filepath.Join(l.Doc(), "asset") }

func (l Layout) Actor(name string) string { return filepath.Join(l.Actors(), name+".yaml") }
func (l Layout) ArchivedActor(name string) string {
	return filepath.Join(l.Archive(), "actor", name+".yaml")
}
func (l Layout) Site(name string) string     { return filepath.Join(l.Sites(), name+".yaml") }
func (l Layout) RepoDecl(name string) string { return filepath.Join(l.RepoDecls(), name+".yaml") }
func (l Layout) ArchivedRepo(name string) string {
	return filepath.Join(l.Archive(), "repo", name+".yaml")
}
func (l Layout) WorktreeDecl(label string) string {
	return filepath.Join(l.WorktreeDecls(), label+".yaml")
}
func (l Layout) ArchivedWorktree(label string) string {
	return filepath.Join(l.Archive(), "worktree", label+".yaml")
}

func (l Layout) TaskDir(job string) string { return filepath.Join(l.Tasks(), job) }
func (l Layout) Brief(job string) string   { return filepath.Join(l.TaskDir(job), "brief.md") }
func (l Layout) ArchivedTaskDir(job string) string {
	return filepath.Join(l.Archive(), "task", job)
}
func (l Layout) ArchivedBrief(job string) string {
	return filepath.Join(l.ArchivedTaskDir(job), "brief.md")
}
func (l Layout) Manifest(asset, version string) string {
	return filepath.Join(l.Assets(), asset, version+".md")
}
func (l Layout) ArchivedManifest(asset, version string) string {
	return filepath.Join(l.Archive(), "asset", asset, version+".md")
}

// The ledger: its own repository, checked out at handoff/.
func (l Layout) Ledger() string    { return filepath.Join(l.Root, "handoff") }
func (l Layout) LedgerGit() string { return filepath.Join(l.Ledger(), ".git") }
func (l Layout) HandoffDir(job string) string {
	return filepath.Join(l.Ledger(), job)
}
func (l Layout) Handoff(job, name string) string {
	return filepath.Join(l.HandoffDir(job), name)
}

// Runtime: this site's, never committed.
func (l Layout) Var() string                 { return filepath.Join(l.Root, "var") }
func (l Layout) SiteFile() string            { return filepath.Join(l.Var(), "site") }
func (l Layout) ConfigDir() string           { return filepath.Join(l.Var(), "config") }
func (l Layout) SecretsDir() string          { return filepath.Join(l.Var(), "secrets") }
func (l Layout) PrivateKey() string          { return filepath.Join(l.SecretsDir(), "site.key") }
func (l Layout) CacheDir() string            { return filepath.Join(l.Var(), "cache") }
func (l Layout) EnvDir() string              { return filepath.Join(l.Var(), "env") }
func (l Layout) StateDir() string            { return filepath.Join(l.Var(), "state") }
func (l Layout) Mailboxes() string           { return filepath.Join(l.Var(), "msg") }
func (l Layout) Mailbox(actor string) string { return filepath.Join(l.Mailboxes(), actor) }
func (l Layout) Locations() string           { return filepath.Join(l.Var(), "location.yaml") }
func (l Layout) DaemonLock() string          { return filepath.Join(l.Var(), "daemon.lock") }
func (l Layout) DaemonSocket() string        { return filepath.Join(l.Var(), "daemon.sock") }
func (l Layout) DaemonLog() string           { return filepath.Join(l.Var(), "daemon.log") }
func (l Layout) DaemonConfig() string        { return filepath.Join(l.Var(), "daemon.yaml") }

// A job's payload, and the working directories.
func (l Layout) Payloads() string          { return filepath.Join(l.Root, "job") }
func (l Layout) Payload(job string) string { return filepath.Join(l.Payloads(), job) }
func (l Layout) PayloadSub(job, sub string) string {
	return filepath.Join(l.Payload(job), sub)
}

var PayloadSubdirs = []string{"log", "tmp", "cache", "data", "state", "ckpt", "out"}

func (l Layout) Worktrees() string { return filepath.Join(l.Root, "worktree") }
func (l Layout) Worktree(label string) string {
	return filepath.Join(l.Worktrees(), label)
}

// SkeletonFile is one file the tooling materialises outside the repositories: the
// entry point an actor is handed and the skill it names. It is unversioned, so it is
// restored rather than merged.
type SkeletonFile struct {
	// Path is relative to the workspace root.
	Path string
}

// Ignore is the rule that keeps this file out of `git status`, and therefore out of
// clean.
func (f SkeletonFile) Ignore() string {
	if i := strings.IndexByte(f.Path, '/'); i >= 0 {
		return "/" + f.Path[:i] + "/"
	}
	return "/" + f.Path
}

// SkeletonFiles are the files the tooling materialises outside the repositories.
// The list is written out rather than derived, because it is the register of what the
// tooling owns.
func SkeletonFiles() []SkeletonFile {
	const skill = ".agents/skills/workspace-actor/"
	return []SkeletonFile{
		{Path: "AGENTS.md"},
		{Path: skill + "SKILL.md"},
		{Path: skill + "actors.md"},
		{Path: skill + "messages.md"},
		{Path: skill + "names.md"},
		{Path: skill + "placement.md"},
		{Path: skill + "records.md"},
		{Path: skill + "knowledge.md"},
		{Path: skill + "assets.md"},
		{Path: skill + "sites.md"},
	}
}

// EmptyDirs are the directories a workspace needs and a repository cannot carry,
// which is why creating a workspace is not a plain file copy.
func (l Layout) EmptyDirs() []string {
	return []string{
		l.Actors(),
		l.Sites(),
		l.RepoDecls(),
		l.Conventions(),
		l.Topics(),
		l.Tasks(),
		l.WorktreeDecls(),
		l.Archive(),
		l.DocTemplates(),
		l.Assets(),
		l.Ledger(),
		l.Repos(),
		l.CacheDir(),
		l.EnvDir(),
		l.StateDir(),
		l.ConfigDir(),
		l.SecretsDir(),
		l.Payloads(),
		l.Worktrees(),
	}
}
